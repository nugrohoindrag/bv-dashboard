// Package appdownload: konfigurasi tautan unduhan aplikasi mobile (Website PRD v1.1 §15–§22, §42–§48).
// Konfigurasi global (bukan per organization); dikelola hanya oleh role admin_internal pada organization internal;
// public website hanya membaca tautan aktif. Distribusi awal lewat Google Drive: URL disimpan, tidak diunduh/diperiksa (§21).
package appdownload

import (
	"context"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/audit"
	"github.com/buildingvision/api/internal/platform/apperr"
	"github.com/buildingvision/api/internal/platform/authctx"
	"github.com/buildingvision/api/internal/platform/db"
)

// Audit action (§20)
const (
	AuditCreated     = "APP_DOWNLOAD_CREATED"
	AuditUpdated     = "APP_DOWNLOAD_UPDATED"
	AuditActivated   = "APP_DOWNLOAD_ACTIVATED"
	AuditDeactivated = "APP_DOWNLOAD_DEACTIVATED"
)

// AllowedHosts: domain Google Drive yang diterima (§21).
var AllowedHosts = []string{"drive.google.com", "docs.google.com", "drive.usercontent.google.com"}

type Service struct {
	DB *db.DB
}

func New(d *db.DB) *Service { return &Service{DB: d} }

type AppDownload struct {
	ID            uuid.UUID  `json:"id"`
	Name          string     `json:"name"`
	AppType       string     `json:"app_type"` // staff | tenant | customer
	Platform      string     `json:"platform"` // android | ios | other
	DownloadURL   string     `json:"download_url"`
	Status        string     `json:"status"` // active | inactive
	Notes         *string    `json:"notes"`
	CreatedAt     time.Time  `json:"created_at"`
	CreatedBy     *uuid.UUID `json:"created_by"`
	UpdatedAt     time.Time  `json:"updated_at"`
	UpdatedBy     *uuid.UUID `json:"updated_by"`
	UpdatedByName string     `json:"updated_by_name"`
	Version       int        `json:"version"`
}

// PublicApp: representasi publik (§22) — tanpa metadata internal.
type PublicApp struct {
	Name        string `json:"name"`
	AppType     string `json:"app_type"`
	Platform    string `json:"platform"`
	DownloadURL string `json:"downloadUrl"`
	Status      string `json:"status"`
}

type Input struct {
	Name        *string `json:"name"`
	AppType     *string `json:"app_type"`
	Platform    *string `json:"platform"`
	DownloadURL *string `json:"download_url"`
	Status      *string `json:"status"`
	Notes       *string `json:"notes"`
}

// ValidateDriveURL: hanya https + host Google Drive (§21). Tidak mengunduh/memeriksa file.
func ValidateDriveURL(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return apperr.Validation("Download URL wajib diisi").WithField("download_url", "wajib")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return apperr.Validation("Download URL harus berupa tautan https Google Drive").WithField("download_url", "format tidak valid")
	}
	host := strings.ToLower(u.Hostname())
	for _, h := range AllowedHosts {
		if host == h {
			return nil
		}
	}
	return apperr.Validation("Download URL harus berada di domain Google Drive (drive.google.com / docs.google.com)").WithField("download_url", "domain tidak diizinkan")
}

func validAppType(s string) bool  { return s == "staff" || s == "tenant" || s == "customer" }
func validPlatform(s string) bool { return s == "android" || s == "ios" || s == "other" }
func validStatus(s string) bool   { return s == "active" || s == "inactive" }

const selectCols = `a.id, a.name, a.app_type, a.platform, a.download_url, a.status, a.notes, a.created_at, a.created_by, a.updated_at, a.updated_by, coalesce(u.full_name,''), a.version`
const selectOne = `SELECT ` + selectCols + ` FROM app_downloads a LEFT JOIN users u ON u.id = a.updated_by WHERE a.id = $1`

func scan(row pgx.Row) (*AppDownload, error) {
	var a AppDownload
	if err := row.Scan(&a.ID, &a.Name, &a.AppType, &a.Platform, &a.DownloadURL, &a.Status, &a.Notes, &a.CreatedAt, &a.CreatedBy, &a.UpdatedAt, &a.UpdatedBy, &a.UpdatedByName, &a.Version); err != nil {
		if db.IsNoRows(err) {
			return nil, apperr.NotFound("App download")
		}
		return nil, err
	}
	return &a, nil
}

// List (admin): seluruh konfigurasi, urut app_type/platform.
func (s *Service) List(ctx context.Context) ([]AppDownload, error) {
	out := []AppDownload{}
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT `+selectCols+` FROM app_downloads a LEFT JOIN users u ON u.id = a.updated_by ORDER BY a.app_type, a.platform, a.created_at`)
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
	return out, err
}

func (s *Service) Get(ctx context.Context, id uuid.UUID) (*AppDownload, error) {
	var out *AppDownload
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		out, err = scan(tx.QueryRow(ctx, selectOne, id))
		return err
	})
	return out, err
}

func (s *Service) Create(ctx context.Context, in Input) (*AppDownload, error) {
	p := authctx.Must(ctx)
	name := strings.TrimSpace(deref(in.Name))
	if name == "" {
		return nil, apperr.Validation("App Name wajib diisi").WithField("name", "wajib")
	}
	appType := strings.ToLower(deref(in.AppType))
	if !validAppType(appType) {
		return nil, apperr.Validation("App Type harus staff|tenant|customer").WithField("app_type", "tidak valid")
	}
	platform := strings.ToLower(deref(in.Platform))
	if !validPlatform(platform) {
		return nil, apperr.Validation("Platform harus android|ios|other").WithField("platform", "tidak valid")
	}
	if err := ValidateDriveURL(deref(in.DownloadURL)); err != nil {
		return nil, err
	}
	status := "inactive"
	if in.Status != nil {
		status = strings.ToLower(*in.Status)
		if !validStatus(status) {
			return nil, apperr.Validation("Status harus active|inactive").WithField("status", "tidak valid")
		}
	}
	var out *AppDownload
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `INSERT INTO app_downloads (name, app_type, platform, download_url, status, notes, created_by, updated_by) VALUES ($1,$2,$3,$4,$5,$6,$7,$7) RETURNING id`,
			name, appType, platform, strings.TrimSpace(*in.DownloadURL), status, in.Notes, p.UserID).Scan(&id); err != nil {
			return err
		}
		var err error
		out, err = scan(tx.QueryRow(ctx, selectOne, id))
		if err != nil {
			return err
		}
		return audit.Log(ctx, tx, audit.AuditEntry{Action: AuditCreated, EntityType: "app_download", EntityID: &id, EntityLabel: name,
			After: map[string]any{"app": name, "app_type": appType, "platform": platform, "new_url": out.DownloadURL, "status": status, "role": p.RoleCodes}})
	})
	return out, err
}

// Update: ubah nama/platform/URL/status (§17, §20). Perubahan status tercatat sebagai ACTIVATED/DEACTIVATED.
func (s *Service) Update(ctx context.Context, id uuid.UUID, in Input, ifVersion *int) (*AppDownload, error) {
	p := authctx.Must(ctx)
	if in.DownloadURL != nil {
		if err := ValidateDriveURL(*in.DownloadURL); err != nil {
			return nil, err
		}
	}
	if in.AppType != nil && !validAppType(strings.ToLower(*in.AppType)) {
		return nil, apperr.Validation("App Type harus staff|tenant|customer").WithField("app_type", "tidak valid")
	}
	if in.Platform != nil && !validPlatform(strings.ToLower(*in.Platform)) {
		return nil, apperr.Validation("Platform harus android|ios|other").WithField("platform", "tidak valid")
	}
	if in.Status != nil && !validStatus(strings.ToLower(*in.Status)) {
		return nil, apperr.Validation("Status harus active|inactive").WithField("status", "tidak valid")
	}
	if in.Name != nil && strings.TrimSpace(*in.Name) == "" {
		return nil, apperr.Validation("App Name wajib diisi").WithField("name", "wajib")
	}
	var out *AppDownload
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		before, err := scan(tx.QueryRow(ctx, selectOne+` FOR UPDATE OF a`, id))
		if err != nil {
			return err
		}
		if ifVersion != nil && *ifVersion != before.Version {
			return apperr.StaleVersion()
		}
		name, appType, platform, urlv, status, notes := before.Name, before.AppType, before.Platform, before.DownloadURL, before.Status, before.Notes
		if in.Name != nil {
			name = strings.TrimSpace(*in.Name)
		}
		if in.AppType != nil {
			appType = strings.ToLower(*in.AppType)
		}
		if in.Platform != nil {
			platform = strings.ToLower(*in.Platform)
		}
		if in.DownloadURL != nil {
			urlv = strings.TrimSpace(*in.DownloadURL)
		}
		if in.Status != nil {
			status = strings.ToLower(*in.Status)
		}
		if in.Notes != nil {
			notes = in.Notes
		}
		if _, err := tx.Exec(ctx, `UPDATE app_downloads SET name=$2, app_type=$3, platform=$4, download_url=$5, status=$6, notes=$7, updated_by=$8 WHERE id = $1`,
			id, name, appType, platform, urlv, status, notes, p.UserID); err != nil {
			return err
		}
		out, err = scan(tx.QueryRow(ctx, selectOne, id))
		if err != nil {
			return err
		}
		action := AuditUpdated
		switch {
		case before.Status != status && status == "active":
			action = AuditActivated
		case before.Status != status && status == "inactive":
			action = AuditDeactivated
		}
		return audit.Log(ctx, tx, audit.AuditEntry{Action: action, EntityType: "app_download", EntityID: &id, EntityLabel: name,
			Before: map[string]any{"app": before.Name, "platform": before.Platform, "previous_url": before.DownloadURL, "status": before.Status},
			After:  map[string]any{"app": name, "platform": platform, "new_url": urlv, "status": status, "role": p.RoleCodes}})
	})
	return out, err
}

// SetStatus: aksi Activate / Deactivate (§44).
func (s *Service) SetStatus(ctx context.Context, id uuid.UUID, status string) (*AppDownload, error) {
	return s.Update(ctx, id, Input{Status: &status}, nil)
}

// ListPublic: hanya tautan aktif; tanpa data internal (§22). Dipanggil tanpa principal (public website).
func (s *Service) ListPublic(ctx context.Context) ([]PublicApp, error) {
	rows, err := s.DB.Pool.Query(ctx, `SELECT name, app_type, platform, download_url FROM app_downloads WHERE status = 'active' ORDER BY app_type, platform, created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PublicApp{}
	for rows.Next() {
		var a PublicApp
		if err := rows.Scan(&a.Name, &a.AppType, &a.Platform, &a.DownloadURL); err != nil {
			return nil, err
		}
		a.Status = "active"
		out = append(out, a)
	}
	return out, rows.Err()
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
