package app_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/billing"
	"github.com/buildingvision/api/internal/platform/authctx"
	"github.com/buildingvision/api/internal/property"
)

// TestP4v21RunPerformance (opt-in: BV_PERF=1): PRD P4 v2.1 P4-NFR-05 — generate tagihan massal 1.000 unit < 1 menit
// (preview + draft invoice; service charge per m² + sinking fund %).
func TestP4v21RunPerformance(t *testing.T) {
	if os.Getenv("BV_PERF") != "1" {
		t.Skip("set BV_PERF=1 untuk menjalankan uji performa billing run")
	}
	e := setup(t)
	ctx := context.Background()
	const units = 1000
	sys := authctx.With(ctx, authctx.System(e.refs.OrgID))
	start := time.Now()
	if err := e.app.DB.WithOrgTx(sys, e.refs.OrgID, func(ctx context.Context, tx pgx.Tx) error {
		svc := &property.Service{}
		var tenant uuid.UUID
		if err := tx.QueryRow(ctx, `INSERT INTO tenants (organization_id, property_id, tenant_code, name, tenant_type) VALUES ($1,$2,'TEN-PERF','PT Perf','company') RETURNING id`, e.refs.OrgID, e.refs.PropertyID).Scan(&tenant); err != nil {
			return err
		}
		for i := 0; i < units; i++ {
			id, err := svc.CreateLocationInTx(ctx, tx, property.CreateLocationInput{LocationType: property.LTUnit, ParentID: &e.refs.FloorB3, Name: fmt.Sprintf("Unit P%04d", i),
				Details: map[string]any{"unit_number": fmt.Sprintf("P%04d", i), "unit_type": "residential", "area_m2": 36.0 + float64(i%40), "tenant_id": tenant.String(), "occupancy_status": "occupied"}})
			if err != nil {
				return err
			}
			_ = id
		}
		return nil
	}); err != nil {
		t.Fatalf("seed unit: %v", err)
	}
	t.Logf("seed %d unit: %s", units, time.Since(start))
	var pmID uuid.UUID
	if err := e.app.DB.WithOrgTx(sys, e.refs.OrgID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT id FROM users WHERE lower(email) = 'pm@demo.buildingvision.id'`).Scan(&pmID)
	}); err != nil {
		t.Fatal(err)
	}
	principal, err := e.app.IAM.LoadPrincipal(ctx, pmID, e.refs.OrgID)
	if err != nil {
		t.Fatal(err)
	}
	pm := authctx.With(ctx, principal)
	pid := e.refs.PropertyID
	base, err := e.app.Billing.SaveRule(pm, nil, billing.RuleInput{PropertyID: &pid, Code: ptrS("IPL"), Name: ptrS("IPL"), ChargeType: ptrS("ipl"), Basis: ptrS("per_area_m2"), Rate: ptrF(15000)})
	if err != nil {
		t.Fatal(err)
	}
	zero := 0.0
	if _, err := e.app.Billing.SaveRule(pm, nil, billing.RuleInput{PropertyID: &pid, Code: ptrS("SF"), Name: ptrS("Sinking Fund"), ChargeType: ptrS("sinking_fund"), Basis: ptrS("percentage"), Rate: ptrF(10), BaseRuleID: &base.ID, TaxRate: &zero}); err != nil {
		t.Fatal(err)
	}
	next := time.Now().AddDate(0, 1, 0).Format("2006-01")
	t0 := time.Now()
	run, err := e.app.Billing.CreateRun(pm, billing.RunInput{PropertyID: pid, Period: next})
	if err != nil {
		t.Fatal(err)
	}
	preview := time.Since(t0)
	t1 := time.Now()
	run, err = e.app.Billing.GenerateRun(pm, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	generate := time.Since(t1)
	t.Logf("billing run %d baris: preview %s, generate %d draft invoice %s, total %s", run.LineCount, preview, run.InvoiceCount, generate, preview+generate)
	if run.InvoiceCount < units || preview+generate > time.Minute {
		t.Fatalf("P4-NFR-05: %d invoice dalam %s (target ≥ %d invoice < 1 menit)", run.InvoiceCount, preview+generate, units)
	}
}

func ptrS(s string) *string   { return &s }
func ptrF(f float64) *float64 { return &f }
