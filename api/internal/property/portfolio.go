package property

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/audit"
	"github.com/buildingvision/api/internal/platform/apperr"
	"github.com/buildingvision/api/internal/platform/authctx"
	"github.com/buildingvision/api/internal/platform/db"
	"github.com/buildingvision/api/internal/platform/ids"
)

// ---------- Portfolio (PRD P0 v2 §4.1): Organization → Portfolio → Property ----------

type Portfolio struct {
	ID            uuid.UUID `json:"id"`
	Code          string    `json:"code"`
	Name          string    `json:"name"`
	Description   *string   `json:"description"`
	IsActive      bool      `json:"is_active"`
	PropertyCount int       `json:"property_count"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
	Version       int       `json:"version"`
}

type PortfolioInput struct {
	Code        *string `json:"code"`
	Name        *string `json:"name"`
	Description *string `json:"description"`
	IsActive    *bool   `json:"is_active"`
}

const portfolioSelect = `SELECT pf.id, pf.code, pf.name, pf.description, pf.is_active,
	(SELECT count(*) FROM properties pr JOIN locations l ON l.id = pr.location_id AND l.deleted_at IS NULL WHERE pr.portfolio_id = pf.id),
	pf.created_at, pf.updated_at, pf.version FROM portfolios pf`

func scanPortfolio(row pgx.Row) (*Portfolio, error) {
	var pf Portfolio
	if err := row.Scan(&pf.ID, &pf.Code, &pf.Name, &pf.Description, &pf.IsActive, &pf.PropertyCount, &pf.CreatedAt, &pf.UpdatedAt, &pf.Version); err != nil {
		if db.IsNoRows(err) {
			return nil, apperr.NotFound("Portfolio")
		}
		return nil, err
	}
	return &pf, nil
}

func (s *Service) ListPortfolios(ctx context.Context, includeInactive bool) ([]Portfolio, error) {
	out := []Portfolio{}
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		where := ` WHERE pf.deleted_at IS NULL`
		if !includeInactive {
			where += ` AND pf.is_active`
		}
		rows, err := tx.Query(ctx, portfolioSelect+where+` ORDER BY pf.name`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			pf, err := scanPortfolio(rows)
			if err != nil {
				return err
			}
			out = append(out, *pf)
		}
		return rows.Err()
	})
	return out, err
}

func (s *Service) GetPortfolio(ctx context.Context, id uuid.UUID) (*Portfolio, error) {
	var out *Portfolio
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		out, err = scanPortfolio(tx.QueryRow(ctx, portfolioSelect+` WHERE pf.id = $1 AND pf.deleted_at IS NULL`, id))
		return err
	})
	return out, err
}

func (s *Service) CreatePortfolio(ctx context.Context, in PortfolioInput) (*Portfolio, error) {
	p := authctx.Must(ctx)
	if in.Name == nil || strings.TrimSpace(*in.Name) == "" {
		return nil, apperr.Validation("name wajib").WithField("name", "wajib")
	}
	var out *Portfolio
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		code := ""
		if in.Code != nil {
			code = strings.ToUpper(strings.TrimSpace(*in.Code))
		}
		if code == "" {
			c, err := ids.NextPlain(ctx, tx, p.OrganizationID, ids.PrefixPortfolio)
			if err != nil {
				return err
			}
			code = c
		}
		var id uuid.UUID
		err := tx.QueryRow(ctx, `INSERT INTO portfolios (organization_id, code, name, description, created_by, updated_by) VALUES ($1,$2,$3,$4,$5,$5) RETURNING id`,
			p.OrganizationID, code, strings.TrimSpace(*in.Name), in.Description, p.UserID).Scan(&id)
		if err != nil {
			if db.IsUniqueViolation(err) {
				return apperr.Conflict("DUPLICATE_CODE", "Kode portfolio sudah dipakai")
			}
			return err
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditCreate, EntityType: "portfolio", EntityID: &id, EntityLabel: code, After: in})
		out, err = scanPortfolio(tx.QueryRow(ctx, portfolioSelect+` WHERE pf.id = $1`, id))
		return err
	})
	return out, err
}

func (s *Service) UpdatePortfolio(ctx context.Context, id uuid.UUID, in PortfolioInput, ifVersion *int) (*Portfolio, error) {
	p := authctx.Must(ctx)
	var out *Portfolio
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		before, err := scanPortfolio(tx.QueryRow(ctx, portfolioSelect+` WHERE pf.id = $1 AND pf.deleted_at IS NULL`, id))
		if err != nil {
			return err
		}
		if ifVersion != nil && *ifVersion != before.Version {
			return apperr.StaleVersion()
		}
		if _, err := tx.Exec(ctx, `UPDATE portfolios SET name = COALESCE(NULLIF(trim($2),''), name), description = COALESCE($3, description),
			is_active = COALESCE($4, is_active), code = COALESCE(NULLIF(upper(trim($5)),''), code), updated_by = $6 WHERE id = $1`,
			id, in.Name, in.Description, in.IsActive, in.Code, p.UserID); err != nil {
			if db.IsUniqueViolation(err) {
				return apperr.Conflict("DUPLICATE_CODE", "Kode portfolio sudah dipakai")
			}
			return err
		}
		out, err = scanPortfolio(tx.QueryRow(ctx, portfolioSelect+` WHERE pf.id = $1`, id))
		if err != nil {
			return err
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditUpdate, EntityType: "portfolio", EntityID: &id, EntityLabel: before.Code, Before: before, After: out})
		return nil
	})
	return out, err
}

// DeletePortfolio: soft delete; property di dalamnya dilepas (portfolio_id NULL), bukan dihapus.
func (s *Service) DeletePortfolio(ctx context.Context, id uuid.UUID) error {
	return s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		pf, err := scanPortfolio(tx.QueryRow(ctx, portfolioSelect+` WHERE pf.id = $1 AND pf.deleted_at IS NULL`, id))
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE properties SET portfolio_id = NULL WHERE portfolio_id = $1`, id); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE portfolios SET deleted_at = now(), is_active = false WHERE id = $1`, id); err != nil {
			return err
		}
		return audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditDelete, EntityType: "portfolio", EntityID: &id, EntityLabel: pf.Code, Before: pf})
	})
}
