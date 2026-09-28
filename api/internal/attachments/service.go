// Package attachments: Photo Evidence / file (PRD §11.2, TAD §5.13) — presign, confirm, list, signed download.
package attachments

import (
	"bytes"
	"context"
	"fmt"
	"mime"
	"path"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

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
	// MaxImageBytes: batas foto (image/*). Klien wajib mengompres ke ≤ batas ini sebelum unggah;
	// server tetap menolak di presign (size_bytes) dan confirm (ukuran nyata di storage). 0 = pakai MaxBytes.
	MaxImageBytes int64
	// MaxVideoBytes: batas video evidence (PRD P2 v2.1 P2-SIN-04, P2-NFR-03). 0 = pakai MaxBytes.
	MaxVideoBytes int64
	// ObjectAccess memvalidasi user boleh menyentuh object (object_type, object_id) → property_id.
	ObjectAccess func(ctx context.Context, tx pgx.Tx, objectType string, objectID uuid.UUID, write bool) error
}

var allowedTypes = map[string]bool{"photo": true, "photo_before": true, "photo_during": true, "photo_after": true, "checklist_item_photo": true, "document": true, "signature": true, "video": true}

// LimitFor: batas byte per content type — foto memakai MaxImageBytes (default 500 KB), lainnya MaxBytes.
func (s *Service) LimitFor(contentType string) int64 {
	if IsImage(contentType) && s.MaxImageBytes > 0 && s.MaxImageBytes < s.MaxBytes {
		return s.MaxImageBytes
	}
	if IsVideo(contentType) && s.MaxVideoBytes > 0 {
		return s.MaxVideoBytes
	}
	return s.MaxBytes
}

// IsImage: content type foto.
func IsImage(contentType string) bool { return strings.HasPrefix(contentType, "image/") }

// IsVideo: content type video evidence (mp4/mov/webm).
func IsVideo(contentType string) bool { return strings.HasPrefix(contentType, "video/") }

// AllowedType: nilai attachment_type yang diterima kolom.
func AllowedType(t string) bool { return allowedTypes[t] }

// NormalizeType: alias pendek dari kontrak sync (`before|after|checklist`, contracts/sync-api.md) →
// nilai kolom `attachments.attachment_type` (CHECK constraint migrasi 00004).
func NormalizeType(t string) string {
	switch t {
	case "", "photo":
		return "photo"
	case "before":
		return "photo_before"
	case "during":
		return "photo_during" // PRD P1 v2 §19: Before / During / After
	case "after":
		return "photo_after"
	case "checklist":
		return "checklist_item_photo"
	}
	return t
}

// allowedContent: foto + dokumen (PRD P0 v2 §13: Photo, Document, PDF, other supported file types).
var allowedContent = map[string]string{
	"image/jpeg": ".jpg", "image/png": ".png", "image/webp": ".webp",
	"application/pdf":    ".pdf",
	"application/msword": ".doc",
	"application/vnd.openxmlformats-officedocument.wordprocessingml.document": ".docx",
	"application/vnd.ms-excel": ".xls",
	"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet": ".xlsx",
	"text/csv":   ".csv",
	"text/plain": ".txt",
	// PRD P2 v2.1 P2-SIN-04: video evidence incident (dibatasi MaxVideoBytes)
	"video/mp4": ".mp4", "video/quicktime": ".mov", "video/webm": ".webm",
}

// SupportedContentTypes: daftar content type yang diterima (untuk pesan error & dokumentasi).
func SupportedContentTypes() []string {
	out := make([]string, 0, len(allowedContent))
	for k := range allowedContent {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ValidMagic: validasi signature file (magic bytes) dokumen non-gambar; gambar divalidasi lewat decode.
func ValidMagic(contentType string, head []byte) bool {
	switch contentType {
	case "application/pdf":
		return bytes.HasPrefix(head, []byte("%PDF"))
	case "application/vnd.openxmlformats-officedocument.wordprocessingml.document", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":
		return bytes.HasPrefix(head, []byte("PK\x03\x04"))
	case "application/msword", "application/vnd.ms-excel":
		return bytes.HasPrefix(head, []byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1})
	case "text/csv", "text/plain":
		return utf8.Valid(head) && !bytes.Contains(head, []byte{0})
	case "video/mp4", "video/quicktime":
		return len(head) >= 8 && bytes.Equal(head[4:8], []byte("ftyp"))
	case "video/webm":
		return bytes.HasPrefix(head, []byte{0x1A, 0x45, 0xDF, 0xA3})
	}
	return true
}

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
	in.AttachmentType = NormalizeType(in.AttachmentType)
	if !allowedTypes[in.AttachmentType] {
		return nil, apperr.Validation("attachment_type tidak valid")
	}
	ext, ok := allowedContent[in.ContentType]
	if !ok {
		return nil, apperr.Validation("content_type tidak didukung; gunakan salah satu: "+strings.Join(SupportedContentTypes(), ", ")).WithField("content_type", "tidak didukung")
	}
	if limit := s.LimitFor(in.ContentType); in.SizeBytes <= 0 || in.SizeBytes > limit {
		return nil, apperr.Validation(fmt.Sprintf("size_bytes harus 1..%d", limit)).WithField("size_bytes", fmt.Sprintf("maksimal %d KB", limit/1024))
	}
	if in.ObjectType == "" || in.ObjectID == uuid.Nil {
		return nil, apperr.Validation("object_type dan object_id wajib")
	}
	// tanda tangan & foto harus berupa gambar; dokumen boleh PDF/Office/CSV/TXT; video hanya untuk attachment_type video
	if in.AttachmentType == "video" {
		if !IsVideo(in.ContentType) {
			return nil, apperr.Validation("attachment_type video harus berupa video (mp4/mov/webm)").WithField("content_type", "harus video/*")
		}
	} else if IsVideo(in.ContentType) {
		return nil, apperr.Validation("video diunggah dengan attachment_type video").WithField("attachment_type", "gunakan video")
	} else if in.AttachmentType != "document" && !IsImage(in.ContentType) {
		return nil, apperr.Validation("attachment_type "+in.AttachmentType+" harus berupa gambar").WithField("content_type", "harus image/*")
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
		// PRD P0 v2 §24.1 secure file access: hanya pengunggah yang dapat mengonfirmasi, dan object masih dapat diakses
		if a.UploadedBy != p.UserID && !p.IsSystem {
			return apperr.Forbidden("Hanya pengunggah yang dapat mengonfirmasi attachment ini")
		}
		if s.ObjectAccess != nil {
			if err := s.ObjectAccess(ctx, tx, a.ObjectType, a.ObjectID, true); err != nil {
				return err
			}
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
		if limit := s.LimitFor(a.ContentType); size > limit {
			_, _ = tx.Exec(ctx, `UPDATE attachments SET status = 'failed' WHERE id = $1`, id)
			return apperr.Validation(fmt.Sprintf("ukuran file melebihi batas %d KB", limit/1024))
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
		if s.ObjectAccess != nil {
			if err := s.ObjectAccess(ctx, tx, a.ObjectType, a.ObjectID, true); err != nil {
				return err
			}
		}
		// selain pengunggah, hapus memerlukan permission pada property object (bukan sekadar di org)
		if a.UploadedBy != p.UserID {
			var pid uuid.UUID
			if err := tx.QueryRow(ctx, `SELECT property_id FROM (
				SELECT property_id FROM tasks WHERE id = $1 UNION ALL SELECT property_id FROM work_orders WHERE id = $1
				UNION ALL SELECT property_id FROM incidents WHERE id = $1 UNION ALL SELECT property_id FROM findings WHERE id = $1
				UNION ALL SELECT property_id FROM service_requests WHERE id = $1 UNION ALL SELECT property_id FROM assets WHERE id = $1) x LIMIT 1`, a.ObjectID).Scan(&pid); err != nil {
				if !p.Has("operations.attachments.delete") {
					return apperr.Forbidden("")
				}
			} else if !p.HasOnProperty("operations.attachments.delete", pid) {
				return apperr.Forbidden("Memerlukan operations.attachments.delete pada property ini")
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

// PhotoTypes: semua tipe foto (evidence Task menerima tipe apa pun; WO menuntut photo_after).
var PhotoTypes = []string{"photo", "photo_before", "photo_during", "photo_after", "checklist_item_photo"}

// CountPhotos: jumlah attachment bertipe foto apa pun (lihat PhotoTypes).
func CountPhotos(ctx context.Context, q db.Querier, objectType string, objectID uuid.UUID, includePending bool) (int, error) {
	var n int
	statuses := []string{"ready"}
	if includePending {
		statuses = append(statuses, "pending")
	}
	err := q.QueryRow(ctx, `SELECT count(*) FROM attachments WHERE object_type = $1 AND object_id = $2 AND attachment_type = ANY($3) AND status = ANY($4) AND deleted_at IS NULL`, objectType, objectID, PhotoTypes, statuses).Scan(&n)
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

// GetForMessageTx: metadata lampiran tanpa cek akses (dipakai setelah akses object pesan divalidasi pemanggil).
func GetForMessageTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*Attachment, error) {
	return getTx(ctx, tx, id)
}
