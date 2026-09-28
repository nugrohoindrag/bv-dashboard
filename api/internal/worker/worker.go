// Package worker: River workers + periodic scheduler (TAD §5.11). Satu binary `worker`.
package worker

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"log/slog"
	"time"

	"github.com/disintegration/imaging"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivertype"

	"github.com/buildingvision/api/internal/app"
	"github.com/buildingvision/api/internal/attachments"
	"github.com/buildingvision/api/internal/platform/authctx"
	"github.com/buildingvision/api/internal/platform/events"
	"github.com/buildingvision/api/internal/platform/jobs"
	"github.com/buildingvision/api/internal/platform/metrics"
)

type Worker struct {
	App    *app.App
	Log    *slog.Logger
	client *river.Client[pgx.Tx]
}

// Subscribers: fan-out domain event (notification rules, search index, sync conflict audit).
func Subscribers(a *app.App) []events.Subscriber {
	subs := []events.Subscriber{a.Notification, a.Search}
	if a.Finance != nil {
		subs = append(subs, a.Finance) // PRD P4 v2.1 P4-INT-04: event keuangan → webhook keluar
	}
	return subs
}

func New(a *app.App, log *slog.Logger) (*Worker, error) {
	w := &Worker{App: a, Log: log}
	workers := river.NewWorkers()
	river.AddWorker(workers, &domainEventWorker{a: a, log: log})
	river.AddWorker(workers, &pushWorker{a: a})
	river.AddWorker(workers, &deliverWorker{a: a})
	river.AddWorker(workers, &attachmentWorker{a: a, log: log})
	river.AddWorker(workers, &searchIndexWorker{a: a})
	river.AddWorker(workers, &exportWorker{a: a, log: log})
	river.AddWorker(workers, &overdueSweepWorker{a: a, log: log})
	river.AddWorker(workers, &slaSweepWorker{a: a, log: log})
	river.AddWorker(workers, &dueSoonWorker{a: a, log: log})
	river.AddWorker(workers, &msGenerateWorker{a: a, log: log})
	river.AddWorker(workers, &mwoCreateWorker{a: a, log: log})
	river.AddWorker(workers, &patrolGenWorker{a: a, log: log})
	river.AddWorker(workers, &cleaningGenWorker{a: a, log: log})
	river.AddWorker(workers, &billingSweepWorker{a: a, log: log})
	river.AddWorker(workers, &visitorExpireWorker{a: a, log: log})
	river.AddWorker(workers, &srAutoCloseWorker{a: a, log: log})
	river.AddWorker(workers, &trialSweepWorker{a: a, log: log})
	river.AddWorker(workers, &idemCleanupWorker{a: a})
	river.AddWorker(workers, &bvroomsSweepWorker{a: a, log: log})
	river.AddWorker(workers, &emergencySweepWorker{a: a, log: log})
	river.AddWorker(workers, &workforceSweepWorker{a: a, log: log})
	river.AddWorker(workers, &engineeringSweepWorker{a: a, log: log})
	river.AddWorker(workers, &tenantExperienceSweepWorker{a: a, log: log})
	river.AddWorker(workers, &financeSweepWorker{a: a, log: log})

	client, err := river.NewClient(riverpgxv5.New(a.DB.Pool), &river.Config{
		Queues: map[string]river.QueueConfig{
			river.QueueDefault: {MaxWorkers: 10},
			"events":           {MaxWorkers: 10},
			"push":             {MaxWorkers: 5},
			"index":            {MaxWorkers: 5},
		},
		Workers:          workers,
		PeriodicJobs:     jobs.PeriodicJobs(),
		Logger:           log,
		WorkerMiddleware: []rivertype.WorkerMiddleware{&metricsMiddleware{}},
	})
	if err != nil {
		return nil, err
	}
	w.client = client
	return w, nil
}

func (w *Worker) Start(ctx context.Context) error { return w.client.Start(ctx) }
func (w *Worker) Stop(ctx context.Context) error  { return w.client.Stop(ctx) }

// ---------- domain_event.dispatch ----------

type domainEventWorker struct {
	river.WorkerDefaults[jobs.DomainEventArgs]
	a   *app.App
	log *slog.Logger
}

func (w *domainEventWorker) Work(ctx context.Context, job *river.Job[jobs.DomainEventArgs]) error {
	ev := job.Args.Event
	for _, sub := range Subscribers(w.a) {
		if err := sub.Handle(ctx, ev); err != nil {
			w.log.Error("subscriber failed", "subscriber", sub.Name(), "event", ev.Type, "err", err)
			return err
		}
	}
	return nil
}

// ---------- notification.push ----------

type pushWorker struct {
	river.WorkerDefaults[jobs.NotificationPushArgs]
	a *app.App
}

func (w *pushWorker) Work(ctx context.Context, job *river.Job[jobs.NotificationPushArgs]) error {
	return w.a.Notification.SendPush(ctx, job.Args.OrganizationID, job.Args.NotificationID)
}

// notification.deliver: channel eksternal (email/WhatsApp) — PRD P0 v2 §14.3.
type deliverWorker struct {
	river.WorkerDefaults[jobs.NotificationDeliverArgs]
	a *app.App
}

func (w *deliverWorker) Work(ctx context.Context, job *river.Job[jobs.NotificationDeliverArgs]) error {
	return w.a.Notification.Deliver(ctx, job.Args.OrganizationID, job.Args.NotificationID, job.Args.Channel)
}

// ---------- attachment.process: thumbnail 320/1280, strip EXIF (re-encode), validasi magic bytes ----------

type attachmentWorker struct {
	river.WorkerDefaults[jobs.AttachmentProcessArgs]
	a   *app.App
	log *slog.Logger
}

func (w *attachmentWorker) Work(ctx context.Context, job *river.Job[jobs.AttachmentProcessArgs]) error {
	a := w.a
	return a.DB.WithOrgTx(ctx, job.Args.OrganizationID, func(ctx context.Context, tx pgx.Tx) error {
		var key, ct string
		if err := tx.QueryRow(ctx, `SELECT storage_key, content_type FROM attachments WHERE id = $1 AND status = 'ready'`, job.Args.AttachmentID).Scan(&key, &ct); err != nil {
			return nil
		}
		if ct != "image/jpeg" && ct != "image/png" && ct != "image/webp" {
			// dokumen: validasi magic bytes (PRD P0 v2 §13)
			rc, err := a.Storage.Get(ctx, key)
			if err != nil {
				return err
			}
			head := make([]byte, 512)
			n, _ := io.ReadFull(rc, head)
			_ = rc.Close()
			if !attachments.ValidMagic(ct, head[:n]) {
				_, _ = tx.Exec(ctx, `UPDATE attachments SET status = 'failed' WHERE id = $1`, job.Args.AttachmentID)
				w.log.Warn("attachment magic bytes mismatch", "id", job.Args.AttachmentID, "content_type", ct)
			}
			return nil
		}
		rc, err := a.Storage.Get(ctx, key)
		if err != nil {
			return err
		}
		defer func() { _ = rc.Close() }()
		img, format, err := image.Decode(rc)
		if err != nil {
			_, _ = tx.Exec(ctx, `UPDATE attachments SET status = 'failed' WHERE id = $1`, job.Args.AttachmentID)
			w.log.Warn("attachment decode failed (magic bytes)", "id", job.Args.AttachmentID, "err", err)
			return nil
		}
		_ = format
		b := img.Bounds()
		thumbKeys := map[int]string{}
		for _, size := range []int{320, 1280} {
			th := imaging.Fit(img, size, size, imaging.Lanczos)
			var buf bytes.Buffer
			if err := jpeg.Encode(&buf, th, &jpeg.Options{Quality: 80}); err != nil { // re-encode = EXIF strip
				return err
			}
			tk := fmt.Sprintf("%s.thumb%d.jpg", key, size)
			if err := a.Storage.Put(ctx, tk, "image/jpeg", bytes.NewReader(buf.Bytes()), int64(buf.Len())); err != nil {
				return err
			}
			thumbKeys[size] = tk
		}
		_, err = tx.Exec(ctx, `UPDATE attachments SET width = $2, height = $3, thumb_320_key = $4, thumb_1280_key = $5 WHERE id = $1`, job.Args.AttachmentID, b.Dx(), b.Dy(), thumbKeys[320], thumbKeys[1280])
		return err
	})
}

// ---------- search.index ----------

type searchIndexWorker struct {
	river.WorkerDefaults[jobs.SearchIndexArgs]
	a *app.App
}

func (w *searchIndexWorker) Work(ctx context.Context, job *river.Job[jobs.SearchIndexArgs]) error {
	return w.a.Search.Index(ctx, job.Args.OrganizationID, job.Args.ObjectType, job.Args.ObjectID)
}

// ---------- export.generate ----------

type exportWorker struct {
	river.WorkerDefaults[jobs.ExportGenerateArgs]
	a   *app.App
	log *slog.Logger
}

func (w *exportWorker) Work(ctx context.Context, job *river.Job[jobs.ExportGenerateArgs]) error {
	if w.a.Exports == nil {
		return nil
	}
	return w.a.Exports.Generate(ctx, job.Args.OrganizationID, job.Args.ExportID)
}

// ---------- periodic ----------

func forEachOrg(ctx context.Context, a *app.App, log *slog.Logger, name string, fn func(ctx context.Context, orgID uuid.UUID) (int, error)) error {
	orgs, err := a.DB.ListOrganizationIDs(ctx)
	if err != nil {
		return err
	}
	start := time.Now()
	total := 0
	for _, org := range orgs {
		n, err := fn(ctx, org)
		if err != nil {
			log.Error(name+" failed", "org", org, "err", err)
			continue
		}
		total += n
	}
	if total > 0 {
		log.Info(name, "affected", total, "orgs", len(orgs), "took", time.Since(start))
	}
	metrics.MarkSweep(name)
	return nil
}

// metricsMiddleware mencatat durasi & status tiap job River (RED job, TAD §11.5).
type metricsMiddleware struct {
	river.WorkerMiddlewareDefaults
}

func (*metricsMiddleware) Work(ctx context.Context, job *rivertype.JobRow, doInner func(context.Context) error) error {
	start := time.Now()
	err := doInner(ctx)
	status := "ok"
	if err != nil {
		status = "error"
	}
	metrics.JobsProcessed.WithLabelValues(job.Kind, status).Inc()
	metrics.JobDuration.WithLabelValues(job.Kind).Observe(time.Since(start).Seconds())
	return err
}

type overdueSweepWorker struct {
	river.WorkerDefaults[jobs.OverdueSweepArgs]
	a   *app.App
	log *slog.Logger
}

func (w *overdueSweepWorker) Work(ctx context.Context, _ *river.Job[jobs.OverdueSweepArgs]) error {
	return forEachOrg(ctx, w.a, w.log, "overdue.sweep", w.a.Operations.OverdueSweep)
}

type slaSweepWorker struct {
	river.WorkerDefaults[jobs.SLASweepArgs]
	a   *app.App
	log *slog.Logger
}

func (w *slaSweepWorker) Work(ctx context.Context, _ *river.Job[jobs.SLASweepArgs]) error {
	return forEachOrg(ctx, w.a, w.log, "sla.sweep", w.a.Operations.SLASweep)
}

type dueSoonWorker struct {
	river.WorkerDefaults[jobs.DueSoonNotifyArgs]
	a   *app.App
	log *slog.Logger
}

func (w *dueSoonWorker) Work(ctx context.Context, _ *river.Job[jobs.DueSoonNotifyArgs]) error {
	return forEachOrg(ctx, w.a, w.log, "due_soon.notify", func(ctx context.Context, org uuid.UUID) (int, error) {
		return w.a.Operations.DueSoonNotify(ctx, org, w.a.Cfg.DueSoonWindow)
	})
}

type msGenerateWorker struct {
	river.WorkerDefaults[jobs.MaintenanceScheduleGenerateArgs]
	a   *app.App
	log *slog.Logger
}

func (w *msGenerateWorker) Work(ctx context.Context, job *river.Job[jobs.MaintenanceScheduleGenerateArgs]) error {
	if job.Args.OrganizationID != nil {
		_, err := w.a.Engineering.GenerateSchedules(ctx, *job.Args.OrganizationID, job.Args.PlanID)
		return err
	}
	return forEachOrg(ctx, w.a, w.log, "maintenance_schedule.generate", func(ctx context.Context, org uuid.UUID) (int, error) {
		return w.a.Engineering.GenerateSchedules(ctx, org, nil)
	})
}

type mwoCreateWorker struct {
	river.WorkerDefaults[jobs.MaintenanceWorkOrderCreateArgs]
	a   *app.App
	log *slog.Logger
}

func (w *mwoCreateWorker) Work(ctx context.Context, _ *river.Job[jobs.MaintenanceWorkOrderCreateArgs]) error {
	return forEachOrg(ctx, w.a, w.log, "maintenance_work_order.create", w.a.Engineering.CreateDueWorkOrders)
}

type patrolGenWorker struct {
	river.WorkerDefaults[jobs.PatrolTaskGenerateArgs]
	a   *app.App
	log *slog.Logger
}

func (w *patrolGenWorker) Work(ctx context.Context, job *river.Job[jobs.PatrolTaskGenerateArgs]) error {
	if job.Args.OrganizationID != nil {
		_, err := w.a.Security.GeneratePatrolTasks(ctx, *job.Args.OrganizationID, job.Args.ScheduleID)
		return err
	}
	return forEachOrg(ctx, w.a, w.log, "patrol_task.generate", func(ctx context.Context, org uuid.UUID) (int, error) {
		return w.a.Security.GeneratePatrolTasks(ctx, org, nil)
	})
}

type cleaningGenWorker struct {
	river.WorkerDefaults[jobs.CleaningTaskGenerateArgs]
	a   *app.App
	log *slog.Logger
}

func (w *cleaningGenWorker) Work(ctx context.Context, job *river.Job[jobs.CleaningTaskGenerateArgs]) error {
	if job.Args.OrganizationID != nil {
		_, err := w.a.Housekeeping.GenerateCleaningTasks(ctx, *job.Args.OrganizationID, job.Args.ScheduleID)
		return err
	}
	return forEachOrg(ctx, w.a, w.log, "cleaning_task.generate", func(ctx context.Context, org uuid.UUID) (int, error) {
		n, err := w.a.Housekeeping.GenerateCleaningTasks(ctx, org, nil)
		if err != nil {
			return n, err
		}
		// PRD P2 v2.1 P2-RTE-02: run cleaning route harian (task berurutan)
		m, err := w.a.Housekeeping.GenerateRouteRuns(ctx, org, nil)
		return n + m, err
	})
}

type idemCleanupWorker struct {
	river.WorkerDefaults[jobs.IdempotencyCleanupArgs]
	a *app.App
}

func (w *idemCleanupWorker) Work(ctx context.Context, _ *river.Job[jobs.IdempotencyCleanupArgs]) error {
	_, err := w.a.DB.Pool.Exec(ctx, `DELETE FROM idempotency_keys WHERE expires_at < now()`)
	return err
}

var _ = authctx.System

// ---------- P1 sweeps ----------

type billingSweepWorker struct {
	river.WorkerDefaults[jobs.BillingSweepArgs]
	a   *app.App
	log *slog.Logger
}

func (w *billingSweepWorker) Work(ctx context.Context, _ *river.Job[jobs.BillingSweepArgs]) error {
	return forEachOrg(ctx, w.a, w.log, "billing.sweep", func(ctx context.Context, org uuid.UUID) (int, error) {
		return 0, w.a.Billing.Sweep(ctx, org)
	})
}

type visitorExpireWorker struct {
	river.WorkerDefaults[jobs.VisitorExpireArgs]
	a   *app.App
	log *slog.Logger
}

func (w *visitorExpireWorker) Work(ctx context.Context, _ *river.Job[jobs.VisitorExpireArgs]) error {
	return forEachOrg(ctx, w.a, w.log, "visitor.expire", w.a.Visitor.ExpireSweep)
}

type srAutoCloseWorker struct {
	river.WorkerDefaults[jobs.ServiceRequestAutoCloseArgs]
	a   *app.App
	log *slog.Logger
}

func (w *srAutoCloseWorker) Work(ctx context.Context, _ *river.Job[jobs.ServiceRequestAutoCloseArgs]) error {
	return forEachOrg(ctx, w.a, w.log, "service_request.auto_close", w.a.TenantApp.AutoCloseSweep)
}

// trialSweepWorker: siklus hidup trial (Website PRD §31–§32) — lintas organization dalam satu sweep.
type trialSweepWorker struct {
	river.WorkerDefaults[jobs.TrialSweepArgs]
	a   *app.App
	log *slog.Logger
}

func (w *trialSweepWorker) Work(ctx context.Context, _ *river.Job[jobs.TrialSweepArgs]) error {
	start := time.Now()
	n, err := w.a.Growth.Sweep(ctx)
	if err != nil {
		return err
	}
	if n > 0 {
		w.log.Info("trial.sweep", "affected", n, "took", time.Since(start))
	}
	metrics.MarkSweep("trial.sweep")
	return nil
}

// bvroomsSweepWorker: booking BVRooms hangus lewat batas bayar, refresh cache listing, Web Push tertunda (Requirements v0.2 §4.2).
type bvroomsSweepWorker struct {
	river.WorkerDefaults[jobs.BVRoomsSweepArgs]
	a   *app.App
	log *slog.Logger
}

func (w *bvroomsSweepWorker) Work(ctx context.Context, _ *river.Job[jobs.BVRoomsSweepArgs]) error {
	return forEachOrg(ctx, w.a, w.log, "bvrooms.sweep", w.a.BVRooms.Sweep)
}

// emergencySweepWorker: Emergency Alert belum di-acknowledge melewati batas → eskalasi (PRD P2 v2.1 P2-EMG-04).
type emergencySweepWorker struct {
	river.WorkerDefaults[jobs.EmergencySweepArgs]
	a   *app.App
	log *slog.Logger
}

func (w *emergencySweepWorker) Work(ctx context.Context, _ *river.Job[jobs.EmergencySweepArgs]) error {
	return forEachOrg(ctx, w.a, w.log, "emergency.sweep", w.a.Security.EmergencySweep)
}

// workforceSweepWorker: auto clock-out sesi yang terlupa (PRD P2 v2.1 §8).
type workforceSweepWorker struct {
	river.WorkerDefaults[jobs.WorkforceSweepArgs]
	a   *app.App
	log *slog.Logger
}

func (w *workforceSweepWorker) Work(ctx context.Context, _ *river.Job[jobs.WorkforceSweepArgs]) error {
	return forEachOrg(ctx, w.a, w.log, "attendance.auto_close", w.a.Workforce.AttendanceSweep)
}

// engineeringSweepWorker: pengingat dokumen/warranty + hitung ulang health seluruh asset (PRD P2 v2.1 §5.6).
type engineeringSweepWorker struct {
	river.WorkerDefaults[jobs.EngineeringSweepArgs]
	a   *app.App
	log *slog.Logger
}

func (w *engineeringSweepWorker) Work(ctx context.Context, _ *river.Job[jobs.EngineeringSweepArgs]) error {
	if err := forEachOrg(ctx, w.a, w.log, "asset_document.reminder", w.a.Asset.DocumentSweep); err != nil {
		return err
	}
	return forEachOrg(ctx, w.a, w.log, "asset.health", w.a.Asset.RecomputeAllHealth)
}

// tenantExperienceSweepWorker: pengumuman terjadwal → terbit, pengingat paket belum diambil, izin parkir kedaluwarsa/H-7
// (PRD P3 v2.1 P3-ANN-03, P3-PKG-04, P3-PRK-02).
type tenantExperienceSweepWorker struct {
	river.WorkerDefaults[jobs.TenantExperienceSweepArgs]
	a   *app.App
	log *slog.Logger
}

func (w *tenantExperienceSweepWorker) Work(ctx context.Context, _ *river.Job[jobs.TenantExperienceSweepArgs]) error {
	if err := forEachOrg(ctx, w.a, w.log, "announcement.publish_scheduled", w.a.TenantRelation.PublishScheduledSweep); err != nil {
		return err
	}
	if err := forEachOrg(ctx, w.a, w.log, "package.reminder", w.a.Parcels.ReminderSweep); err != nil {
		return err
	}
	return forEachOrg(ctx, w.a, w.log, "parking_permit.expiry", w.a.Security.ParkingPermitSweep)
}

// financeSweepWorker: webhook keluar tertunda (PRD P4 v2.1 P4-INT-04).
type financeSweepWorker struct {
	river.WorkerDefaults[jobs.FinanceSweepArgs]
	a   *app.App
	log *slog.Logger
}

func (w *financeSweepWorker) Work(ctx context.Context, _ *river.Job[jobs.FinanceSweepArgs]) error {
	if w.a.Finance == nil {
		return nil
	}
	return forEachOrg(ctx, w.a, w.log, "finance.webhook_deliver", w.a.Finance.DeliverSweep)
}
