package demo

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/platform/apperr"
)

// Reset (§32, §39): hapus SELURUH data organization demo (semua tabel org-scoped, urutan FK ditangani otomatis) lalu
// organization-nya. Hanya organization ber-marker `settings.demo` (slug `demo`) yang boleh dihapus — data organization
// lain tidak pernah disentuh. Bila `profiles` sebagian, environment profile lain yang sebelumnya ada di-seed ulang
// (ketiga profile berbagi satu organization demo agar Property Switcher & BVRooms white-label konsisten).
type ResetResult struct {
	DeletedOrg  bool          `json:"deleted_org"`
	TablesWiped int           `json:"tables_wiped"`
	RowsDeleted int64         `json:"rows_deleted"`
	Reseeded    []string      `json:"reseeded"`
	Took        time.Duration `json:"took"`
	State       *State        `json:"state"`
	Log         []string      `json:"log"`
}

func (s *Service) Reset(ctx context.Context, profiles []string) (*ResetResult, error) {
	if !s.Enabled() {
		return nil, apperr.Forbidden("Demo tooling dinonaktifkan pada environment ini")
	}
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return nil, apperr.Conflict("DEMO_BUSY", "Operasi demo lain sedang berjalan")
	}
	s.running = true
	s.mu.Unlock()
	start := time.Now()
	res := &ResetResult{}
	logf := func(format string, a ...any) {
		msg := fmt.Sprintf(format, a...)
		res.Log = append(res.Log, msg)
		s.Log.Info("demo reset", "msg", msg)
	}
	orgID, m, err := s.readMarker(ctx)
	if err != nil {
		s.mu.Lock()
		s.running = false
		s.mu.Unlock()
		return nil, err
	}
	if orgID == nil {
		s.mu.Lock()
		s.running = false
		s.mu.Unlock()
		res.State, _ = s.Status(ctx)
		logf("organization demo belum ada — tidak ada yang dihapus")
		return res, nil
	}
	// profile yang perlu di-seed ulang setelah reset (bila reset sebagian)
	var reseed []string
	if len(profiles) > 0 && len(profiles) < len(Profiles) {
		req := map[string]bool{}
		for _, p := range profiles {
			req[p] = true
		}
		for _, p := range Profiles {
			if ps, ok := m.Profiles[p]; ok && ps.PropertyID != nil && !req[p] {
				reseed = append(reseed, p)
			}
		}
	}
	m.State = "resetting"
	_ = s.writeMarker(ctx, *orgID, m)
	tables, rows, err := s.wipeOrganization(ctx, *orgID)
	s.mu.Lock()
	s.running = false
	s.mu.Unlock()
	if err != nil {
		m.State, m.Error = "failed", "reset: "+err.Error()
		_ = s.writeMarker(ctx, *orgID, m)
		return nil, fmt.Errorf("demo reset: %w", err)
	}
	res.DeletedOrg, res.TablesWiped, res.RowsDeleted = true, tables, rows
	logf("organization demo dihapus: %d tabel, %d baris", tables, rows)
	if len(reseed) > 0 {
		sort.Strings(reseed)
		if _, err := s.Seed(ctx, reseed); err != nil {
			return nil, err
		}
		res.Reseeded = reseed
		logf("profile lain di-seed ulang: %v", reseed)
	}
	res.Took = time.Since(start)
	res.State, _ = s.Status(ctx)
	return res, nil
}

// wipeOrganization: hapus baris seluruh tabel yang memiliki organization_id (dan tabel anak tanpa organization_id lewat FK),
// beberapa pass sampai tidak ada pelanggaran FK, lalu hapus organization. Refuse bila organization bukan demo.
func (s *Service) wipeOrganization(ctx context.Context, orgID uuid.UUID) (int, int64, error) {
	var slug string
	var hasMarker bool
	if err := s.DB.Pool.QueryRow(ctx, `SELECT slug, settings ? 'demo' FROM organizations WHERE id = $1`, orgID).Scan(&slug, &hasMarker); err != nil {
		return 0, 0, err
	}
	if slug != OrgSlug || !hasMarker {
		return 0, 0, apperr.Forbidden("Reset hanya diizinkan untuk organization demo (marker settings.demo)")
	}
	type fkChild struct{ table, col, parent string }
	var orgTables []string
	var children []fkChild
	err := s.DB.Pool.QueryRow(ctx, `SELECT 1`).Scan(new(int))
	if err != nil {
		return 0, 0, err
	}
	rows, err := s.DB.Pool.Query(ctx, `SELECT c.table_name FROM information_schema.columns c JOIN information_schema.tables t ON t.table_name = c.table_name AND t.table_schema = c.table_schema
		WHERE c.table_schema = 'public' AND c.column_name = 'organization_id' AND t.table_type = 'BASE TABLE' AND c.table_name <> 'organizations' ORDER BY c.table_name`)
	if err != nil {
		return 0, 0, err
	}
	for rows.Next() {
		var t string
		_ = rows.Scan(&t)
		orgTables = append(orgTables, t)
	}
	rows.Close()
	isOrg := map[string]bool{}
	for _, t := range orgTables {
		isOrg[t] = true
	}
	// tabel anak tanpa organization_id yang ber-FK ke tabel org-scoped
	rows, err = s.DB.Pool.Query(ctx, `SELECT tc.table_name, kcu.column_name, ccu.table_name AS parent
		FROM information_schema.table_constraints tc
		JOIN information_schema.key_column_usage kcu ON kcu.constraint_name = tc.constraint_name AND kcu.table_schema = tc.table_schema
		JOIN information_schema.constraint_column_usage ccu ON ccu.constraint_name = tc.constraint_name AND ccu.table_schema = tc.table_schema
		WHERE tc.constraint_type = 'FOREIGN KEY' AND tc.table_schema = 'public'`)
	if err != nil {
		return 0, 0, err
	}
	for rows.Next() {
		var c fkChild
		_ = rows.Scan(&c.table, &c.col, &c.parent)
		if !isOrg[c.table] && isOrg[c.parent] {
			children = append(children, c)
		}
	}
	rows.Close()
	var total int64
	wiped := map[string]bool{}
	err = s.DB.WithOrgTx(ctx, orgID, func(ctx context.Context, tx pgx.Tx) error {
		// 1) anak tanpa organization_id (mis. user_roles, team_members, unit_occupants, patrol_route_checkpoints)
		for _, c := range children {
			parentKey := "id"
			if c.parent == "units" || c.parent == "properties" || c.parent == "hotel_rooms" {
				parentKey = "location_id"
			}
			if _, err := tx.Exec(ctx, "SAVEPOINT sp"); err != nil {
				return err
			}
			tag, err := tx.Exec(ctx, fmt.Sprintf(`DELETE FROM %s WHERE %s IN (SELECT %s FROM %s WHERE organization_id = $1)`, c.table, c.col, parentKey, c.parent), orgID)
			if err != nil {
				_, _ = tx.Exec(ctx, "ROLLBACK TO SAVEPOINT sp")
				continue
			}
			_, _ = tx.Exec(ctx, "RELEASE SAVEPOINT sp")
			total += tag.RowsAffected()
			if tag.RowsAffected() > 0 {
				wiped[c.table] = true
			}
		}
		// FK melingkar (work_orders ↔ maintenance_schedules) diputus dulu
		_, _ = tx.Exec(ctx, `UPDATE maintenance_schedules SET work_order_id = NULL WHERE organization_id = $1`, orgID)
		_, _ = tx.Exec(ctx, `UPDATE work_orders SET maintenance_schedule_id = NULL WHERE organization_id = $1`, orgID)
		// 2) tabel org-scoped: ulang sampai semua kosong (FK RESTRICT/NO ACTION menunggu anaknya terhapus)
		remaining := append([]string(nil), orgTables...)
		lastErr := map[string]string{}
		for pass := 0; pass < 25 && len(remaining) > 0; pass++ {
			var next []string
			for _, t := range remaining {
				if _, err := tx.Exec(ctx, "SAVEPOINT sp"); err != nil {
					return err
				}
				tag, err := tx.Exec(ctx, fmt.Sprintf(`DELETE FROM %s WHERE organization_id = $1`, t), orgID)
				if err != nil {
					_, _ = tx.Exec(ctx, "ROLLBACK TO SAVEPOINT sp")
					lastErr[t] = shortErr(err)
					next = append(next, t)
					continue
				}
				_, _ = tx.Exec(ctx, "RELEASE SAVEPOINT sp")
				total += tag.RowsAffected()
				if tag.RowsAffected() > 0 {
					wiped[t] = true
				}
			}
			remaining = next
		}
		if len(remaining) > 0 {
			return fmt.Errorf("tabel tidak dapat dikosongkan (FK): %v — %v", remaining, lastErr)
		}
		if _, err := tx.Exec(ctx, `DELETE FROM organizations WHERE id = $1 AND slug = $2`, orgID, OrgSlug); err != nil {
			return fmt.Errorf("hapus organization: %w", err)
		}
		return nil
	})
	if err != nil {
		return 0, 0, err
	}
	s.orgCacheInvalidate()
	return len(wiped), total, nil
}

func (s *Service) orgCacheInvalidate() {
	// principal cache IAM per user — user demo sudah terhapus; cache kedaluwarsa sendiri (login berikutnya gagal 401)
}
