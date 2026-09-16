package tenantapp

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/operations"
	"github.com/buildingvision/api/internal/operations/workflow"
	"github.com/buildingvision/api/internal/platform/authctx"
	"github.com/buildingvision/api/internal/platform/events"
	"github.com/buildingvision/api/internal/tenantservice"
)

// AutoCloseSweep (worker): Service Request berstatus Resolved yang tidak dikonfirmasi/dibuka kembali tenant dalam
// `auto_close_resolved_hours` (konfigurasi profile property; 0 = nonaktif) ditutup otomatis oleh sistem (PRD §17, WF-P1-003).
// Transisi lewat Task Engine (audit + event service_request.closed); tambahan event service_request.auto_closed → notifikasi tenant.
func (s *Service) AutoCloseSweep(ctx context.Context, orgID uuid.UUID) (int, error) {
	ctx = authctx.With(ctx, authctx.System(orgID))
	type row struct {
		id         uuid.UUID
		propertyID uuid.UUID
		number     string
		tenantUser *uuid.UUID
		hours      int
	}
	var due []row
	err := s.DB.WithOrgTx(ctx, orgID, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT sr.id, sr.property_id, sr.request_number, sr.tenant_user_id, COALESCE(c.auto_close_resolved_hours, 72)
			FROM service_requests sr LEFT JOIN property_profile_configs c ON c.property_id = sr.property_id
			WHERE sr.status = 'resolved' AND sr.resolved_at IS NOT NULL AND COALESCE(c.auto_close_resolved_hours, 72) > 0
			  AND sr.resolved_at + make_interval(hours => COALESCE(c.auto_close_resolved_hours, 72)) < now()
			ORDER BY sr.resolved_at LIMIT 200`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var r row
			if err := rows.Scan(&r.id, &r.propertyID, &r.number, &r.tenantUser, &r.hours); err != nil {
				return err
			}
			due = append(due, r)
		}
		return rows.Err()
	})
	if err != nil || len(due) == 0 {
		return 0, err
	}
	n := 0
	for _, r := range due {
		err := s.DB.WithOrgTx(ctx, orgID, func(ctx context.Context, tx pgx.Tx) error {
			if err := s.SR.TransitionTx(ctx, tx, r.id, workflow.ActClose, tenantservice.TransitionInput{Reason: fmt.Sprintf("Ditutup otomatis: tidak ada konfirmasi tenant dalam %d jam", r.hours)}, false); err != nil {
				return err
			}
			if s.Jobs != nil {
				payload := map[string]any{"from": "resolved", "to": "closed", "actor_kind": "system", "auto_close_hours": r.hours}
				if r.tenantUser != nil {
					payload["tenant_user_id"] = *r.tenantUser
				}
				_ = s.Jobs.EnqueueEventTx(ctx, tx, events.Event{Type: "service_request.auto_closed", OrganizationID: orgID, PropertyID: &r.propertyID, ObjectType: operations.ObjServiceRequest, ObjectID: r.id, ObjectLabel: r.number, Payload: payload})
			}
			return nil
		})
		if err == nil {
			n++
		}
	}
	return n, nil
}
