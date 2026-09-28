package workforce

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/platform/apperr"
	"github.com/buildingvision/api/internal/platform/authctx"
)

// DomainStaff: anggota team domain (security/housekeeping) — pilihan staf roster, penerima serah terima, petugas route
// bagi supervisor domain yang tidak memiliki iam.users.view.
type DomainStaff struct {
	UserID    uuid.UUID   `json:"user_id"`
	FullName  string      `json:"full_name"`
	TeamIDs   []uuid.UUID `json:"team_ids"`
	TeamNames []string    `json:"team_names"`
}

func (s *Service) ListDomainStaff(ctx context.Context, domain string, propertyID, teamID *uuid.UUID) ([]DomainStaff, error) {
	if err := validDomain(domain); err != nil {
		return nil, err
	}
	p := authctx.Must(ctx)
	out := []DomainStaff{}
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		args := []any{domain}
		add := func(v any) string { args = append(args, v); return fmt.Sprintf("$%d", len(args)) }
		where := " WHERE t.domain = $1 AND t.is_active AND t.deleted_at IS NULL AND u.is_active AND u.deleted_at IS NULL"
		if propertyID != nil {
			if !p.HasAnyOnProperty(shiftPerm(domain, "view"), *propertyID) {
				return apperr.Forbidden("")
			}
			where += " AND t.property_id = " + add(*propertyID)
		}
		if teamID != nil {
			where += " AND t.id = " + add(*teamID)
		}
		where += " AND " + p.ScopeSQL(shiftPerm(domain, "view"), "t.property_id", "", add)
		rows, err := tx.Query(ctx, `SELECT u.id, u.full_name, array_agg(t.id ORDER BY t.name), array_agg(t.name ORDER BY t.name)
			FROM team_members tm JOIN teams t ON t.id = tm.team_id JOIN users u ON u.id = tm.user_id`+where+`
			GROUP BY u.id, u.full_name ORDER BY u.full_name LIMIT 500`, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var x DomainStaff
			if err := rows.Scan(&x.UserID, &x.FullName, &x.TeamIDs, &x.TeamNames); err != nil {
				return err
			}
			out = append(out, x)
		}
		return rows.Err()
	})
	return out, err
}
