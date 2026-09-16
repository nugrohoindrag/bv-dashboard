// Package attachments: Photo Evidence / file (PRD §11.2, TAD §5.13) — presign, confirm, list, signed download.
package attachments

import (
	"context"
	"fmt"
	"mime"
	"path"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/audit"
	"github.com/buildingvision/api/internal/platform/apperr"
	"github.com/buildingvision/api/internal/platform/authctx"
	"github.com/buildingvision/api/internal/platform/db"
	"github.com/buildingvision/api/internal/platform/events"
	"github.com/buildingvision/api/internal/platform/jobs"
	"github.com/buildingvision/api/internal/platform/storage"
)

type Service struct {
	DB          *db.DB
	Storage     storage.Storage
	Jobs        jobs.Enqueuer
	UploadTTL   time.Duration
	DownloadTTL time.Duration
	MaxBytes    int64
	// ObjectAccess memvalidasi user boleh menyentuh object (object_type, object_id) → property_id.
	ObjectAccess func(ctx context.Context, tx pgx.Tx, objectType string, objectID uuid.UUID, write bool) error
}

var allowedTypes = map[string]bool{"photo": true, "photo_before": true, "photo_after": true, "checklist_item_photo": true, "document": true, "signature": true}
var allowedContent = map[string]string{"image/jpeg": ".jpg", "image/png": ".png", "image/webp": ".webp", "application/pdf": ".pdf"}

type PresignInput struct {
	ObjectType         string    `json:"object_type"`
	ObjectID           uuid.UUID `json:"object_id"`
	AttachmentType     string    `json:"attachment_type"`
	ContentType        string    `json:"content_type"`
	SizeBytes          int64     `json:"size_bytes"`
	SHA256             *string   `json:"sha256"`
	OriginalFilename   *string   `json:"original_filename"`
	ClientAttachmentID *string   `json:"client_attachment_id"`
}

type PresignOutput struct {
	AttachmentID uuid.UUID         `json:"attachment_id"`
	UploadURL    string            `json:"upload_url"`
	StorageKey   string            `json:"storage_key"`
	ExpiresAt    time.Time         `json:"expires_at"`
	Method       string            `json:"method"`
	Headers      map[string]string `json:"headers"`
}

func (s *Service) Presign(ctx context.Context, in PresignInput) (*PresignOutput, error) {
	p := authctx.Must(ctx)
	if !allowedTypes[in.AttachmentType] {
		return nil, apperr.Validation("attachment_type tidak valid")
	}
	ext, ok := allowedContent[in.ContentType]
	if !ok {
		return nil, apperr.Validation("content_type harus image/jpeg|image/png|image/webp|application/pdf")
	}
	if in.SizeBytes <= 0 || in.SizeBytes > s.MaxBytes {
		return nil, apperr.Validation(fmt.Sprintf("size_bytes harus 1..%d", s.MaxBytes))
	}
	if in.ObjectType == "" || in.ObjectID == uuid.Nil {
		return nil, apperr.Validation("object_type dan object_id wajib")
	}
	var out *PresignOutput
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if s.ObjectAccess != nil {
			if err := s.ObjectAccess(ctx, tx, in.ObjectType, in.ObjectID, true); err != nil {
				return err
			}
		}
		// idempotent via client_attachment_id
		if in.ClientAttachmentID != nil && *in.ClientAttachmentID != "" {
			var id uuid.UUID
			var key string
			err := tx.QueryRow(ctx, `SELECT id, storage_key FROM attachments WHERE uploaded_by = $1 AND client_attachment_id = $2`, p.UserID, *in.ClientAttachmentID).Scan(&id, &key)
			if err == nil {
				url, err := s.Storage.PresignPut(ctx, key, in.ContentType, in.SizeBytes, s.UploadTTL)
				if err != nil {
					return err
				}
				out = &PresignOutput{AttachmentID: id, UploadURL: url, StorageKey: key, ExpiresAt: time.Now().Add(s.UploadTTL), Method: "PUT", Headers: map[string]string{"Content-Type": in.ContentType}}
				return nil
			}
		}
		id := uuid.Must(uuid.NewV7())
		now := time.Now().UTC()
		key := storage.ObjectKey(p.OrganizationID.String(), id.String(), now, ext)
		_, err := tx.Exec(ctx, `INSERT INTO attachments (id, organization_id, object_type, object_id, attachment_type, storage_key, original_filename, content_type, size_bytes, sha256, uploaded_by, client_attachment_id, status)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,'pending')`,
			id, p.OrganizationID, in.ObjectType, in.ObjectID, in.AttachmentType, key, in.OriginalFilename, in.ContentType, in.SizeBytes, in.SHA256, p.UserID, in.ClientAttachmentID)
		if err != nil {
			return err
		}
		url, err := s.Storage.PresignPut(ctx, key, in.ContentType, in.SizeBytes, s.UploadTTL)
		if err != nil {
			return err
		}
		out = &PresignOutput{AttachmentID: id, UploadURL: url, StorageKey: key, ExpiresAt: now.Add(s.UploadTTL), Method: "PUT", Headers: map[string]string{"Content-Type": in.ContentType}}
		return nil
	})
	return out, err
}

type ConfirmInput struct {
	CapturedAt   *time.Time `json:"captured_at"`
	GPSLat       *float64   `json:"gps_lat"`
	GPSLng       *float64   `json:"gps_lng"`
	GPSAccuracyM *float64   `json:"gps_accuracy_m"`
	GPSStatus    string     `json:"gps_status"` // captured | unavailable | denied
	DeviceID     *string    `json:"device_id"`
	Caption      *string    `json:"caption"`
	Width        *int       `json:"width"`
	Height       *int       `json:"height"`
}

// Confirm menandai file sudah tiba → ready; enqueue attachment.process; activity attachment_added.
func (s *Service) Confirm(ctx context.Context, id uuid.UUID, in ConfirmInput) (*Attachment, error) {
	p := authctx.Must(ctx)
	if in.GPSStatus == "" {
		in.GPSStatus = "unavailable"
	}
	if in.GPSStatus != "captured" && in.GPSStatus != "unavailable" && in.GPSStatus != "denied" {
		return nil, apperr.Validation("gps_status harus captured|unavailable|denied")
	}
	if in.GPSStatus == "captured" && (in.GPSLat == nil || in.GPSLng == nil) {
		in.GPSStatus = "unavailable"
	}
	var out *Attachment
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		a, err := getTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if a.Status == "ready" {
			out = a // idempotent
			return nil
		}
		// verifikasi file ada di storage (magic bytes/size divalidasi lebih lanjut oleh job)
		size, _, err := s.Storage.Head(ctx, a.StorageKey)
		if err != nil {
			return apperr.Conflict("UPLOAD_NOT_FOUND", "File belum diunggah ke storage")
		}
		if size > s.MaxBytes {
			_, _ = tx.Exec(ctx, `UPDATE attachments SET status = 'failed' WHERE id = $1`, id)
			return apperr.Validation("ukuran file melebihi batas")
		}
		captured := time.Now().UTC()
		if in.CapturedAt != nil {
			captured = *in.CapturedAt
		}
		_, err = tx.Exec(ctx, `UPDATE attachments SET status = 'ready', size_bytes = $2, captured_at = $3, gps_lat = $4, gps_lng = $5, gps_accuracy_m = $6, gps_status = $7, device_id = $8, caption = $9, width = $10, height = $11 WHERE id = $1`,
			id, size, captured, in.GPSLat, in.GPSLng, in.GPSAccuracyM, in.GPSStatus, in.DeviceID, in.Caption, in.Width, in.Height)
		if err != nil {
			return err
		}
		_ = audit.Record(ctx, tx, audit.Entry{ObjectType: a.ObjectType, ObjectID: a.ObjectID, Action: audit.ActAttachmentAdded, Payload: map[string]any{
			"attachment_id": id, "attachment_type": a.AttachmentType, "gps_status": in.GPSStatus, "content_type": a.ContentType,
		}, ClientRecordedAt: in.CapturedAt})
		if s.Jobs != nil {
			_ = s.Jobs.EnqueueTx(ctx, tx, jobs.AttachmentProcessArgs{AttachmentID: id, OrganizationID: p.OrganizationID})
			_ = s.Jobs.EnqueueEventTx(ctx, tx, events.Event{Type: events.AttachmentConfirmed, OrganizationID: p.OrganizationID, ObjectType: a.ObjectType, ObjectID: a.ObjectID, ActorUserID: &p.UserID, Payload: map[string]any{"attachment_id": id, "attachment_type": a.AttachmentType}})
		}
		out, err = getTx(ctx, tx, id)
		return err
	})
	return out, err
}

type Attachment struct {
	ID                 uuid.UUID  `json:"id"`
	ObjectType         string     `json:"object_type"`
	ObjectID           uuid.UUID  `json:"object_id"`
	AttachmentType     string     `json:"attachment_type"`
	StorageKey         string     `json:"-"`
	OriginalFilename   *string    `json:"original_filename"`
	ContentType        string     `json:"content_type"`
	SizeBytes          int64      `json:"size_bytes"`
	Width              *int       `json:"width"`
	Height             *int       `json:"height"`
	CapturedAt         *time.Time `json:"captured_at"`
	UploadedBy         uuid.UUID  `json:"uploaded_by"`
	UploadedByName     string     `json:"uploaded_by_name"`
	UploadedAt         time.Time  `json:"uploaded_at"`
	GPSLat             *float64   `json:"gps_lat"`
	GPSLng             *float64   `json:"gps_lng"`
	GPSAccuracyM       *float64   `json:"gps_accuracy_m"`
	GPSStatus          string     `json:"gps_status"`
	Status             string     `json:"status"`
	Caption            *string    `json:"caption"`
	ClientAttachmentID *string    `json:"client_attachment_id"`
	URL                string     `json:"url,omitempty"`
	ThumbURL           string     `json:"thumb_url,omitempty"`
	thumb320           *string
}

const selectCols = `a.id, a.object_type, a.object_id, a.attachment_type, a.storage_key, a.original_filename, a.content_type, a.size_bytes, a.width, a.height,
	a.captured_at, a.uploaded_by, COALESCE(u.full_name,''), a.uploaded_at, a.gps_lat, a.gps_lng, a.gps_accuracy_m, a.gps_status, a.status, a.caption, a.client_attachment_id, a.thumb_320_key`

func scan(row pgx.Row) (*Attachment, error) {
	var a Attachment
	if err := row.Scan(&a.ID, &a.ObjectType, &a.ObjectID, &a.AttachmentType, &a.StorageKey, &a.OriginalFilename, &a.ContentType, &a.SizeBytes, &a.Width, &a.Height,
		&a.CapturedAt, &a.UploadedBy, &a.UploadedByName, &a.UploadedAt, &a.GPSLat, &a.GPSLng, &a.GPSAccuracyM, &a.GPSStatus, &a.Status, &a.Caption, &a.ClientAttachmentID, &a.thumb320); err != nil {
		return nil, err
	}
	return &a, nil
}

func getTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*Attachment, error) {
	a, err := scan(tx.QueryRow(ctx, `SELECT `+selectCols+` FROM attachments a LEFT JOIN users u ON u.id = a.uploaded_by WHERE a.id = $1 AND a.deleted_at IS NULL`, id))
	if err != nil {
		if db.IsNoRows(err) {
			return nil, apperr.NotFound("Attachment")
		}
		return nil, err
	}
	return a, nil
}

// ListForObject: dengan presigned GET URL (10 menit) setelah cek permission via ObjectAccess.
func (s *Service) ListForObject(ctx context.Context, objectType string, objectID uuid.UUID) ([]Attachment, error) {
	var out []Attachment
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if s.ObjectAccess != nil {
			if err := s.ObjectAccess(ctx, tx, objectType, objectID, false); err != nil {
				return err
			}
		}
		rows, err := tx.Query(ctx, `SELECT `+selectCols+` FROM attachments a LEFT JOIN users u ON u.id = a.uploaded_by WHERE a.object_type = $1 AND a.object_id = $2 AND a.deleted_at IS NULL ORDER BY a.uploaded_at`, objectType, objectID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			a, err := scan(rows)
			if err != nil {
				return err
			}
			out = append(out, *a)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	for i := range out {
		s.fillURLs(ctx, &out[i])
	}
	if out == nil {
		out = []Attachment{}
	}
	return out, nil
}

// ListForObjectTx: versi untuk dipakai service lain di dalam transaksi (tanpa URL).
func ListForObjectTx(ctx context.Context, tx pgx.Tx, objectType string, objectID uuid.UUID) ([]Attachment, error) {
	rows, err := tx.Query(ctx, `SELECT `+selectCols+` FROM attachments a LEFT JOIN users u ON u.id = a.uploaded_by WHERE a.object_type = $1 AND a.object_id = $2 AND a.deleted_at IS NULL ORDER BY a.uploaded_at`, objectType, objectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Attachment
	for rows.Next() {
		a, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

func (s *Service) fillURLs(ctx context.Context, a *Attachment) {
	if a.Status != "ready" {
		return
	}
	if u, err := s.Storage.PresignGet(ctx, a.StorageKey, s.DownloadTTL); err == nil {
		a.URL = u
	}
	if a.thumb320 != nil {
		if u, err := s.Storage.PresignGet(ctx, *a.thumb320, s.DownloadTTL); err == nil {
			a.ThumbURL = u
		}
	} else {
		a.ThumbURL = a.URL
	}
}

// GetTx: baca attachment di dalam transaksi tanpa cek akses (pemanggil bertanggung jawab atas otorisasi).
func (s *Service) GetTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*Attachment, error) {
	return getTx(ctx, tx, id)
}

// FillURLs: dipakai handler lain (mis. detail WO) untuk melengkapi URL.
func (s *Service) FillURLs(ctx context.Context, list []Attachment) {
	for i := range list {
		s.fillURLs(ctx, &list[i])
	}
}

func (s *Service) Get(ctx context.Context, id uuid.UUID) (*Attachment, error) {
	var out *Attachment
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		a, err := getTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if s.ObjectAccess != nil {
			if err := s.ObjectAccess(ctx, tx, a.ObjectType, a.ObjectID, false); err != nil {
				return err
			}
		}
		out = a
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.fillURLs(ctx, out)
	return out, nil
}

func (s *Service) Delete(ctx context.Context, id uuid.UUID) error {
	p := authctx.Must(ctx)
	return s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		a, err := getTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if a.UploadedBy != p.UserID && !p.Has("operations.attachments.delete") {
			return apperr.Forbidden("")
		}
		if s.ObjectAccess != nil {
			if err := s.ObjectAccess(ctx, tx, a.ObjectType, a.ObjectID, true); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE attachments SET deleted_at = now() WHERE id = $1`, id); err != nil {
			return err
		}
		_ = audit.Record(ctx, tx, audit.Entry{ObjectType: a.ObjectType, ObjectID: a.ObjectID, Action: audit.ActUpdated, Payload: map[string]any{"attachment_removed": id}})
		return audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditDelete, EntityType: "attachment", EntityID: &id})
	})
}

// CountByType: helper guard EvidenceSatisfied (photo_after wajib).
func CountByType(ctx context.Context, q db.Querier, objectType string, objectID uuid.UUID, attachmentType string, includePending bool) (int, error) {
	var n int
	statuses := []string{"ready"}
	if includePending {
		statuses = append(statuses, "pending")
	}
	err := q.QueryRow(ctx, `SELECT count(*) FROM attachments WHERE object_type = $1 AND object_id = $2 AND attachment_type = $3 AND status = ANY($4) AND deleted_at IS NULL`, objectType, objectID, attachmentType, statuses).Scan(&n)
	return n, err
}

func ExtFor(contentType string) string {
	if e, ok := allowedContent[contentType]; ok {
		return e
	}
	exts, _ := mime.ExtensionsByType(contentType)
	if len(exts) > 0 {
		return exts[0]
	}
	return path.Ext(strings.ToLower(contentType))
}
