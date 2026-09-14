package app_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// dispatch: proses domain event yang antre di MemoryEnqueuer lewat subscriber (notification, search) — meniru worker.
func (e *env) dispatch(t *testing.T) {
	t.Helper()
	for e.dispatched < len(e.jobs.Events) {
		ev := e.jobs.Events[e.dispatched]
		e.dispatched++
		if err := e.app.Notification.Handle(context.Background(), ev); err != nil {
			t.Fatalf("notification handle %s: %v", ev.Type, err)
		}
		if err := e.app.Search.Handle(context.Background(), ev); err != nil {
			t.Fatalf("search handle %s: %v", ev.Type, err)
		}
	}
}

type inbox struct {
	Data []struct {
		ID       uuid.UUID `json:"id"`
		Type     string    `json:"type"`
		Title    string    `json:"title"`
		Body     string    `json:"body"`
		DeepLink *string   `json:"deep_link"`
		ReadAt   *string   `json:"read_at"`
	} `json:"data"`
	UnreadCount int `json:"unread_count"`
}

func (e *env) inboxOf(t *testing.T, token string) inbox {
	t.Helper()
	var ib inbox
	st, body := e.do(token, http.MethodGet, "/api/v1/notifications?limit=50", nil)
	e.mustJSON(st, body, 200, &ib)
	return ib
}

func hasType(ib inbox, typ string) bool {
	for _, n := range ib.Data {
		if n.Type == typ {
			return true
		}
	}
	return false
}

// AT-008 Service Request (WF-004) + PRD §17 notification events + search.
func TestServiceRequestAndNotifications(t *testing.T) {
	e := setup(t)
	ops := e.login("ops@demo.buildingvision.id")
	engSpv := e.login("eng.spv@demo.buildingvision.id")
	tech := e.login("budi@demo.buildingvision.id")
	budiID := e.refs.Users["technician"]

	// tenant dari seed
	var tenants struct {
		Data []struct {
			ID   uuid.UUID `json:"id"`
			Name string    `json:"name"`
		} `json:"data"`
	}
	st, body := e.do(ops, http.MethodGet, "/api/v1/tenants?property_id="+e.refs.PropertyID.String(), nil)
	e.mustJSON(st, body, 200, &tenants)
	if len(tenants.Data) != 1 {
		t.Fatalf("tenant seed: %d", len(tenants.Data))
	}
	tenantID := tenants.Data[0].ID

	// staff membuat SR atas nama tenant (kategori maintenance → domain engineering)
	var sr struct {
		ID             uuid.UUID `json:"id"`
		RequestNumber  string    `json:"request_number"`
		Status         string    `json:"status"`
		Priority       string    `json:"priority"`
		AllowedActions []string  `json:"allowed_actions"`
		Location       struct {
			PathText *string `json:"path_text"`
		} `json:"location"`
		Links []struct {
			ObjectType string `json:"object_type"`
			Label      string `json:"label"`
			Direction  string `json:"direction"`
		} `json:"links"`
		SLA *struct {
			ResolutionDueAt *string `json:"resolution_due_at"`
		} `json:"sla"`
	}
	st, body = e.do(ops, http.MethodPost, "/api/v1/service-requests", map[string]any{"category_code": "maintenance", "title": "AC unit 1201 tidak dingin", "tenant_id": tenantID, "channel": "phone"})
	e.mustJSON(st, body, 201, &sr)
	if !strings.HasPrefix(sr.RequestNumber, "SR-") || sr.Status != "new" || sr.SLA == nil {
		t.Fatalf("AT-008 create: %+v", sr)
	}
	if sr.Location.PathText == nil || !strings.Contains(*sr.Location.PathText, "1201") {
		t.Fatalf("SR lokasi default dari unit tenant: %v", sr.Location.PathText)
	}
	e.dispatch(t)
	// PRD §17.1: Service Request created → Operations/Domain Supervisor (engineering lead = eng.spv)
	if ib := e.inboxOf(t, engSpv); !hasType(ib, "service_request_received") {
		t.Fatalf("notifikasi SR received ke supervisor engineering tidak ada: %+v", ib.Data)
	}
	// triage: acknowledge → assign team engineering
	st, body = e.do(ops, http.MethodPost, "/api/v1/service-requests/"+sr.ID.String()+"/acknowledge", nil)
	e.mustJSON(st, body, 200, &sr)
	st, body = e.do(ops, http.MethodPost, "/api/v1/service-requests/"+sr.ID.String()+"/assign", map[string]any{"assignee_team_id": e.refs.Teams["engineering"]})
	e.mustJSON(st, body, 200, &sr)
	if sr.Status != "assigned" {
		t.Fatalf("AT-008 assign: %s", sr.Status)
	}
	// SR → Work Order (link dua arah)
	var wo workItem
	st, body = e.do(engSpv, http.MethodPost, "/api/v1/service-requests/"+sr.ID.String()+"/work-orders", map[string]any{"work_order_type": "repair", "assignee_user_id": budiID, "requires_evidence": false})
	e.mustJSON(st, body, 201, &wo)
	st, body = e.do(ops, http.MethodGet, "/api/v1/service-requests/"+sr.ID.String(), nil)
	e.mustJSON(st, body, 200, &sr)
	if sr.Status != "in_progress" {
		t.Fatalf("AT-008: SR harus in_progress setelah WO dibuat, got %s", sr.Status)
	}
	if len(sr.Links) != 1 || sr.Links[0].ObjectType != "work_order" || sr.Links[0].Label != wo.Number {
		t.Fatalf("AT-008: link SR→WO: %+v", sr.Links)
	}
	var links struct {
		Data []struct {
			ObjectType string `json:"object_type"`
			Label      string `json:"label"`
		} `json:"data"`
	}
	st, body = e.do(engSpv, http.MethodGet, "/api/v1/work-orders/"+wo.ID.String()+"/links", nil)
	e.mustJSON(st, body, 200, &links)
	if len(links.Data) != 1 || links.Data[0].ObjectType != "service_request" || links.Data[0].Label != sr.RequestNumber {
		t.Fatalf("AT-008: link WO→SR (bidirectional): %+v", links.Data)
	}
	// resolve SR ditolak selama WO masih open (kecuali manage)
	st, _ = e.do(engSpv, http.MethodPost, "/api/v1/service-requests/"+sr.ID.String()+"/resolve", map[string]any{"resolution": "x"})
	if st != 403 && st != 409 {
		t.Fatalf("resolve SR saat WO open harus ditolak, got %d", st)
	}
	e.dispatch(t)
	// Work Order assigned → Assignee (budi)
	if ib := e.inboxOf(t, tech); !hasType(ib, "work_order_assigned") {
		t.Fatalf("notifikasi WO assigned ke technician tidak ada: %+v", ib.Data)
	}
	// execution: start → complete → close (requires_evidence=false) ⇒ SR resolved otomatis
	st, body = e.do(tech, http.MethodPost, "/api/v1/work-orders/"+wo.ID.String()+"/start", nil)
	e.mustJSON(st, body, 200, &wo)
	st, body = e.do(tech, http.MethodPost, "/api/v1/work-orders/"+wo.ID.String()+"/complete", map[string]any{"resolution": "Freon diisi ulang"})
	e.mustJSON(st, body, 200, &wo)
	e.dispatch(t)
	// Work Order completed → Supervisor / Requester (ops = requester)
	if ib := e.inboxOf(t, engSpv); !hasType(ib, "work_order_completed") {
		t.Fatalf("notifikasi WO completed ke supervisor tidak ada")
	}
	if ib := e.inboxOf(t, ops); !hasType(ib, "work_order_completed") {
		t.Fatalf("notifikasi WO completed ke requester tidak ada: %+v", ib.Data)
	}
	st, body = e.do(engSpv, http.MethodPost, "/api/v1/work-orders/"+wo.ID.String()+"/close", nil)
	e.mustJSON(st, body, 200, &wo)
	st, body = e.do(ops, http.MethodGet, "/api/v1/service-requests/"+sr.ID.String(), nil)
	e.mustJSON(st, body, 200, &sr)
	if sr.Status != "resolved" {
		t.Fatalf("WF-004: SR harus resolved setelah WO closed, got %s", sr.Status)
	}
	st, body = e.do(ops, http.MethodPost, "/api/v1/service-requests/"+sr.ID.String()+"/close", nil)
	e.mustJSON(st, body, 200, &sr)
	if sr.Status != "closed" {
		t.Fatalf("SR close: %s", sr.Status)
	}
	// inbox: mark read, unread count
	ib := e.inboxOf(t, tech)
	if ib.UnreadCount == 0 {
		t.Fatal("unread_count harus > 0")
	}
	st, _ = e.do(tech, http.MethodPost, "/api/v1/notifications/"+ib.Data[0].ID.String()+"/read", nil)
	if st != 204 {
		t.Fatalf("mark read: %d", st)
	}
	st, _ = e.do(tech, http.MethodPost, "/api/v1/notifications/read-all", nil)
	if st != 204 {
		t.Fatalf("read-all: %d", st)
	}
	if ib := e.inboxOf(t, tech); ib.UnreadCount != 0 {
		t.Fatalf("unread setelah read-all: %d", ib.UnreadCount)
	}
	if ib.Data[0].DeepLink == nil || !strings.HasPrefix(*ib.Data[0].DeepLink, "/operations/") {
		t.Fatalf("deep link: %v", ib.Data[0].DeepLink)
	}
	// notifikasi org lain tidak bocor
	adminB := e.login(e.orgBAdmin)
	if ib := e.inboxOf(t, adminB); len(ib.Data) != 0 {
		t.Fatal("AT-010: inbox org B harus kosong")
	}

	// ---------- Search (PRD §23): Object · Type · Location · Status ----------
	e.dispatch(t)
	var res struct {
		Data []struct {
			ObjectType   string  `json:"object_type"`
			BusinessID   *string `json:"business_id"`
			Title        string  `json:"title"`
			LocationPath *string `json:"location_path"`
			Status       *string `json:"status"`
			DeepLink     string  `json:"deep_link"`
		} `json:"data"`
	}
	st, body = e.do(ops, http.MethodGet, "/api/v1/search?q="+wo.Number[:11], nil) // "WO-2026-000" parsial
	e.mustJSON(st, body, 200, &res)
	found := false
	for _, r := range res.Data {
		if r.ObjectType == "work_order" && r.BusinessID != nil && *r.BusinessID == wo.Number && r.Status != nil && r.LocationPath != nil {
			found = true
		}
	}
	if !found {
		t.Fatalf("search parsial business id: %+v", res.Data)
	}
	st, body = e.do(ops, http.MethodGet, "/api/v1/search?q=AC+unit", nil)
	e.mustJSON(st, body, 200, &res)
	if len(res.Data) == 0 || res.Data[0].ObjectType != "service_request" {
		t.Fatalf("search judul SR: %+v", res.Data)
	}
	// technician tidak boleh melihat SR di search (tanpa permission tenant.service_requests.view)
	st, body = e.do(tech, http.MethodGet, "/api/v1/search?q=AC+unit", nil)
	e.mustJSON(st, body, 200, &res)
	for _, r := range res.Data {
		if r.ObjectType == "service_request" {
			t.Fatal("search harus memfilter permission per object type")
		}
	}
	// org B tidak melihat apa pun
	st, body = e.do(adminB, http.MethodGet, "/api/v1/search?q="+wo.Number, nil)
	e.mustJSON(st, body, 200, &res)
	if len(res.Data) != 0 {
		t.Fatal("AT-010: search org B harus kosong")
	}

	// ---------- Export (Should) ----------
	var ex struct {
		ID     uuid.UUID `json:"id"`
		Status string    `json:"status"`
	}
	st, body = e.do(ops, http.MethodPost, "/api/v1/exports", map[string]any{"resource": "work_orders", "format": "csv", "filters": map[string]string{"property_id": e.refs.PropertyID.String()}})
	e.mustJSON(st, body, 202, &ex)
	if err := e.app.Exports.Generate(context.Background(), e.refs.OrgID, ex.ID); err != nil {
		t.Fatalf("export generate: %v", err)
	}
	var ex2 struct {
		Status      string  `json:"status"`
		RowCount    *int    `json:"row_count"`
		DownloadURL *string `json:"download_url"`
	}
	st, body = e.do(ops, http.MethodGet, "/api/v1/exports/"+ex.ID.String(), nil)
	e.mustJSON(st, body, 200, &ex2)
	if ex2.Status != "ready" || ex2.RowCount == nil || *ex2.RowCount != 1 || ex2.DownloadURL == nil {
		t.Fatalf("export: %+v", ex2)
	}
}
