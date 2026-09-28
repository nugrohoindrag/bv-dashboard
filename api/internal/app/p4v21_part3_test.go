package app_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/platform/jobs"
)

func p4Part3(t *testing.T, e *env, ctx context.Context, c p4ctx, day func(int) string, invAOld string) {
	pm, fin, admin, pid := c.pm, c.fin, c.admin, c.pid
	orgID := e.refs.OrgID
	year := time.Now().In(c.loc).Year()

	// ---- additional charge dari Work Order (P4-INV-09, exit gate §29) ----
	st, body := e.do(pm, http.MethodPost, "/api/v1/work-orders", map[string]any{"work_order_type": "corrective", "title": "Perbaikan AC unit 1201", "priority": "medium", "location_id": e.refs.UnitA1201})
	wo := e.jm(st, body, 201)
	woID := uuid.MustParse(str(wo["id"]))
	if err := e.app.DB.WithOrgTx(ctx, orgID, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE work_orders SET service_cost_amount = 350000, actual_cost_amount = 350000 WHERE id = $1`, woID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	st, body = e.do(fin, http.MethodGet, "/api/v1/billing/charges/draft?source_type=work_order&source_id="+woID.String(), nil)
	draft := e.jm(st, body, 200)
	items := list(draft["items"])
	if len(items) != 1 || num(items[0]["amount"]) != 350000 || str(draft["tenant_id"]) != c.tenantA.String() {
		t.Fatalf("draft biaya WO: %v", draft)
	}
	st, body = e.do(fin, http.MethodPost, "/api/v1/billing/charges", map[string]any{"source_type": "work_order", "source_id": woID, "issue_now": true})
	charge := e.jm(st, body, 201)
	if charge["invoice_type"] != "additional_charge" || charge["source"] != "work_order" || charge["status"] != "issued" {
		t.Fatalf("invoice additional charge: %v", charge)
	}
	if st, body := e.do(fin, http.MethodPost, "/api/v1/billing/charges", map[string]any{"source_type": "work_order", "source_id": woID}); st != 409 || !strings.Contains(string(body), "ALREADY_CHARGED") {
		t.Fatalf("tagih ulang WO harus 409: %d %s", st, body)
	}

	// ---- impor invoice CSV (P4-BRL-05): dry run dengan error per baris, lalu impor ----
	due := day(20)
	st, body = e.do(fin, http.MethodPost, "/api/v1/invoices/import", map[string]any{"property_id": pid, "file_name": "tagihan.csv", "dry_run": true,
		"content": "tenant_code,unit,invoice_type,description,amount,due_date\nNOPE-1,,other,Tes,100000," + due + "\n"})
	if r := e.jm(st, body, 200); len(list(r["errors"])) != 1 {
		t.Fatalf("impor dry run harus 1 error: %v", r)
	}
	st, body = e.do(fin, http.MethodPost, "/api/v1/invoices/import", map[string]any{"property_id": pid, "file_name": "tagihan.csv",
		"content": "unit,invoice_type,description,amount,due_date,group\n1202,other,Biaya kartu akses,150000," + due + ",G1\n1202,other,Biaya parkir tamu,\"50.000\"," + due + ",G1\n"})
	if r := e.jm(st, body, 201); num(r["invoices"]) != 1 || num(r["total_amount"]) != 200000 || len(list(r["errors"])) != 0 {
		t.Fatalf("impor invoice: %v", r)
	}

	// ---- sinking fund: penerimaan dari pembayaran + penggunaan tertaut WO (P4-SCF-02..04) ----
	st, body = e.do(fin, http.MethodGet, "/api/v1/billing/sinking-fund?property_id="+pid, nil)
	sf := e.jm(st, body, 200)
	bal0 := num(sf["balance"])
	if bal0 <= 0 {
		t.Fatalf("sinking fund dari pembayaran: %v", sf)
	}
	if st, _ := e.do(fin, http.MethodPost, "/api/v1/billing/sinking-fund/entries", map[string]any{"property_id": pid, "entry_type": "usage", "amount": 100000, "description": "x"}); st != 403 {
		t.Fatalf("finance_staff pakai sinking fund harus 403: %d", st)
	}
	st, body = e.do(pm, http.MethodPost, "/api/v1/billing/sinking-fund/entries", map[string]any{"property_id": pid, "entry_type": "usage", "amount": 100000, "description": "Penggantian pompa", "work_order_id": woID})
	e.jm(st, body, 201)
	st, body = e.do(fin, http.MethodGet, "/api/v1/billing/sinking-fund?property_id="+pid, nil)
	if sf = e.jm(st, body, 200); num(sf["balance"]) != bal0-100000 {
		t.Fatalf("saldo sinking fund setelah penggunaan: %v", sf)
	}

	// ---- deposit ledger + potong deposit untuk tagihan (P4-PND-03..04) ----
	st, body = e.do(fin, http.MethodPost, "/api/v1/billing/deposits/entries", map[string]any{"property_id": pid, "tenant_id": c.tenantA, "entry_type": "received", "amount": 2000000, "reason": "Deposit fit-out"})
	e.jm(st, body, 201)
	st, body = e.do(fin, http.MethodPost, "/api/v1/invoices/"+str(charge["id"])+"/apply-deposit", map[string]any{})
	dp := e.jm(st, body, 201)
	st, body = e.do(fin, http.MethodGet, "/api/v1/billing/balances?kind=deposit&property_id="+pid+"&tenant_id="+c.tenantA.String(), nil)
	bals := list(e.jm(st, body, 200)["data"])
	if len(bals) != 1 || num(bals[0]["balance"]) != 2000000-num(dp["amount"]) {
		t.Fatalf("saldo deposit: %v (dipotong %v)", bals, dp["amount"])
	}

	// ---- log penagihan + janji bayar → daftar kerja → ingkar (P4-COL-03..04) ----
	st, body = e.do(fin, http.MethodPost, "/api/v1/billing/collection-logs", map[string]any{"property_id": pid, "tenant_id": c.tenantA, "invoice_ids": []string{invAOld}, "channel": "whatsapp",
		"outcome": "promise_to_pay", "promise_date": day(3), "promise_amount": 500000, "notes": "Janji transfer"})
	cl := e.jm(st, body, 201)
	if cl["promise_status"] != "open" {
		t.Fatalf("janji bayar: %v", cl)
	}
	st, body = e.do(fin, http.MethodGet, "/api/v1/billing/collections/worklist?property_id="+pid, nil)
	wl := list(e.jm(st, body, 200)["data"])
	found := false
	for _, w := range wl {
		if str(w["tenant_id"]) == c.tenantA.String() && w["promise_date"] != nil {
			found = true
		}
	}
	if !found {
		t.Fatalf("daftar kerja penagihan: %v", wl)
	}
	if err := e.app.DB.WithOrgTx(ctx, orgID, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE collection_logs SET promise_date = current_date - 2 WHERE id = $1`, str(cl["id"]))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	evBefore := len(e.jobs.Events)
	if err := e.app.Billing.Sweep(ctx, orgID); err != nil {
		t.Fatal(err)
	}
	broken := false
	for _, ev := range e.jobs.Events[evBefore:] {
		if ev.Type == "collection_promise.broken" {
			broken = true
		}
	}
	st, body = e.do(fin, http.MethodGet, "/api/v1/billing/collection-logs?property_id="+pid+"&promise_status=broken", nil)
	if l := list(e.jm(st, body, 200)["data"]); !broken || len(l) != 1 {
		t.Fatalf("janji ingkar: event=%v list=%v", broken, l)
	}

	// ---- budget vs actual + biaya manual (P4-BGT-*, P4-CST-*) ----
	months := func(v int64) []int64 {
		out := make([]int64, 12)
		for i := range out {
			out[i] = v
		}
		return out
	}
	st, body = e.do(pm, http.MethodPost, "/api/v1/finance/budgets", map[string]any{"property_id": pid, "fiscal_year": year, "name": "Budget operasional",
		"lines": []map[string]any{{"kind": "revenue", "category": "service_charge", "months": months(2000000)}, {"kind": "cost", "category": "staff", "months": months(1000000)}}})
	bg := e.jm(st, body, 201)
	if num(bg["revenue_total"]) != 24000000 || num(bg["cost_total"]) != 12000000 {
		t.Fatalf("budget: %v", bg)
	}
	if st, _ := e.do(fin, http.MethodPost, "/api/v1/finance/budgets/"+str(bg["id"])+"/approve", nil); st != 403 {
		t.Fatalf("finance_staff approve budget harus 403: %d", st)
	}
	st, body = e.do(pm, http.MethodPost, "/api/v1/finance/budgets/"+str(bg["id"])+"/approve", nil)
	if x := e.jm(st, body, 200); x["status"] != "approved" {
		t.Fatalf("approve budget: %v", x)
	}
	st, body = e.do(fin, http.MethodPost, "/api/v1/finance/costs", map[string]any{"property_id": pid, "category": "staff", "entry_date": day(0), "amount": 750000, "description": "Gaji outsourcing security", "payee": "PT Aman"})
	cost := e.jm(st, body, 201)
	e.uploadStaffAttachment(fin, "cost_entry", uuid.MustParse(str(cost["id"])))
	st, body = e.do(fin, http.MethodGet, "/api/v1/finance/budget-actual?property_id="+pid+"&year="+itoa(year), nil)
	bva := e.jm(st, body, 200)
	tr, _ := bva["total_revenue"].(map[string]any)
	trTotal, _ := tr["total"].(map[string]any)
	var staffActual float64
	for _, r := range list(bva["cost"]) {
		if r["category"] == "staff" {
			tt, _ := r["total"].(map[string]any)
			staffActual = num(tt["actual"])
		}
	}
	if num(trTotal["actual"]) <= 0 || staffActual != 750000 || bva["budget_status"] != "approved" {
		t.Fatalf("budget vs actual: revenue=%v staff=%v status=%v", trTotal, staffActual, bva["budget_status"])
	}
	st, body = e.do(fin, http.MethodGet, "/api/v1/finance/budget-actual/transactions?property_id="+pid+"&kind=cost&category=staff&year="+itoa(year), nil)
	if l := list(e.jm(st, body, 200)["data"]); len(l) != 1 || num(l[0]["amount"]) != 750000 {
		t.Fatalf("drill-down transaksi biaya: %v", l)
	}
	st, body = e.do(fin, http.MethodGet, "/api/v1/finance/operating-costs?property_id="+pid+"&year="+itoa(year), nil)
	if oc := e.jm(st, body, 200); num(oc["total"]) < 750000 {
		t.Fatalf("operating cost: %v", oc)
	}

	// ---- jurnal siap-ekspor (P4-INT-02): seimbang; ekspor CSV ----
	st, body = e.do(fin, http.MethodGet, "/api/v1/finance/journal?property_id="+pid+"&from="+day(-90)+"&to="+day(1), nil)
	jr := e.jm(st, body, 200)
	if num(jr["total_debit"]) <= 0 || num(jr["total_debit"]) != num(jr["total_credit"]) {
		t.Fatalf("jurnal tidak seimbang: debit=%v kredit=%v", jr["total_debit"], jr["total_credit"])
	}
	if st, h, b := e.rawDo(fin, http.MethodGet, "/api/v1/finance/journal/export?property_id="+pid+"&from="+day(-90)+"&to="+day(1)+"&format=csv"); st != 200 || !strings.HasPrefix(h.Get("Content-Type"), "text/csv") || !strings.Contains(string(b), "Kode Akun") {
		t.Fatalf("ekspor jurnal: %d %s", st, h.Get("Content-Type"))
	}
	st, body = e.do(pm, http.MethodPut, "/api/v1/finance/account-mappings?property_id="+pid, map[string]any{"mappings": []map[string]any{{"key": "receivable", "account_code": "1-1310", "account_name": "Piutang IPL"}}})
	if l := list(e.jm(st, body, 200)["data"]); len(l) == 0 || l[0]["account_code"] != "1-1310" {
		t.Fatalf("pemetaan akun: %v", l)
	}

	// ---- webhook keluar bertanda tangan HMAC (P4-INT-04) ----
	var mu sync.Mutex
	var got []string
	var gotBody []byte
	var gotSig, gotTS string
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		got = append(got, r.Header.Get("X-BV-Event"))
		gotBody, gotSig, gotTS = b, r.Header.Get("X-BV-Signature"), r.Header.Get("X-BV-Timestamp")
		mu.Unlock()
		w.WriteHeader(204)
	}))
	defer hook.Close()
	if st, _ := e.do(pm, http.MethodPost, "/api/v1/finance/webhooks", map[string]any{"name": "ERP", "url": hook.URL, "event_types": []string{"invoice.issued"}}); st != 403 {
		t.Fatalf("webhook butuh grant organization: %d", st)
	}
	st, body = e.do(admin, http.MethodPost, "/api/v1/finance/webhooks", map[string]any{"name": "ERP", "url": hook.URL, "event_types": []string{"invoice.issued"}})
	ep := e.jm(st, body, 201)
	secret := str(ep["secret"])
	if !strings.HasPrefix(secret, "whsec_") {
		t.Fatalf("secret webhook: %v", ep)
	}
	evBefore = len(e.jobs.Events)
	st, body = e.do(fin, http.MethodPost, "/api/v1/invoices", map[string]any{"property_id": pid, "tenant_id": c.tenantB, "invoice_type": "other", "due_date": day(14), "issue_now": true, "items": []map[string]any{{"description": "Sewa gudang", "unit_price": 400000}}})
	e.jm(st, body, 201)
	for _, ev := range e.jobs.Events[evBefore:] {
		if err := e.app.Finance.Handle(ctx, ev); err != nil {
			t.Fatalf("finance subscriber: %v", err)
		}
	}
	n, err := e.app.Finance.DeliverSweep(ctx, orgID)
	if err != nil || n != 1 {
		t.Fatalf("pengiriman webhook: n=%d err=%v", n, err)
	}
	mu.Lock()
	// verifikasi penerima: v1=hex(hmac_sha256(secret, timestamp + "." + body))
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(gotTS + "."))
	mac.Write(gotBody)
	var payload struct {
		Type string `json:"type"`
		Data struct {
			Object map[string]any `json:"object"`
		} `json:"data"`
	}
	_ = json.Unmarshal(gotBody, &payload)
	if len(got) != 1 || got[0] != "invoice.issued" || gotSig != "v1="+hex.EncodeToString(mac.Sum(nil)) || !strings.HasPrefix(str(payload.Data.Object["invoice_number"]), "INV-") {
		t.Fatalf("webhook diterima: %v sig=%s body=%s", got, gotSig, gotBody)
	}
	mu.Unlock()
	st, body = e.do(admin, http.MethodGet, "/api/v1/finance/webhooks/"+str(ep["id"])+"/deliveries", nil)
	dl := list(e.jm(st, body, 200)["data"])
	if len(dl) != 1 || dl[0]["status"] != "delivered" {
		t.Fatalf("riwayat delivery: %v", dl)
	}

	// ---- dashboard Finance (P4-FIN-01) ----
	st, body = e.do(fin, http.MethodGet, "/api/v1/dashboards/finance?property_id="+pid, nil)
	dash := e.jm(st, body, 200)
	keys := map[string]bool{}
	for _, k := range list(dash["kpis"]) {
		keys[str(k["key"])] = k["drill_down"] != ""
	}
	for _, k := range []string{"collection_rate", "outstanding", "overdue", "aging_90", "revenue", "operating_cost", "budget_cost_variance", "sinking_fund", "pending_verification"} {
		if !keys[k] {
			t.Fatalf("KPI dashboard finance %s tidak ada: %v", k, dash["kpis"])
		}
	}

	// ---- ekspor dataset keuangan (P4-INT-01, B-17) ----
	for _, res := range []string{"invoices", "payments", "invoice_items", "credit_notes", "sinking_fund", "deposit_ledger", "credit_ledger"} {
		if st, body := e.do(fin, http.MethodPost, "/api/v1/exports", map[string]any{"resource": res, "format": "csv", "filters": map[string]string{"property_id": pid}}); st != 202 {
			t.Fatalf("ekspor %s: %d %s", res, st, body)
		}
	}
	for _, j := range e.jobs.Jobs {
		if x, ok := j.(jobs.ExportGenerateArgs); ok {
			if err := e.app.Exports.Generate(ctx, x.OrganizationID, x.ExportID); err != nil {
				t.Fatalf("generate export: %v", err)
			}
		}
	}
	var mine struct {
		Data []struct {
			Resource string  `json:"resource"`
			Status   string  `json:"status"`
			RowCount *int    `json:"row_count"`
			Error    *string `json:"error"`
		} `json:"data"`
	}
	st, body = e.do(fin, http.MethodGet, "/api/v1/exports", nil)
	e.mustJSON(st, body, 200, &mine)
	for _, x := range mine.Data {
		if x.Status != "ready" || x.RowCount == nil || *x.RowCount == 0 {
			t.Fatalf("ekspor %s: %+v (%v)", x.Resource, x, x.Error)
		}
	}
}

func itoa(v int) string {
	b := []byte{}
	if v == 0 {
		return "0"
	}
	for v > 0 {
		b = append([]byte{byte('0' + v%10)}, b...)
		v /= 10
	}
	return string(b)
}

// uploadStaffAttachment: presign → PUT → confirm lewat endpoint lampiran staf.
func (e *env) uploadStaffAttachment(token, objectType string, objectID uuid.UUID) uuid.UUID {
	e.t.Helper()
	var pre struct {
		AttachmentID uuid.UUID `json:"attachment_id"`
		StorageKey   string    `json:"storage_key"`
	}
	content := "%PDF-1.4\n0000"
	st, body := e.do(token, http.MethodPost, "/api/v1/attachments/presign", map[string]any{"object_type": objectType, "object_id": objectID, "attachment_type": "document", "content_type": "application/pdf", "size_bytes": len(content)})
	e.mustJSON(st, body, 201, &pre)
	if err := e.store.Put(context.Background(), pre.StorageKey, "application/pdf", strings.NewReader(content), int64(len(content))); err != nil {
		e.t.Fatal(err)
	}
	st, body = e.do(token, http.MethodPost, "/api/v1/attachments/"+pre.AttachmentID.String()+"/confirm", map[string]any{})
	e.mustJSON(st, body, 200, nil)
	return pre.AttachmentID
}
