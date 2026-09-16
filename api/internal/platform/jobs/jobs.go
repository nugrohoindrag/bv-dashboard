// Package jobs: River (Postgres-backed) job queue — transactional enqueue sebagai outbox (TAD §5.10, §5.11).
package jobs

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
	"github.com/riverqueue/river/rivertype"

	"github.com/buildingvision/api/internal/platform/events"
)

// ---------- Job args ----------

type DomainEventArgs struct {
	Event events.Event `json:"event"`
}

func (DomainEventArgs) Kind() string { return "domain_event.dispatch" }
func (DomainEventArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{MaxAttempts: 10, Queue: "events"}
}

type NotificationPushArgs struct {
	NotificationID uuid.UUID `json:"notification_id"`
	OrganizationID uuid.UUID `json:"organization_id"`
}

func (NotificationPushArgs) Kind() string { return "notification.push" }
func (NotificationPushArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{MaxAttempts: 5, Queue: "push"}
}

type AttachmentProcessArgs struct {
	AttachmentID   uuid.UUID `json:"attachment_id"`
	OrganizationID uuid.UUID `json:"organization_id"`
}

func (AttachmentProcessArgs) Kind() string { return "attachment.process" }

type ExportGenerateArgs struct {
	ExportID       uuid.UUID `json:"export_id"`
	OrganizationID uuid.UUID `json:"organization_id"`
}

func (ExportGenerateArgs) Kind() string { return "export.generate" }

type SearchIndexArgs struct {
	OrganizationID uuid.UUID `json:"organization_id"`
	ObjectType     string    `json:"object_type"`
	ObjectID       uuid.UUID `json:"object_id"`
}

func (SearchIndexArgs) Kind() string { return "search.index" }
func (SearchIndexArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{MaxAttempts: 5, Queue: "index"}
}

// Periodic job args (tanpa payload; iterasi per organization di worker)
type OverdueSweepArgs struct{}

func (OverdueSweepArgs) Kind() string { return "overdue.sweep" }

type SLASweepArgs struct{}

func (SLASweepArgs) Kind() string { return "sla.sweep" }

type DueSoonNotifyArgs struct{}

func (DueSoonNotifyArgs) Kind() string { return "due_soon.notify" }

type MaintenanceScheduleGenerateArgs struct {
	PlanID         *uuid.UUID `json:"plan_id,omitempty"` // nil = semua plan published
	OrganizationID *uuid.UUID `json:"organization_id,omitempty"`
}

func (MaintenanceScheduleGenerateArgs) Kind() string { return "maintenance_schedule.generate" }

type MaintenanceWorkOrderCreateArgs struct{}

func (MaintenanceWorkOrderCreateArgs) Kind() string { return "maintenance_work_order.create" }

type PatrolTaskGenerateArgs struct {
	ScheduleID     *uuid.UUID `json:"schedule_id,omitempty"`
	OrganizationID *uuid.UUID `json:"organization_id,omitempty"`
}

func (PatrolTaskGenerateArgs) Kind() string { return "patrol_task.generate" }

type CleaningTaskGenerateArgs struct {
	ScheduleID     *uuid.UUID `json:"schedule_id,omitempty"`
	OrganizationID *uuid.UUID `json:"organization_id,omitempty"`
}

func (CleaningTaskGenerateArgs) Kind() string { return "cleaning_task.generate" }

type IdempotencyCleanupArgs struct{}

func (IdempotencyCleanupArgs) Kind() string { return "idempotency.cleanup" }

// P1 sweeps: tagihan (overdue/due soon/payment expired), tamu kedaluwarsa, auto-close Service Request resolved.
type BillingSweepArgs struct{}

func (BillingSweepArgs) Kind() string { return "billing.sweep" }

type VisitorExpireArgs struct{}

func (VisitorExpireArgs) Kind() string { return "visitor.expire" }

type ServiceRequestAutoCloseArgs struct{}

// TrialSweepArgs: siklus hidup trial (Website PRD §31–§32): ending soon, expired, pengingat setup.
type TrialSweepArgs struct{}

func (TrialSweepArgs) Kind() string { return "trial.sweep" }

func (ServiceRequestAutoCloseArgs) Kind() string { return "service_request.auto_close" }

// BVRoomsSweepArgs: booking BVRooms yang lewat batas bayar → hangus; refresh min_rate_cache & popularity (Requirements v0.2 §4.2).
type BVRoomsSweepArgs struct{}

func (BVRoomsSweepArgs) Kind() string { return "bvrooms.sweep" }

// ---------- Enqueuer ----------

// Enqueuer dipakai service untuk enqueue dalam transaksi (outbox).
type Enqueuer interface {
	EnqueueTx(ctx context.Context, tx pgx.Tx, args river.JobArgs) error
	EnqueueEventTx(ctx context.Context, tx pgx.Tx, ev events.Event) error
}

type RiverEnqueuer struct {
	client *river.Client[pgx.Tx]
}

// NewInsertOnlyClient: client untuk api (hanya insert, tidak menjalankan worker).
func NewInsertOnlyClient(pool *pgxpool.Pool) (*RiverEnqueuer, error) {
	client, err := river.NewClient(riverpgxv5.New(pool), &river.Config{})
	if err != nil {
		return nil, err
	}
	return &RiverEnqueuer{client: client}, nil
}

func (r *RiverEnqueuer) Client() *river.Client[pgx.Tx] { return r.client }

func (r *RiverEnqueuer) EnqueueTx(ctx context.Context, tx pgx.Tx, args river.JobArgs) error {
	_, err := r.client.InsertTx(ctx, tx, args, nil)
	return err
}

func (r *RiverEnqueuer) EnqueueEventTx(ctx context.Context, tx pgx.Tx, ev events.Event) error {
	if ev.OccurredAt.IsZero() {
		ev.OccurredAt = time.Now().UTC()
	}
	return r.EnqueueTx(ctx, tx, DomainEventArgs{Event: ev})
}

// MemoryEnqueuer: untuk test — menyimpan job yang di-enqueue (tidak transactional).
type MemoryEnqueuer struct {
	Jobs   []river.JobArgs
	Events []events.Event
}

func (m *MemoryEnqueuer) EnqueueTx(_ context.Context, _ pgx.Tx, args river.JobArgs) error {
	m.Jobs = append(m.Jobs, args)
	if de, ok := args.(DomainEventArgs); ok {
		m.Events = append(m.Events, de.Event)
	}
	return nil
}
func (m *MemoryEnqueuer) EnqueueEventTx(ctx context.Context, tx pgx.Tx, ev events.Event) error {
	if ev.OccurredAt.IsZero() {
		ev.OccurredAt = time.Now().UTC()
	}
	return m.EnqueueTx(ctx, tx, DomainEventArgs{Event: ev})
}
func (m *MemoryEnqueuer) EventTypes() []string {
	out := make([]string, 0, len(m.Events))
	for _, e := range m.Events {
		out = append(out, e.Type)
	}
	return out
}

// Migrate menjalankan migrasi skema River.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	m, err := rivermigrate.New(riverpgxv5.New(pool), nil)
	if err != nil {
		return err
	}
	_, err = m.Migrate(ctx, rivermigrate.DirectionUp, nil)
	return err
}

// PeriodicJobs: jadwal TAD §5.11.
func PeriodicJobs() []*river.PeriodicJob {
	mk := func(interval time.Duration, args river.JobArgs) *river.PeriodicJob {
		return river.NewPeriodicJob(river.PeriodicInterval(interval), func() (river.JobArgs, *river.InsertOpts) {
			return args, &river.InsertOpts{UniqueOpts: river.UniqueOpts{ByArgs: true, ByState: []rivertype.JobState{rivertype.JobStateAvailable, rivertype.JobStateRunning, rivertype.JobStateScheduled, rivertype.JobStatePending, rivertype.JobStateRetryable}}}
		}, &river.PeriodicJobOpts{RunOnStart: true})
	}
	return []*river.PeriodicJob{
		mk(1*time.Minute, OverdueSweepArgs{}),
		mk(1*time.Minute, SLASweepArgs{}),
		mk(5*time.Minute, DueSoonNotifyArgs{}),
		mk(24*time.Hour, MaintenanceScheduleGenerateArgs{}),
		mk(6*time.Hour, MaintenanceWorkOrderCreateArgs{}),
		mk(6*time.Hour, PatrolTaskGenerateArgs{}),
		mk(6*time.Hour, CleaningTaskGenerateArgs{}),
		mk(1*time.Hour, IdempotencyCleanupArgs{}),
		mk(15*time.Minute, BillingSweepArgs{}),
		mk(15*time.Minute, VisitorExpireArgs{}),
		mk(30*time.Minute, ServiceRequestAutoCloseArgs{}),
		mk(1*time.Hour, TrialSweepArgs{}),
		mk(1*time.Minute, BVRoomsSweepArgs{}),
	}
}

// LogArgs helper untuk debugging.
func LogArgs(log *slog.Logger, args river.JobArgs) {
	b, _ := json.Marshal(args)
	log.Debug("job", slog.String("kind", args.Kind()), slog.String("args", string(b)))
}
