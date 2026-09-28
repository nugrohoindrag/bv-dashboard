package app_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// PRD P2 v2.1 GAP-P2-09: dashboard per domain (P2-ENG-01..04, P2-SDB-01..02, P2-HDB-01..05) + Security Report (P2-SDB-03).
// Setiap KPI punya drill-down (Principle 9); scope permission domain + filter building.

type domainDash struct {
	Domain string `json:"domain"`
	KPIs   []struct {
		Key       string  `json:"key"`
		Value     float64 `json:"value"`
		Unit      string  `json:"unit"`
		Severity  string  `json:"severity"`
		Hint      string  `json:"hint"`
		DrillDown string  `json:"drill_down"`
	} `json:"kpis"`
	Distribution map[string]int `json:"distribution"`
	Breakdowns   map[string][]struct {
		Key    string             `json:"key"`
		Label  string             `json:"label"`
		Values map[string]float64 `json:"values"`
	} `json:"breakdowns"`
	Attention []struct {
		Category string `json:"category"`
		Title    string `json:"title"`
	} `json:"attention"`
}

func (d domainDash) kpi(t *testing.T, key string) (float64, string) {
	t.Helper()
	for _, k := range d.KPIs {
		if k.Key == key {
			return k.Value, k.Severity
		}
	}
	t.Fatalf("KPI %s tidak ada di dashboard %s", key, d.Domain)
	return 0, ""
}

func (d domainDash) check(t *testing.T, keys ...string) {
	t.Helper()
	for _, k := range d.KPIs {
		if k.DrillDown == "" || k.Hint == "" || k.Unit == "" {
			t.Fatalf("KPI %s wajib punya drill-down, definisi, dan unit", k.Key)
		}
	}
	for _, key := range keys {
		d.kpi(t, key)
	}
}

func TestP2v21DomainDashboards(t *testing.T) {
	e := setup(t)
	engMgr := e.login("eng.manager@demo.buildingvision.id")
	engSpv := e.login("eng.spv@demo.buildingvision.id")
	secSpv := e.login("sec.spv@demo.buildingvision.id")
	officer := e.login("wawan@demo.buildingvision.id")
	hkSpv := e.login("hk.spv@demo.buildingvision.id")
	tech := e.login("budi@demo.buildingvision.id")
	bm := e.login("bm@demo.buildingvision.id")
	pid := e.refs.PropertyID.String()

	// ---- akses ----
	if st, _ := e.do(tech, http.MethodGet, "/api/v1/dashboards/engineering", nil); st != 403 {
		t.Fatalf("teknisi tanpa overview tidak boleh membuka dashboard: %d", st)
	}
	if st, _ := e.do(secSpv, http.MethodGet, "/api/v1/dashboards/engineering", nil); st != 403 {
		t.Fatalf("Security Supervisor tidak boleh membuka dashboard Engineering: %d", st)
	}
	if st, _ := e.do(engMgr, http.MethodGet, "/api/v1/dashboards/engineering?from=kemarin", nil); st != 400 {
		t.Fatalf("from tidak valid harus 400: %d", st)
	}

	// ---- Engineering (P2-ENG-01..04) ----
	var asset struct {
		ID uuid.UUID `json:"id"`
	}
	st, body := e.do(engMgr, http.MethodPost, "/api/v1/assets", map[string]any{"name": "AHU-31", "equipment_id": ahuEquipment(t, e, engMgr), "location_id": e.refs.MechRoomA12, "criticality": "high"})
	e.mustJSON(st, body, 201, &asset)
	st, body = e.do(engSpv, http.MethodPost, "/api/v1/work-orders", map[string]any{"work_order_type": "corrective", "title": "AHU-31 bocor", "asset_id": asset.ID, "priority": "critical", "assignee_team_id": e.refs.Teams["engineering"]})
	e.mustJSON(st, body, 201, nil)
	st, body = e.do(engSpv, http.MethodPost, "/api/v1/assets/"+asset.ID.String()+"/health/recompute", nil)
	if st != 200 && st != 403 {
		t.Fatalf("recompute: %d %s", st, body)
	}
	st, body = e.do(engMgr, http.MethodPost, "/api/v1/assets/"+asset.ID.String()+"/health/recompute", nil)
	e.mustJSON(st, body, 200, nil)
	var eng domainDash
	st, body = e.do(engMgr, http.MethodGet, "/api/v1/dashboards/engineering?property_id="+pid, nil)
	e.mustJSON(st, body, 200, &eng)
	eng.check(t, "pm_due", "pm_overdue", "pm_compliance", "open_corrective_work_orders", "critical_asset_count", "at_risk_asset_count", "maintenance_cost")
	if v, _ := eng.kpi(t, "open_corrective_work_orders"); v != 1 {
		t.Fatalf("open corrective WO: %s", body)
	}
	if v, _ := eng.kpi(t, "at_risk_asset_count"); v != 1 || eng.Distribution["warning"] != 1 {
		t.Fatalf("asset at-risk (health 75 warning) & distribusi: %s", body)
	}
	for _, key := range []string{"building", "equipment_category", "team"} {
		if len(eng.Breakdowns[key]) == 0 {
			t.Fatalf("breakdown %s kosong: %s", key, body)
		}
	}
	// filter building: WO di Tower A tidak dihitung saat filter Tower B
	st, body = e.do(engMgr, http.MethodGet, "/api/v1/dashboards/engineering?property_id="+pid+"&location_id="+e.refs.TowerB.String(), nil)
	e.mustJSON(st, body, 200, &eng)
	if v, _ := eng.kpi(t, "open_corrective_work_orders"); v != 0 {
		t.Fatalf("filter building Tower B: %s", body)
	}

	// ---- Security (P2-SDB-01..02) ----
	st, body = e.do(secSpv, http.MethodPost, "/api/v1/incidents", map[string]any{"title": "Orang tak dikenal di lantai 12", "category": "unauthorized_access", "severity": "critical", "location_id": e.refs.LobbyA})
	e.mustJSON(st, body, 201, nil)
	st, body = e.do(officer, http.MethodPost, "/api/v1/emergency-alerts", map[string]any{"emergency_type": "fire", "location_id": e.refs.MechRoomA12, "channel": "panic_button"})
	e.mustJSON(st, body, 201, nil)
	var sec domainDash
	st, body = e.do(secSpv, http.MethodGet, "/api/v1/dashboards/security?property_id="+pid, nil)
	e.mustJSON(st, body, 200, &sec)
	sec.check(t, "incidents_today", "open_incidents", "critical_incidents", "patrol_completion", "checkpoint_compliance", "security_response_time", "emergency_events", "visitor_volume", "unauthorized_entry", "active_emergencies")
	if v, sev := sec.kpi(t, "active_emergencies"); v != 1 || sev != "critical" {
		t.Fatalf("emergency aktif di Security Dashboard (P2-EMG-09): %s", body)
	}
	if v, _ := sec.kpi(t, "unauthorized_entry"); v != 1 {
		t.Fatalf("unauthorized entry: %s", body)
	}
	emgTitle := ""
	for _, a := range sec.Attention {
		if a.Category == "active_emergency" {
			emgTitle = a.Title
		}
	}
	if !strings.Contains(emgTitle, "Kebakaran") {
		t.Fatalf("judul attention emergency harus memakai label jenis (bukan kode): %q", emgTitle)
	}

	// ---- Housekeeping (P2-HDB-01..05) ----
	st, body = e.do(hkSpv, http.MethodPost, "/api/v1/cleaning-tasks", map[string]any{"title": "Spot cleaning lobby", "location_id": e.refs.LobbyA, "assignee_team_id": e.refs.Teams["housekeeping"], "cleaning_type": "spot"})
	e.mustJSON(st, body, 201, nil)
	var hk domainDash
	st, body = e.do(hkSpv, http.MethodGet, "/api/v1/dashboards/housekeeping?property_id="+pid, nil)
	e.mustJSON(st, body, 200, &hk)
	hk.check(t, "cleaning_tasks_today", "completion_rate", "inspection_score", "finding_rate", "rework_rate", "staff_on_duty", "area_condition", "cleaning_sla_compliance")
	if v, _ := hk.kpi(t, "cleaning_tasks_today"); v < 1 {
		t.Fatalf("cleaning tasks today: %s", body)
	}

	// ---- Security Report (P2-SDB-03) ----
	var rep struct {
		Summary    map[string]float64 `json:"summary"`
		Breakdowns map[string][]any   `json:"breakdowns"`
	}
	st, body = e.do(bm, http.MethodGet, "/api/v1/reports/security?property_id="+pid, nil)
	e.mustJSON(st, body, 200, &rep)
	for _, k := range []string{"patrol_completion_pct", "checkpoint_compliance_pct", "checkpoints_missed", "incidents_reported", "avg_response_minutes", "emergencies_raised", "visitors_checked_in"} {
		if _, ok := rep.Summary[k]; !ok {
			t.Fatalf("security report summary %s: %s", k, body)
		}
	}
	if rep.Summary["emergencies_raised"] != 1 || rep.Summary["unauthorized_entry"] != 1 {
		t.Fatalf("security report emergency & unauthorized entry: %s", body)
	}
}
