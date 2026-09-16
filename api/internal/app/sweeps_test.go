package app_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/platform/authctx"
)

// P1.10 — sweep worker: auto-close Service Request resolved tanpa konfirmasi tenant (PRD §17, WF-P1-003),
// tamu kedaluwarsa (PRD §3.7), tagihan overdue (PRD §23) — semua berjalan sebagai system principal per organization.
func TestP1Sweeps(t *testing.T) {
	e := setup(t)
	tenA, _ := e.setupTenants(t)
	tr := e.login("tr.manager@demo.buildingvision.id")
	pm := e.login("pm@demo.buildingvision.id")
	ctx := context.Background()
	orgID := e.refs.OrgID
	sys := authctx.With(ctx, authctx.System(orgID))

	// auto_close_resolved_hours = 1 pada property demo
	st, body := e.do(pm, http.MethodPatch, "/api/v1/properties/"+e.refs.PropertyID.String()+"/profile-config", map[string]any{"auto_close_resolved_hours": 1})
	e.mustJSON(st, body, 200, nil)

	// ticket tenant → resolved → backdate resolved_at 2 jam → sweep → closed + notifikasi tenant
	var sr struct {
		ID           uuid.UUID `json:"id"`
		TenantStatus string    `json:"tenant_status"`
	}
	st, body = e.do(tenA, http.MethodPost, "/api/v1/tenant/requests", map[string]any{"category_code": "cleanliness", "title": "Koridor kotor", "description": "Koridor depan unit kotor sejak pagi tadi"})
	e.mustJSON(st, body, 201, &sr)
	st, body = e.do(tr, http.MethodPost, "/api/v1/service-requests/"+sr.ID.String()+"/acknowledge", nil)
	e.mustJSON(st, body, 200, nil)
	st, body = e.do(tr, http.MethodPost, "/api/v1/service-requests/"+sr.ID.String()+"/resolve", map[string]any{"resolution": "Sudah dibersihkan"})
	e.mustJSON(st, body, 200, nil)
	if n, err := e.app.TenantApp.AutoCloseSweep(sys, orgID); err != nil || n != 0 {
		t.Fatalf("sweep sebelum jatuh tempo harus 0: n=%d err=%v", n, err)
	}
	if err := e.app.DB.WithOrgTx(sys, orgID, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE service_requests SET resolved_at = now() - interval '2 hours' WHERE id = $1`, sr.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	n, err := e.app.TenantApp.AutoCloseSweep(sys, orgID)
	if err != nil || n != 1 {
		t.Fatalf("auto-close sweep: n=%d err=%v", n, err)
	}
	st, body = e.do(tenA, http.MethodGet, "/api/v1/tenant/requests/"+sr.ID.String(), nil)
	e.mustJSON(st, body, 200, &sr)
	if sr.TenantStatus != "closed" {
		t.Fatalf("ticket harus closed setelah auto-close: %+v", sr)
	}
	e.dispatch(t)
	if ib := e.inboxOf(t, tenA); !hasType(ib, "ticket_closed") {
		t.Fatalf("tenant harus menerima notifikasi ticket_closed: %+v", ib.Data)
	}
	// idempoten: sweep kedua tidak menemukan apa pun
	if n, _ := e.app.TenantApp.AutoCloseSweep(sys, orgID); n != 0 {
		t.Fatalf("sweep kedua harus 0, dapat %d", n)
	}

	// visitor: tamu terdaftar yang lewat batas → expired
	var vis struct {
		ID     uuid.UUID `json:"id"`
		Status string    `json:"status"`
	}
	st, body = e.do(tenA, http.MethodPost, "/api/v1/tenant/visitors", map[string]any{"visitor_name": "Tamu Telat", "expected_at": "2026-01-01T09:00:00Z", "expected_until": "2026-01-01T12:00:00Z"})
	if st == 201 {
		_ = json.Unmarshal(body, &vis)
		if n, err := e.app.Visitor.ExpireSweep(ctx, orgID); err != nil || n < 1 {
			t.Fatalf("visitor expire sweep: n=%d err=%v", n, err)
		}
		st, body = e.do(tenA, http.MethodGet, "/api/v1/tenant/visitors/"+vis.ID.String(), nil)
		e.mustJSON(st, body, 200, &vis)
		if vis.Status != "expired" {
			t.Fatalf("tamu harus expired: %+v", vis)
		}
	} else {
		// server menolak tanggal lampau → cukup pastikan sweep tidak error
		if _, err := e.app.Visitor.ExpireSweep(ctx, orgID); err != nil {
			t.Fatalf("visitor expire sweep: %v", err)
		}
	}

	// billing: sweep berjalan tanpa error (overdue/due-soon dicakup TestBillingAndPayment)
	if err := e.app.Billing.Sweep(ctx, orgID); err != nil {
		t.Fatalf("billing sweep: %v", err)
	}
}
