package authctx

import (
	"strings"
	"testing"

	"github.com/google/uuid"
)

// PRD P0 v2 §8.4: scope Organization · Property · Building/Tower.
func TestScopeGrants(t *testing.T) {
	propA, propB := uuid.New(), uuid.New()
	tower := uuid.New()
	perms := func(p ...string) map[string]struct{} {
		m := map[string]struct{}{}
		for _, x := range p {
			m[x] = struct{}{}
		}
		return m
	}
	p := &Principal{Grants: []PropertyGrant{
		{PropertyID: &propA, Permissions: perms("operations.tasks.view")},
		{PropertyID: &propB, ScopeLocationID: &tower, ScopePath: "p_b.bld.twr_a", Permissions: perms("operations.work_orders.*")},
	}}
	if !p.HasOnProperty("operations.tasks.view", propA) || p.HasOnProperty("operations.tasks.view", propB) {
		t.Fatal("property-wide grant")
	}
	// grant ber-scope tidak dihitung sebagai property-wide
	if p.HasOnProperty("operations.work_orders.view", propB) {
		t.Fatal("scoped grant tidak boleh lolos HasOnProperty")
	}
	if !p.HasOnLocation("operations.work_orders.view", propB, "p_b.bld.twr_a.fl_12") {
		t.Fatal("lokasi di dalam subtree tower harus lolos")
	}
	if !p.HasOnLocation("operations.work_orders.view", propB, "p_b.bld.twr_a") {
		t.Fatal("lokasi scope itu sendiri harus lolos")
	}
	if p.HasOnLocation("operations.work_orders.view", propB, "p_b.bld.twr_ab") {
		t.Fatal("prefix label bukan subtree (twr_ab) tidak boleh lolos")
	}
	if p.HasOnLocation("operations.work_orders.view", propB, "p_b.bld.twr_b.fl_3") || p.HasOnLocation("operations.work_orders.view", propB, "") {
		t.Fatal("lokasi di luar scope / tanpa lokasi tidak boleh lolos")
	}
	if !p.HasAnyOnProperty("operations.work_orders.view", propB) || p.HasAnyOnProperty("operations.work_orders.view", propA) {
		t.Fatal("HasAnyOnProperty")
	}
	loaded := false
	if p.HasOnPropertyAt("operations.tasks.view", propA, func() string { loaded = true; return "" }); loaded {
		t.Fatal("path tidak perlu dimuat bila grant property-wide sudah cukup")
	}
	ids, all := p.PropertyIDsFor("operations.work_orders.view")
	if all || len(ids) != 0 {
		t.Fatalf("PropertyIDsFor tidak boleh memuat grant ber-scope: %v %v", ids, all)
	}
	var args []any
	add := func(v any) string { args = append(args, v); return "$" + string(rune('0'+len(args))) }
	sql := p.ScopeSQL("operations.work_orders.view", "w.property_id", "lp", add)
	if !strings.Contains(sql, "<@ ANY") || strings.Contains(sql, "w.property_id = ANY") {
		t.Fatalf("ScopeSQL building-only: %s", sql)
	}
	if got := p.ScopeSQL("operations.incidents.view", "w.property_id", "lp", add); got != "FALSE" {
		t.Fatalf("tanpa grant → FALSE, got %s", got)
	}
	org := &Principal{Grants: []PropertyGrant{{Permissions: perms("*")}}}
	if got := org.ScopeSQL("operations.tasks.view", "t.property_id", "lp", add); got != "TRUE" {
		t.Fatalf("org-wide → TRUE, got %s", got)
	}
	if !(&Principal{IsSystem: true}).HasOnLocation("x.y.z", propA, "") {
		t.Fatal("system principal")
	}
}
