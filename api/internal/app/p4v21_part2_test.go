package app_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func p4Part2(t *testing.T, e *env, ctx context.Context, c p4ctx) {
	pm, fin, tenA, tenB, pid := c.pm, c.fin, c.tenA, c.tenB, c.pid
	orgID := e.refs.OrgID
	today := time.Now().In(c.loc)
	day := func(d int) string { return today.AddDate(0, 0, d).Format("2006-01-02") }

	// ---- bukti transfer tenant + verifikasi dengan jumlah diterima (P4-VRF-02, B-09) + filter server invoice_id (B-14) ----
	st, body := e.do(tenA, http.MethodPost, "/api/v1/tenant/invoices/"+c.inv1201+"/payments", map[string]any{"provider_code": "manual", "method": "transfer", "amount": 1000000})
	pay1 := e.jm(st, body, 201)
	pay1ID := uuid.MustParse(str(pay1["id"]))
	// respons inisiasi memuat aksi tenant (unggah bukti untuk pemilik); kwitansi belum tersedia sebelum verifikasi
	hasProof := false
	for _, a := range pay1["allowed_actions"].([]any) {
		hasProof = hasProof || a == "upload_proof"
	}
	if !hasProof {
		t.Fatalf("TenantPay harus mengembalikan upload_proof: %v", pay1["allowed_actions"])
	}
	if st, body := e.do(tenA, http.MethodPost, "/api/v1/tenant/payments/"+pay1ID.String()+"/document-link", nil); st != 409 || !strings.Contains(string(body), "RECEIPT_NOT_AVAILABLE") {
		t.Fatalf("tautan kwitansi tenant sebelum lunas harus 409: %d %s", st, body)
	}
	e.uploadTenantAttachment(tenA, "payment", pay1ID, "photo", "image/png")
	st, body = e.do(fin, http.MethodGet, "/api/v1/payments/"+pay1ID.String()+"/proofs", nil)
	if pr := e.jm(st, body, 200); len(list(pr["data"])) != 1 {
		t.Fatalf("bukti transfer: %v", pr)
	}
	st, body = e.do(fin, http.MethodPost, "/api/v1/payments/"+pay1ID.String()+"/verify", map[string]any{"amount": 900000, "reference": "BCA 998877"})
	if p := e.jm(st, body, 200); p["status"] != "paid" || num(p["amount"]) != 900000 {
		t.Fatalf("verifikasi jumlah diterima: %v", p)
	}
	st, body = e.do(tenA, http.MethodGet, "/api/v1/tenant/payments?invoice_id="+c.inv1201, nil)
	if l := list(e.jm(st, body, 200)["data"]); len(l) != 1 {
		t.Fatalf("B-14 filter invoice_id: %d", len(l))
	}
	st, body = e.do(tenA, http.MethodGet, "/api/v1/tenant/payments?invoice_id="+c.elInv, nil)
	if l := list(e.jm(st, body, 200)["data"]); len(l) != 0 {
		t.Fatalf("B-14 invoice lain harus kosong: %d", len(l))
	}

	// ---- satu penerimaan → banyak invoice (FIFO) + sisa → saldo kredit (P4-PAY-04..05) ----
	outA := c.total1201 - 900000
	st, body = e.do(fin, http.MethodGet, "/api/v1/invoices/"+c.elInv, nil)
	elTotal := int64(num(e.jm(st, body, 200)["total_amount"]))
	st, body = e.do(fin, http.MethodPost, "/api/v1/payments/receive", map[string]any{"property_id": pid, "tenant_id": c.tenantA, "amount": outA + elTotal + 105750, "method": "transfer", "reference": "Transfer gabungan"})
	rcv := e.jm(st, body, 201)
	if !strings.HasPrefix(str(rcv["receipt_group"]), "RCV-") || int64(num(rcv["allocated_amount"])) != outA+elTotal || num(rcv["credit_amount"]) != 105750 || len(list(rcv["payments"])) != 2 {
		t.Fatalf("receive: %v", rcv)
	}
	var elPayID string
	for _, p := range list(rcv["payments"]) {
		if str(p["invoice_id"]) == c.elInv {
			elPayID = str(p["id"])
		}
	}
	balance := func(tok string) map[string]any {
		st, body := e.do(tok, http.MethodGet, "/api/v1/tenant/balances", nil)
		return e.jm(st, body, 200)
	}
	if b := balance(tenA); num(b["credit_balance"]) != 105750 || num(b["outstanding_amount"]) != 0 {
		t.Fatalf("saldo tenant A: %v", b)
	}
	// ---- refund ke saldo kredit (P4-PAY-06, B-12) → pakai saldo kredit untuk invoice ----
	if st, _ := e.do(fin, http.MethodPost, "/api/v1/payments/"+elPayID+"/refund", map[string]any{"reason": "salah alokasi", "to_credit": true}); st != 403 {
		t.Fatalf("finance_staff refund harus 403: %d", st)
	}
	st, body = e.do(pm, http.MethodPost, "/api/v1/payments/"+elPayID+"/refund", map[string]any{"reason": "salah alokasi", "to_credit": true})
	if p := e.jm(st, body, 200); p["status"] != "refunded" {
		t.Fatalf("refund: %v", p)
	}
	if b := balance(tenA); int64(num(b["credit_balance"])) != 105750+elTotal || int64(num(b["outstanding_amount"])) != elTotal {
		t.Fatalf("saldo setelah refund ke kredit: %v", b)
	}
	st, body = e.do(fin, http.MethodPost, "/api/v1/invoices/"+c.elInv+"/apply-credit", map[string]any{})
	if p := e.jm(st, body, 201); p["method"] != "credit" || int64(num(p["amount"])) != elTotal {
		t.Fatalf("pakai saldo kredit: %v", p)
	}
	if b := balance(tenA); num(b["credit_balance"]) != 105750 || num(b["outstanding_amount"]) != 0 {
		t.Fatalf("saldo setelah pakai kredit: %v", b)
	}

	// ---- credit note (P4-INV-07; approve = manager) → kelebihan menjadi kredit ----
	st, body = e.do(fin, http.MethodPost, "/api/v1/invoices/"+c.inv1201+"/credit-notes", map[string]any{"amount": 100000, "reason": "Koreksi luas unit"})
	cn := e.jm(st, body, 201)
	if st, _ := e.do(fin, http.MethodPost, "/api/v1/billing/credit-notes/"+str(cn["id"])+"/approve", nil); st != 403 {
		t.Fatalf("finance_staff approve credit note harus 403: %d", st)
	}
	st, body = e.do(pm, http.MethodPost, "/api/v1/billing/credit-notes/"+str(cn["id"])+"/approve", map[string]any{"note": "ok"})
	if x := e.jm(st, body, 200); x["status"] != "approved" || !strings.HasPrefix(str(x["credit_note_number"]), "CN-") {
		t.Fatalf("approve credit note: %v", x)
	}
	if b := balance(tenA); num(b["credit_balance"]) != 205750 {
		t.Fatalf("kredit dari credit note: %v", b)
	}

	// ---- void invoice terbit (SoD: finance_staff 403, manager OK) + notifikasi pembatalan (B-18) ----
	st, body = e.do(fin, http.MethodPost, "/api/v1/invoices", map[string]any{"property_id": pid, "tenant_id": c.tenantA, "unit_location_id": e.refs.UnitA1201, "invoice_type": "other", "due_date": day(10), "issue_now": true,
		"items": []map[string]any{{"description": "Biaya kartu akses", "unit_price": 300000}}})
	voidInv := e.jm(st, body, 201)
	if st, _ := e.do(fin, http.MethodPost, "/api/v1/invoices/"+str(voidInv["id"])+"/void", map[string]any{"reason": "salah"}); st != 403 {
		t.Fatalf("finance_staff void harus 403: %d", st)
	}
	st, body = e.do(pm, http.MethodPost, "/api/v1/invoices/"+str(voidInv["id"])+"/void", map[string]any{"reason": "Salah tagih"})
	if x := e.jm(st, body, 200); x["status"] != "cancelled" || x["invoice_number"] == nil {
		t.Fatalf("void: %v", x)
	}
	e.dispatch(t)
	if ib := e.inboxOf(t, tenA); !hasType(ib, "invoice_cancelled") {
		t.Fatalf("B-18 notifikasi pembatalan: %+v", ib.Data)
	}

	// ---- denda: akrual pada invoice overdue, tagih selisih, waive (P4-PND-01..02) + pengingat bertahap (P4-COL-02) ----
	st, body = e.do(pm, http.MethodPost, "/api/v1/billing/penalty-rules", map[string]any{"property_id": pid, "name": "Denda 2%/bulan", "method": "percent", "rate": 2, "period": "per_month"})
	prule := e.jm(st, body, 201)
	if !strings.Contains(str(prule["summary"]), "2% per bulan") {
		t.Fatalf("ringkasan aturan denda: %v", prule)
	}
	st, body = e.do(fin, http.MethodPost, "/api/v1/invoices", map[string]any{"property_id": pid, "tenant_id": c.tenantB, "unit_location_id": e.refs.UnitA1202, "invoice_type": "service_charge", "due_date": day(-10), "issue_now": true, "apply_tax": false,
		"items": []map[string]any{{"description": "Service charge lama", "unit_price": 1000000}}})
	invB := e.jm(st, body, 201)
	st, body = e.do(fin, http.MethodPost, "/api/v1/invoices", map[string]any{"property_id": pid, "tenant_id": c.tenantA, "unit_location_id": e.refs.UnitA1201, "invoice_type": "service_charge", "due_date": day(-40), "issue_now": true, "apply_tax": false,
		"items": []map[string]any{{"description": "Service charge tertunggak", "unit_price": 500000}}})
	invAOld := e.jm(st, body, 201)
	st, body = e.do(fin, http.MethodPost, "/api/v1/invoices", map[string]any{"property_id": pid, "tenant_id": c.tenantA, "unit_location_id": e.refs.UnitA1201, "invoice_type": "parking", "due_date": day(3), "issue_now": true, "apply_tax": false,
		"items": []map[string]any{{"description": "Parkir bulanan", "unit_price": 250000}}})
	invSoon := e.jm(st, body, 201)
	evBefore := len(e.jobs.Events)
	if err := e.app.Billing.Sweep(ctx, orgID); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	dueSoon, overdueEv := 0, 0
	for _, ev := range e.jobs.Events[evBefore:] {
		if ev.Type == "invoice.due_soon" && ev.ObjectID.String() == str(invSoon["id"]) && ev.Payload["stage"] == "H-3" {
			dueSoon++
		}
		if ev.Type == "invoice.overdue" && ev.ObjectID.String() == str(invB["id"]) {
			overdueEv++
		}
	}
	if dueSoon != 1 || overdueEv != 1 {
		t.Fatalf("pengingat H-3 (%d) / overdue (%d) harus sekali", dueSoon, overdueEv)
	}
	evBefore = len(e.jobs.Events)
	if err := e.app.Billing.Sweep(ctx, orgID); err != nil {
		t.Fatalf("sweep 2: %v", err)
	}
	for _, ev := range e.jobs.Events[evBefore:] {
		if ev.Type == "invoice.due_soon" || ev.Type == "invoice.overdue" || ev.Type == "invoice.reminder" {
			t.Fatalf("sweep ulang tidak boleh mengirim pengingat ganda: %s %s", ev.Type, ev.ObjectLabel)
		}
	}
	var pens struct {
		Data []struct {
			ID             string   `json:"id"`
			InvoiceID      string   `json:"invoice_id"`
			AccruedAmount  int64    `json:"accrued_amount"`
			UnbilledAmount int64    `json:"unbilled_amount"`
			Status         string   `json:"status"`
			AllowedActions []string `json:"allowed_actions"`
		} `json:"data"`
	}
	st, body = e.do(fin, http.MethodGet, "/api/v1/billing/penalties?property_id="+pid, nil)
	e.mustJSON(st, body, 200, &pens)
	var penB, penA string
	for _, p := range pens.Data {
		switch p.InvoiceID {
		case str(invB["id"]):
			if p.AccruedAmount != 20000 || p.Status != "accruing" {
				t.Fatalf("denda 2%% × 1.000.000 × 1 bulan: %+v", p)
			}
			penB = p.ID
		case str(invAOld["id"]):
			if p.AccruedAmount != 20000 { // 2% × 500.000 × 2 bulan (40 hari)
				t.Fatalf("denda 40 hari: %+v", p)
			}
			penA = p.ID
		}
	}
	if penB == "" || penA == "" {
		t.Fatalf("denda tidak terakrual: %s", body)
	}
	st, body = e.do(fin, http.MethodPost, "/api/v1/billing/penalties/bill", map[string]any{"property_id": pid, "penalty_ids": []string{penB}})
	if b := e.jm(st, body, 201); num(b["total_amount"]) != 20000 || len(strs(b["invoice_ids"])) != 1 {
		t.Fatalf("tagih denda: %v", b)
	}
	if st, _ := e.do(fin, http.MethodPost, "/api/v1/billing/penalties/bill", map[string]any{"property_id": pid, "penalty_ids": []string{penB}}); st != 409 {
		t.Fatalf("tagih ulang denda tanpa selisih harus 409: %d", st)
	}
	if st, _ := e.do(fin, http.MethodPost, "/api/v1/billing/penalties/"+penA+"/waive", map[string]any{"reason": "kebijakan"}); st != 403 {
		t.Fatalf("finance_staff waive harus 403: %d", st)
	}
	st, body = e.do(pm, http.MethodPost, "/api/v1/billing/penalties/"+penA+"/waive", map[string]any{"reason": "Keringanan pertama"})
	if x := e.jm(st, body, 200); x["status"] != "waived" || num(x["unbilled_amount"]) != 0 {
		t.Fatalf("waive denda: %v", x)
	}

	// ---- aging, statement of account (+PDF), laporan (B-17: finance_staff punya reports.*) ----
	st, body = e.do(fin, http.MethodGet, "/api/v1/billing/aging?property_id="+pid+"&group_by=tenant", nil)
	ag := e.jm(st, body, 200)
	tot, _ := ag["totals"].(map[string]any)
	if num(tot["d1_30"]) < 1000000 || num(tot["d31_60"]) < 500000 {
		t.Fatalf("aging bucket: %v", tot)
	}
	st, body = e.do(fin, http.MethodGet, "/api/v1/reports/aging?property_id="+pid, nil)
	e.jm(st, body, 200)
	st, body = e.do(fin, http.MethodGet, "/api/v1/billing/statement?property_id="+pid+"&tenant_id="+c.tenantA.String()+"&from="+day(-90)+"&to="+day(1), nil)
	stmt := e.jm(st, body, 200)
	if len(list(stmt["entries"])) < 5 || int64(num(stmt["closing_balance"])) != int64(num(stmt["outstanding_amount"]))-int64(num(stmt["credit_balance"])) {
		t.Fatalf("statement tidak konsisten (saldo akhir = sisa tagihan − saldo kredit): closing=%v outstanding=%v credit=%v entries=%v", stmt["closing_balance"], stmt["outstanding_amount"], stmt["credit_balance"], stmt["entries"])
	}
	if st, h, b := e.rawDo(fin, http.MethodGet, "/api/v1/billing/statement?property_id="+pid+"&tenant_id="+c.tenantA.String()+"&format=pdf"); st != 200 || h.Get("Content-Type") != "application/pdf" || !strings.HasPrefix(string(b), "%PDF") {
		t.Fatalf("statement PDF: %d %s", st, h.Get("Content-Type"))
	}
	st, body = e.do(tenA, http.MethodGet, "/api/v1/tenant/statement", nil)
	if ts := e.jm(st, body, 200); len(list(ts["entries"])) == 0 {
		t.Fatalf("statement tenant: %v", ts)
	}

	// ---- PDF invoice & kwitansi + tautan bertanda tangan (P4-INV-10, P4-RCP-02, P4-TNT-03) ----
	if st, h, b := e.rawDo(fin, http.MethodGet, "/api/v1/invoices/"+c.inv1201+"/pdf"); st != 200 || h.Get("Content-Type") != "application/pdf" || !strings.HasPrefix(string(b), "%PDF") {
		t.Fatalf("invoice PDF: %d", st)
	}
	if st, _, b := e.rawDo(fin, http.MethodGet, "/api/v1/payments/"+pay1ID.String()+"/receipt"); st != 200 || !strings.HasPrefix(string(b), "%PDF") {
		t.Fatalf("kwitansi PDF: %d", st)
	}
	if st, _, b := e.rawDo(tenA, http.MethodGet, "/api/v1/tenant/invoices/"+c.inv1201+"/pdf"); st != 200 || !strings.HasPrefix(string(b), "%PDF") {
		t.Fatalf("invoice PDF tenant: %d", st)
	}
	if st, _, _ := e.rawDo(tenB, http.MethodGet, "/api/v1/tenant/invoices/"+c.inv1201+"/pdf"); st != 404 {
		t.Fatalf("PDF invoice tenant lain harus 404: %d", st)
	}
	if st, _, b := e.rawDo(tenA, http.MethodGet, "/api/v1/tenant/payments/"+pay1ID.String()+"/receipt"); st != 200 || !strings.HasPrefix(string(b), "%PDF") {
		t.Fatalf("kwitansi PDF tenant: %d", st)
	}
	st, body = e.do(tenA, http.MethodPost, "/api/v1/tenant/invoices/"+c.inv1201+"/document-link", nil)
	link := e.jm(st, body, 200)
	u := str(link["url"])
	path := u[strings.Index(u, "/api/v1/public/documents/"):]
	if st, _, b := e.rawDo("", http.MethodGet, path); st != 200 || !strings.HasPrefix(string(b), "%PDF") {
		t.Fatalf("tautan dokumen publik: %d", st)
	}
	if st, _, _ := e.rawDo("", http.MethodGet, path[:len(path)-2]+"xx"); st != 404 {
		t.Fatalf("tautan dimanipulasi harus 404: %d", st)
	}

	// ---- rekonsiliasi: impor mutasi CSV → saran (nomor pembayaran di berita) → konfirmasi massal; abaikan bunga (P4-REC-*) ----
	st, body = e.do(tenB, http.MethodPost, "/api/v1/tenant/invoices/"+str(invB["id"])+"/payments", map[string]any{"provider_code": "manual", "method": "transfer"})
	payB := e.jm(st, body, 201)
	d := today.Format("02/01/2006")
	csvText := "Mutasi Rekening BCA 1234567890\n\nTanggal,Keterangan,Debet,Kredit,Saldo\n" +
		d + ",TRSF E-BANKING CR " + str(payB["payment_number"]) + " RUDI HARTONO,,\"1.000.000,00\",\"5.000.000,00\"\n" +
		d + ",BUNGA REKENING,,\"12.345,00\",\"5.012.345,00\"\n" +
		d + ",BIAYA ADM,\"15.000,00\",,\"4.997.345,00\"\n"
	st, body = e.do(fin, http.MethodPost, "/api/v1/billing/bank-statements", map[string]any{"property_id": pid, "file_name": "mutasi.csv", "content": csvText})
	imp := e.jm(st, body, 201)
	if num(imp["line_count"]) != 3 || num(imp["credit_count"]) != 2 || num(imp["suggested_count"]) != 1 || num(imp["unmatched_count"]) != 1 || num(imp["ignored_count"]) != 1 {
		t.Fatalf("impor mutasi: %v", imp)
	}
	st, body = e.do(fin, http.MethodPost, "/api/v1/billing/bank-statements/"+str(imp["id"])+"/confirm-suggestions", map[string]any{})
	conf := e.jm(st, body, 200)
	if num(conf["confirmed"]) != 1 {
		t.Fatalf("konfirmasi saran: %v", conf)
	}
	st, body = e.do(fin, http.MethodGet, "/api/v1/payments/"+str(payB["id"]), nil)
	if p := e.jm(st, body, 200); p["status"] != "paid" {
		t.Fatalf("pembayaran tenant B setelah rekonsiliasi: %v", p)
	}
	impDetail, _ := conf["import"].(map[string]any)
	for _, l := range list(impDetail["lines"]) {
		if l["status"] == "unmatched" {
			if st, _ := e.do(fin, http.MethodPost, "/api/v1/billing/bank-statement-lines/"+str(l["id"])+"/ignore", map[string]any{}); st != 400 {
				t.Fatalf("abaikan tanpa alasan harus 400: %d", st)
			}
			st, body = e.do(fin, http.MethodPost, "/api/v1/billing/bank-statement-lines/"+str(l["id"])+"/ignore", map[string]any{"note": "Bunga bank"})
			if x := e.jm(st, body, 200); x["status"] != "completed" {
				t.Fatalf("impor harus selesai setelah semua baris ditangani: %v", x["status"])
			}
		}
	}
	st, body = e.do(fin, http.MethodPost, "/api/v1/billing/bank-statements", map[string]any{"property_id": pid, "file_name": "mutasi.csv", "content": csvText})
	if x := e.jm(st, body, 201); num(x["duplicates_skipped"]) != 3 {
		t.Fatalf("impor ulang harus melewati duplikat: %v", x["duplicates_skipped"])
	}

	p4Part3(t, e, ctx, c, day, str(invAOld["id"]))
}
