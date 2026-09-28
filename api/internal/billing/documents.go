package billing

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/go-pdf/fpdf"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/platform/apperr"
	"github.com/buildingvision/api/internal/platform/authctx"
	"github.com/buildingvision/api/internal/property"
	"github.com/buildingvision/api/internal/tenantscope"
)

// ---------- Dokumen PDF: invoice, kwitansi, statement of account (PRD P4 v2.1 P4-INV-10, P4-RCP-02, P4-OUT-02, P4-TNT-03) ----------
// Dibuat on-the-fly (tidak disimpan) dari data terkini. Unduhan langsung (token user) atau tautan bertanda tangan HMAC berumur
// pendek `/public/documents/{token}` untuk dibagikan lewat WhatsApp manual / dibuka dari Tenant App (WebView tanpa header auth).

type Document struct {
	Bytes    []byte
	FileName string
}

type docParty struct {
	orgName, orgLegal, orgAddress, orgTaxID string
	propName, propAddress                   string
	st                                      Settings
	loc                                     *time.Location
}

func (s *Service) partyTx(ctx context.Context, tx pgx.Tx, propertyID uuid.UUID) docParty {
	var d docParty
	var legal, addr, city, tax, paddr, pcity *string
	_ = tx.QueryRow(ctx, `SELECT o.name, o.legal_name, o.address, o.city, o.tax_id, l.name, pr.address, pr.city FROM properties pr JOIN locations l ON l.id = pr.location_id
		JOIN organizations o ON o.id = pr.organization_id WHERE pr.location_id = $1`, propertyID).Scan(&d.orgName, &legal, &addr, &city, &tax, &d.propName, &paddr, &pcity)
	d.orgLegal = strPtrVal(legal)
	d.orgAddress = joinNonEmpty(", ", strPtrVal(addr), strPtrVal(city))
	d.orgTaxID = strPtrVal(tax)
	d.propAddress = joinNonEmpty(", ", strPtrVal(paddr), strPtrVal(pcity))
	d.st = s.settingsFor(ctx, tx, &propertyID)
	d.loc = property.PropertyTimezone(ctx, tx, propertyID)
	return d
}

func joinNonEmpty(sep string, xs ...string) string {
	var out []string
	for _, x := range xs {
		if strings.TrimSpace(x) != "" {
			out = append(out, strings.TrimSpace(x))
		}
	}
	return strings.Join(out, sep)
}

// ---------- terbilang (jumlah dalam kata, kwitansi) ----------

var satuan = []string{"", "satu", "dua", "tiga", "empat", "lima", "enam", "tujuh", "delapan", "sembilan", "sepuluh", "sebelas"}

func terbilang(n int64) string {
	switch {
	case n < 0:
		return "minus " + terbilang(-n)
	case n < 12:
		return satuan[n]
	case n < 20:
		return terbilang(n-10) + " belas"
	case n < 100:
		return strings.TrimSpace(terbilang(n/10) + " puluh " + terbilang(n%10))
	case n < 200:
		return strings.TrimSpace("seratus " + terbilang(n-100))
	case n < 1000:
		return strings.TrimSpace(terbilang(n/100) + " ratus " + terbilang(n%100))
	case n < 2000:
		return strings.TrimSpace("seribu " + terbilang(n-1000))
	case n < 1_000_000:
		return strings.TrimSpace(terbilang(n/1000) + " ribu " + terbilang(n%1000))
	case n < 1_000_000_000:
		return strings.TrimSpace(terbilang(n/1_000_000) + " juta " + terbilang(n%1_000_000))
	case n < 1_000_000_000_000:
		return strings.TrimSpace(terbilang(n/1_000_000_000) + " miliar " + terbilang(n%1_000_000_000))
	}
	return strings.TrimSpace(terbilang(n/1_000_000_000_000) + " triliun " + terbilang(n%1_000_000_000_000))
}

// Terbilang: "Satu juta lima ratus ribu rupiah".
func Terbilang(n int64) string {
	if n == 0 {
		return "Nol rupiah"
	}
	t := strings.Join(strings.Fields(terbilang(n)), " ") + " rupiah"
	return strings.ToUpper(t[:1]) + t[1:]
}

// ---------- rendering ----------

type pdfDoc struct {
	*fpdf.Fpdf
	tr func(string) string
}

func newPDF(title string) *pdfDoc {
	f := fpdf.New("P", "mm", "A4", "")
	f.SetMargins(15, 15, 15)
	f.SetAutoPageBreak(true, 18)
	f.SetTitle(title, true)
	f.SetCreator("BuildingVision", true)
	d := &pdfDoc{Fpdf: f, tr: f.UnicodeTranslatorFromDescriptor("")}
	f.SetFooterFunc(func() {
		f.SetY(-12)
		f.SetFont("Helvetica", "", 7)
		f.SetTextColor(130, 130, 130)
		f.CellFormat(0, 4, d.tr(fmt.Sprintf("%s · dibuat %s · halaman %d", title, time.Now().Format("02-01-2006 15:04"), f.PageNo())), "", 0, "C", false, 0, "")
	})
	f.AddPage()
	return d
}

func (d *pdfDoc) text(w, h float64, s, align string, style string, size float64) {
	d.SetFont("Helvetica", style, size)
	d.CellFormat(w, h, d.tr(s), "", 0, align, false, 0, "")
}

func (d *pdfDoc) multi(w, h float64, s string, style string, size float64) {
	d.SetFont("Helvetica", style, size)
	d.MultiCell(w, h, d.tr(s), "", "L", false)
}

func (d *pdfDoc) header(p docParty, title, number string, stamp string) {
	seller := p.st.SellerName
	name := p.orgName
	if seller != nil && *seller != "" {
		name = *seller
	} else if p.orgLegal != "" {
		name = p.orgLegal
	}
	d.SetTextColor(20, 60, 60)
	d.text(110, 7, name, "L", "B", 13)
	d.SetTextColor(20, 20, 20)
	d.text(0, 7, title, "R", "B", 16)
	d.Ln(7)
	d.SetTextColor(90, 90, 90)
	addr := p.orgAddress
	if p.st.SellerAddress != nil && *p.st.SellerAddress != "" {
		addr = *p.st.SellerAddress
	}
	d.text(110, 4.5, joinNonEmpty(" · ", p.propName, p.propAddress), "L", "", 8.5)
	d.text(0, 4.5, number, "R", "B", 10)
	d.Ln(4.5)
	if addr != "" {
		d.text(110, 4.5, addr, "L", "", 8.5)
		d.Ln(4.5)
	}
	tax := p.orgTaxID
	if p.st.SellerTaxID != nil && *p.st.SellerTaxID != "" {
		tax = *p.st.SellerTaxID
	}
	if tax != "" {
		d.text(110, 4.5, "NPWP "+tax, "L", "", 8.5)
		d.Ln(4.5)
	}
	d.SetTextColor(20, 20, 20)
	if stamp != "" {
		x, y := d.GetXY()
		d.SetDrawColor(200, 40, 40)
		d.SetTextColor(200, 40, 40)
		if stamp == "LUNAS" {
			d.SetDrawColor(20, 130, 80)
			d.SetTextColor(20, 130, 80)
		}
		d.SetLineWidth(0.6)
		d.SetXY(150, 34)
		d.SetFont("Helvetica", "B", 14)
		d.CellFormat(45, 10, d.tr(stamp), "1", 0, "C", false, 0, "")
		d.SetLineWidth(0.2)
		d.SetDrawColor(0, 0, 0)
		d.SetTextColor(20, 20, 20)
		d.SetXY(x, y)
	}
	d.Ln(3)
	d.SetDrawColor(20, 60, 60)
	d.Line(15, d.GetY(), 195, d.GetY())
	d.SetDrawColor(0, 0, 0)
	d.Ln(4)
}

func (d *pdfDoc) kv(label, value string) {
	d.SetTextColor(100, 100, 100)
	d.text(32, 5, label, "L", "", 8.5)
	d.SetTextColor(20, 20, 20)
	d.text(58, 5, value, "L", "B", 9)
}

func fmtDate(t *time.Time, loc *time.Location) string {
	if t == nil {
		return "—"
	}
	return fmt.Sprintf("%02d %s %d", t.In(loc).Day(), bulan[t.In(loc).Month()], t.In(loc).Year())
}

func fmtDateStr(s *string) string {
	if s == nil || *s == "" {
		return "—"
	}
	t, err := time.Parse("2006-01-02", *s)
	if err != nil {
		return *s
	}
	return fmt.Sprintf("%02d %s %d", t.Day(), bulan[t.Month()], t.Year())
}

func money(v int64) string { return rupiahText(v) }

var invoiceTypeLabels = map[string]string{"service_charge": "Service Charge", "ipl": "IPL", "utility": "Utilitas", "electricity": "Listrik", "water": "Air", "parking": "Parkir",
	"sinking_fund": "Sinking Fund", "penalty": "Denda", "deposit": "Deposit", "rental": "Sewa", "facility": "Fasilitas", "additional_charge": "Biaya Tambahan", "other": "Lainnya"}

func (s *Service) renderInvoiceTx(ctx context.Context, tx pgx.Tx, inv *Invoice) (*Document, error) {
	if err := loadItems(ctx, tx, inv); err != nil {
		return nil, err
	}
	p := s.partyTx(ctx, tx, inv.PropertyID)
	stamp := map[string]string{"paid": "LUNAS", "cancelled": "BATAL", "draft": "DRAFT"}[inv.Status]
	d := newPDF("Invoice " + inv.DisplayNumber)
	d.header(p, "INVOICE", inv.DisplayNumber, stamp)
	var tenantTax, contact *string
	if inv.TenantID != nil {
		_ = tx.QueryRow(ctx, `SELECT tax_id, contact_name FROM tenants WHERE id = $1`, *inv.TenantID).Scan(&tenantTax, &contact)
	}
	top := d.GetY()
	d.SetTextColor(100, 100, 100)
	d.text(90, 5, "Ditagihkan kepada", "L", "", 8.5)
	d.Ln(5)
	d.SetTextColor(20, 20, 20)
	billTo := strPtrVal(inv.TenantName)
	if billTo == "" {
		billTo = strPtrVal(inv.UnitLabel)
	}
	d.text(90, 5.5, billTo, "L", "B", 11)
	d.Ln(5.5)
	if inv.TenantName != nil && inv.UnitLabel != nil {
		d.text(90, 4.5, *inv.UnitLabel, "L", "", 9)
		d.Ln(4.5)
	}
	if contact != nil && *contact != "" {
		d.text(90, 4.5, "u.p. "+*contact, "L", "", 9)
		d.Ln(4.5)
	}
	if tenantTax != nil && *tenantTax != "" {
		d.text(90, 4.5, "NPWP "+*tenantTax, "L", "", 9)
		d.Ln(4.5)
	}
	bottom := d.GetY()
	d.SetXY(115, top)
	d.kv("Tanggal terbit", fmtDate(inv.IssuedAt, p.loc))
	d.SetXY(115, top+5)
	due := fmtDateStr(inv.DueDate)
	if due == "—" {
		due = fmtDate(&inv.DueAt, p.loc)
	}
	d.kv("Jatuh tempo", due)
	d.SetXY(115, top+10)
	d.kv("Jenis", invoiceTypeLabels[inv.InvoiceType])
	y := top + 15
	if inv.PeriodStart != nil && inv.PeriodEnd != nil {
		d.SetXY(115, y)
		d.kv("Periode", inv.PeriodStart.Format("02/01/2006")+" – "+inv.PeriodEnd.Format("02/01/2006"))
		y += 5
	}
	if inv.ExternalRef != nil && *inv.ExternalRef != "" {
		d.SetXY(115, y)
		d.kv("Ref.", *inv.ExternalRef)
		y += 5
	}
	if y > bottom {
		bottom = y
	}
	d.SetXY(15, bottom+4)
	if inv.Description != nil && *inv.Description != "" {
		d.multi(180, 4.5, *inv.Description, "", 9)
		d.Ln(2)
	}
	// tabel item
	cols := []float64{78, 16, 16, 28, 16, 26}
	head := []string{"Uraian", "Qty", "Satuan", "Harga", "Pajak", "Jumlah"}
	d.SetFillColor(230, 240, 240)
	d.SetFont("Helvetica", "B", 8.5)
	for i, h := range head {
		al := "R"
		if i == 0 || i == 2 {
			al = "L"
		}
		d.CellFormat(cols[i], 7, d.tr(h), "B", 0, al, true, 0, "")
	}
	d.Ln(-1)
	taxByRate := map[float64]int64{}
	for _, it := range inv.Items {
		rate := 0.0
		if it.TaxRate != nil {
			rate = *it.TaxRate
		}
		if it.TaxAmount > 0 {
			taxByRate[rate] += it.TaxAmount
		}
		d.SetFont("Helvetica", "", 8.5)
		x0, y0 := d.GetXY()
		d.MultiCell(cols[0], 4.5, d.tr(it.Description), "", "L", false)
		h := d.GetY() - y0
		if h < 6 {
			h = 6
		}
		d.SetXY(x0+cols[0], y0)
		qty := strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.2f", it.Quantity), "0"), ".")
		d.CellFormat(cols[1], 4.5, qty, "", 0, "R", false, 0, "")
		d.CellFormat(cols[2], 4.5, d.tr(" "+strPtrVal(it.Unit)), "", 0, "L", false, 0, "")
		d.CellFormat(cols[3], 4.5, money(it.UnitPrice), "", 0, "R", false, 0, "")
		tx := "—"
		if rate > 0 {
			tx = strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.2f", rate), "0"), ".") + "%"
		}
		d.CellFormat(cols[4], 4.5, d.tr(tx), "", 0, "R", false, 0, "")
		d.CellFormat(cols[5], 4.5, money(it.Amount), "", 0, "R", false, 0, "")
		d.SetXY(15, y0+h)
		d.SetDrawColor(225, 225, 225)
		d.Line(15, d.GetY(), 195, d.GetY())
		d.SetDrawColor(0, 0, 0)
		d.Ln(1)
	}
	d.Ln(2)
	sum := func(label, value string, bold bool) {
		d.SetX(115)
		style := ""
		if bold {
			style = "B"
		}
		d.text(46, 5.5, label, "L", style, 9)
		d.text(34, 5.5, value, "R", style, 9)
		d.Ln(5.5)
	}
	sum("Subtotal", money(inv.SubtotalAmount), false)
	if inv.TaxMode == "manual" && inv.TaxAmount > 0 {
		sum(p.st.TaxName, money(inv.TaxAmount), false)
	} else {
		for rate, amt := range taxByRate {
			sum(fmt.Sprintf("%s %s%%", p.st.TaxName, strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.2f", rate), "0"), ".")), money(amt), false)
		}
	}
	sum("Total", money(inv.TotalAmount), true)
	if inv.PaidAmount > 0 {
		sum("Dibayar", "-"+money(inv.PaidAmount), false)
	}
	if inv.CreditedAmount > 0 {
		sum("Koreksi (credit note)", "-"+money(inv.CreditedAmount), false)
	}
	if inv.Status != "cancelled" && inv.Status != "draft" {
		sum("Sisa tagihan", money(inv.OutstandingAmt), true)
	}
	d.Ln(4)
	if inv.Status != "paid" && inv.Status != "cancelled" {
		instr := strPtrVal(p.st.PaymentInstructions)
		if acct := s.bankAccountText(ctx, tx, inv.PropertyID); acct != "" {
			instr = joinNonEmpty("\n", instr, "Transfer ke: "+acct)
		}
		if instr != "" {
			d.SetFillColor(245, 248, 248)
			d.text(0, 6, "Cara pembayaran", "L", "B", 9.5)
			d.Ln(6)
			d.multi(180, 4.5, instr+"\nCantumkan nomor invoice "+inv.DisplayNumber+" pada berita transfer.", "", 9)
			d.Ln(2)
		}
	}
	if inv.Status == "cancelled" && inv.CancelReason != nil {
		d.multi(180, 4.5, "Dibatalkan: "+*inv.CancelReason, "I", 9)
	}
	if p.st.InvoiceFooter != nil && *p.st.InvoiceFooter != "" {
		d.Ln(2)
		d.SetTextColor(100, 100, 100)
		d.multi(180, 4, *p.st.InvoiceFooter, "", 8)
	}
	var buf bytes.Buffer
	if err := d.Output(&buf); err != nil {
		return nil, err
	}
	return &Document{Bytes: buf.Bytes(), FileName: "invoice-" + strings.ReplaceAll(inv.DisplayNumber, " ", "-") + ".pdf"}, nil
}

func (s *Service) renderReceiptTx(ctx context.Context, tx pgx.Tx, pay *Payment) (*Document, error) {
	if pay.Status != "paid" && pay.Status != "refunded" {
		return nil, apperr.Conflict("RECEIPT_NOT_AVAILABLE", "Kwitansi tersedia setelah pembayaran terverifikasi")
	}
	p := s.partyTx(ctx, tx, pay.PropertyID)
	number := pay.PaymentNumber
	if pay.ReceiptNumber != nil {
		number = *pay.ReceiptNumber
	}
	stamp := ""
	if pay.Status == "refunded" {
		stamp = "REFUND"
	}
	d := newPDF("Kwitansi " + number)
	d.header(p, "KWITANSI", number, stamp)
	var payer, unit *string
	var invType string
	_ = tx.QueryRow(ctx, `SELECT COALESCE(t.name, u.full_name), COALESCE('Unit ' || un.unit_number, l.name), i.invoice_type FROM payments p JOIN invoices i ON i.id = p.invoice_id
		LEFT JOIN tenants t ON t.id = i.tenant_id LEFT JOIN users u ON u.id = (SELECT tu.user_id FROM tenant_users tu WHERE tu.id = p.tenant_user_id)
		LEFT JOIN locations l ON l.id = i.unit_location_id LEFT JOIN units un ON un.location_id = i.unit_location_id WHERE p.id = $1`, pay.ID).Scan(&payer, &unit, &invType)
	d.kv("Telah diterima dari", joinNonEmpty(" · ", strPtrVal(payer), strPtrVal(unit)))
	d.Ln(6)
	d.kv("Tanggal", fmtDate(pay.PaidAt, p.loc))
	d.Ln(6)
	d.kv("Metode", map[string]string{"transfer": "Transfer bank", "cash": "Tunai", "card": "Kartu", "va": "Virtual account", "qris": "QRIS", "ewallet": "E-wallet", "check": "Cek/Giro", "credit": "Saldo kredit", "deposit": "Potong deposit", "other": "Lainnya"}[pay.Method])
	d.Ln(6)
	if pay.Reference != nil && *pay.Reference != "" {
		d.kv("Referensi", *pay.Reference)
		d.Ln(6)
	}
	d.Ln(3)
	// grup penerimaan (satu transfer untuk beberapa invoice)
	type alloc struct {
		inv    string
		amount int64
	}
	var allocs []alloc
	if pay.ReceiptGroup != nil && *pay.ReceiptGroup != "" {
		rows, err := tx.Query(ctx, `SELECT COALESCE(i.invoice_number,''), p.amount FROM payments p JOIN invoices i ON i.id = p.invoice_id WHERE p.receipt_group = $1 AND p.status IN ('paid','refunded') ORDER BY p.created_at`, *pay.ReceiptGroup)
		if err == nil {
			for rows.Next() {
				var a alloc
				if rows.Scan(&a.inv, &a.amount) == nil {
					allocs = append(allocs, a)
				}
			}
			rows.Close()
		}
	}
	if len(allocs) <= 1 {
		allocs = []alloc{{pay.InvoiceNumber, pay.Amount}}
	}
	var total int64
	d.SetFillColor(230, 240, 240)
	d.SetFont("Helvetica", "B", 9)
	d.CellFormat(130, 7, d.tr("Untuk pembayaran"), "B", 0, "L", true, 0, "")
	d.CellFormat(50, 7, d.tr("Jumlah"), "B", 1, "R", true, 0, "")
	for _, a := range allocs {
		d.SetFont("Helvetica", "", 9)
		d.CellFormat(130, 6, d.tr("Invoice "+a.inv+" ("+invoiceTypeLabels[invType]+")"), "", 0, "L", false, 0, "")
		d.CellFormat(50, 6, money(a.amount), "", 1, "R", false, 0, "")
		total += a.amount
	}
	d.SetFont("Helvetica", "B", 10)
	d.CellFormat(130, 8, d.tr("Total diterima"), "T", 0, "L", false, 0, "")
	d.CellFormat(50, 8, money(total), "T", 1, "R", false, 0, "")
	d.Ln(2)
	d.multi(180, 5, "Terbilang: "+Terbilang(total), "I", 9)
	if pay.ReceiptGroup != nil && len(allocs) > 1 {
		d.multi(180, 4.5, "Grup penerimaan "+*pay.ReceiptGroup, "", 8.5)
	}
	if pay.Status == "refunded" {
		d.Ln(2)
		d.multi(180, 4.5, "Pembayaran ini telah dikembalikan (refund).", "B", 9)
	}
	d.Ln(10)
	d.SetX(135)
	d.text(60, 5, fmtDate(pay.PaidAt, p.loc), "C", "", 9)
	d.Ln(18)
	d.SetX(135)
	name := p.orgName
	if p.st.SellerName != nil && *p.st.SellerName != "" {
		name = *p.st.SellerName
	}
	d.text(60, 5, name, "C", "B", 9)
	d.Ln(8)
	d.SetTextColor(110, 110, 110)
	d.multi(180, 4, "Kwitansi ini dibuat secara elektronik oleh BuildingVision dan sah tanpa tanda tangan basah.", "", 7.5)
	var buf bytes.Buffer
	if err := d.Output(&buf); err != nil {
		return nil, err
	}
	return &Document{Bytes: buf.Bytes(), FileName: "kwitansi-" + number + ".pdf"}, nil
}

func (s *Service) renderStatementTx(ctx context.Context, tx pgx.Tx, st *Statement) (*Document, error) {
	p := s.partyTx(ctx, tx, st.PropertyID)
	d := newPDF("Statement of Account")
	d.header(p, "STATEMENT", fmtDateStr(&st.From)+" – "+fmtDateStr(&st.To), "")
	party := joinNonEmpty(" · ", strPtrVal(st.TenantName), strPtrVal(st.UnitLabel))
	d.kv("Pihak", party)
	d.Ln(6)
	d.kv("Saldo awal", money(st.Opening))
	d.Ln(8)
	cols := []float64{22, 30, 68, 20, 20, 20}
	head := []string{"Tanggal", "Referensi", "Keterangan", "Debit", "Kredit", "Saldo"}
	d.SetFillColor(230, 240, 240)
	d.SetFont("Helvetica", "B", 8)
	for i, h := range head {
		al := "R"
		if i < 3 {
			al = "L"
		}
		d.CellFormat(cols[i], 6.5, d.tr(h), "B", 0, al, true, 0, "")
	}
	d.Ln(-1)
	short := func(v int64) string {
		if v == 0 {
			return ""
		}
		return strings.TrimPrefix(money(v), "Rp")
	}
	for _, e := range st.Entries {
		d.SetFont("Helvetica", "", 7.5)
		desc := e.Description
		if len(desc) > 60 {
			desc = desc[:57] + "..."
		}
		d.CellFormat(cols[0], 5.5, e.Date.In(p.loc).Format("02/01/2006"), "", 0, "L", false, 0, "")
		d.CellFormat(cols[1], 5.5, d.tr(strPtrVal(e.Reference)), "", 0, "L", false, 0, "")
		d.CellFormat(cols[2], 5.5, d.tr(desc), "", 0, "L", false, 0, "")
		d.CellFormat(cols[3], 5.5, short(e.Debit), "", 0, "R", false, 0, "")
		d.CellFormat(cols[4], 5.5, short(e.Credit), "", 0, "R", false, 0, "")
		d.CellFormat(cols[5], 5.5, d.tr(strings.TrimPrefix(money(e.Balance), "Rp")), "", 1, "R", false, 0, "")
	}
	if len(st.Entries) == 0 {
		d.multi(180, 6, "Tidak ada transaksi pada periode ini.", "I", 9)
	}
	d.Ln(3)
	line := func(label, v string) {
		d.SetX(115)
		d.text(46, 5.5, label, "L", "B", 9)
		d.text(34, 5.5, v, "R", "B", 9)
		d.Ln(5.5)
	}
	line("Saldo akhir", money(st.Closing))
	line("Sisa tagihan terbuka", money(st.Outstanding))
	if st.CreditBalance != 0 {
		line("Saldo kredit", money(st.CreditBalance))
	}
	if st.DepositBalance != 0 {
		line("Saldo deposit", money(st.DepositBalance))
	}
	d.Ln(3)
	d.SetTextColor(110, 110, 110)
	d.multi(180, 4, "Saldo positif = jumlah terutang; negatif = kelebihan bayar (kredit).", "", 7.5)
	var buf bytes.Buffer
	if err := d.Output(&buf); err != nil {
		return nil, err
	}
	return &Document{Bytes: buf.Bytes(), FileName: "statement-" + st.From + "-" + st.To + ".pdf"}, nil
}

// ---------- akses (staf / tenant) ----------

func (s *Service) InvoicePDF(ctx context.Context, id uuid.UUID) (*Document, error) {
	var out *Document
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		inv, err := s.getTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := canViewAt(ctx, tx, "billing.invoices.view", inv.PropertyID, inv.UnitLocationID); err != nil {
			return err
		}
		out, err = s.renderInvoiceTx(ctx, tx, inv)
		return err
	})
	return out, err
}

func (s *Service) ReceiptPDF(ctx context.Context, paymentID uuid.UUID) (*Document, error) {
	var out *Document
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		pay, err := s.getPaymentTx(ctx, tx, paymentID)
		if err != nil {
			return err
		}
		if err := canViewAt(ctx, tx, "billing.payments.view", pay.PropertyID, invoiceUnitTx(ctx, tx, pay.InvoiceID)); err != nil {
			return err
		}
		out, err = s.renderReceiptTx(ctx, tx, pay)
		return err
	})
	return out, err
}

func (s *Service) StatementPDF(ctx context.Context, propertyID uuid.UUID, tenantID, unitID *uuid.UUID, from, to *string) (*Document, error) {
	st, err := s.Statement(ctx, propertyID, tenantID, unitID, from, to)
	if err != nil {
		return nil, err
	}
	var out *Document
	err = s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		out, err = s.renderStatementTx(ctx, tx, st)
		return err
	})
	return out, err
}

func (s *Service) TenantInvoicePDF(ctx context.Context, id uuid.UUID) (*Document, error) {
	var out *Document
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		sc, err := tenantscope.LoadScopeTx(ctx, tx)
		if err != nil {
			return err
		}
		inv, err := s.tenantInvoiceTx(ctx, tx, sc, id)
		if err != nil {
			return err
		}
		if inv.Status == "draft" {
			return apperr.NotFound("Invoice")
		}
		out, err = s.renderInvoiceTx(ctx, tx, inv)
		return err
	})
	return out, err
}

func (s *Service) tenantPaymentTx(ctx context.Context, tx pgx.Tx, paymentID uuid.UUID) (*Payment, error) {
	sc, err := tenantscope.LoadScopeTx(ctx, tx)
	if err != nil {
		return nil, err
	}
	pay, err := s.getPaymentTx(ctx, tx, paymentID)
	if err != nil {
		return nil, err
	}
	if _, err := s.tenantInvoiceTx(ctx, tx, sc, pay.InvoiceID); err != nil {
		return nil, apperr.NotFound("Payment")
	}
	return pay, nil
}

func (s *Service) TenantReceiptPDF(ctx context.Context, paymentID uuid.UUID) (*Document, error) {
	var out *Document
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		pay, err := s.tenantPaymentTx(ctx, tx, paymentID)
		if err != nil {
			return err
		}
		out, err = s.renderReceiptTx(ctx, tx, pay)
		return err
	})
	return out, err
}

func (s *Service) TenantStatementPDF(ctx context.Context, from, to *string) (*Document, error) {
	st, err := s.TenantStatement(ctx, from, to)
	if err != nil {
		return nil, err
	}
	var out *Document
	err = s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		out, err = s.renderStatementTx(ctx, tx, st)
		return err
	})
	return out, err
}

// ---------- tautan bertanda tangan (public) ----------

type docToken struct {
	Kind     string     `json:"k"` // invoice | receipt | statement
	ID       uuid.UUID  `json:"i,omitempty"`
	Org      uuid.UUID  `json:"o"`
	Exp      int64      `json:"e"`
	Property *uuid.UUID `json:"p,omitempty"`
	Tenant   *uuid.UUID `json:"t,omitempty"`
	Unit     *uuid.UUID `json:"u,omitempty"`
	From     string     `json:"f,omitempty"`
	To       string     `json:"to,omitempty"`
}

type DocumentLink struct {
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expires_at"`
	FileName  string    `json:"file_name,omitempty"`
}

const documentLinkTTL = 72 * time.Hour

func (s *Service) sign(t docToken) (string, error) {
	if len(s.DocumentSecret) == 0 {
		return "", apperr.Conflict("DOCUMENT_LINKS_DISABLED", "Tautan dokumen belum dikonfigurasi")
	}
	b, _ := json.Marshal(t)
	payload := base64.RawURLEncoding.EncodeToString(b)
	m := hmac.New(sha256.New, s.DocumentSecret)
	m.Write([]byte(payload))
	return payload + "." + base64.RawURLEncoding.EncodeToString(m.Sum(nil)), nil
}

func (s *Service) verifyToken(token string) (*docToken, error) {
	parts := strings.SplitN(token, ".", 2)
	if len(parts) != 2 || len(s.DocumentSecret) == 0 {
		return nil, apperr.NotFound("Dokumen")
	}
	m := hmac.New(sha256.New, s.DocumentSecret)
	m.Write([]byte(parts[0]))
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || !hmac.Equal(sig, m.Sum(nil)) {
		return nil, apperr.NotFound("Dokumen")
	}
	b, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, apperr.NotFound("Dokumen")
	}
	var t docToken
	if err := json.Unmarshal(b, &t); err != nil {
		return nil, apperr.NotFound("Dokumen")
	}
	if time.Now().Unix() > t.Exp {
		return nil, apperr.Conflict("LINK_EXPIRED", "Tautan dokumen sudah kedaluwarsa; minta tautan baru")
	}
	return &t, nil
}

func (s *Service) linkFor(t docToken) (*DocumentLink, error) {
	exp := time.Now().Add(documentLinkTTL)
	t.Exp = exp.Unix()
	tok, err := s.sign(t)
	if err != nil {
		return nil, err
	}
	base := strings.TrimRight(s.PublicURL, "/")
	return &DocumentLink{URL: base + "/api/v1/public/documents/" + tok, ExpiresAt: exp}, nil
}

// DocumentLinkFor: staf (dengan izin lihat) atau tenant (scope sendiri) meminta tautan dokumen yang dapat dibagikan.
func (s *Service) DocumentLinkFor(ctx context.Context, kind string, id uuid.UUID, tenant bool) (*DocumentLink, error) {
	p := authctx.Must(ctx)
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		switch kind {
		case "invoice":
			if tenant {
				sc, err := tenantscope.LoadScopeTx(ctx, tx)
				if err != nil {
					return err
				}
				inv, err := s.tenantInvoiceTx(ctx, tx, sc, id)
				if err != nil {
					return err
				}
				if inv.Status == "draft" {
					return apperr.NotFound("Invoice")
				}
				return nil
			}
			inv, err := s.getTx(ctx, tx, id)
			if err != nil {
				return err
			}
			return canViewAt(ctx, tx, "billing.invoices.view", inv.PropertyID, inv.UnitLocationID)
		case "receipt":
			if tenant {
				pay, err := s.tenantPaymentTx(ctx, tx, id)
				if err != nil {
					return err
				}
				if pay.Status != "paid" && pay.Status != "refunded" {
					return apperr.Conflict("RECEIPT_NOT_AVAILABLE", "Kwitansi tersedia setelah pembayaran terverifikasi")
				}
				return nil
			}
			pay, err := s.getPaymentTx(ctx, tx, id)
			if err != nil {
				return err
			}
			if pay.Status != "paid" && pay.Status != "refunded" {
				return apperr.Conflict("RECEIPT_NOT_AVAILABLE", "Kwitansi tersedia setelah pembayaran terverifikasi")
			}
			return canViewAt(ctx, tx, "billing.payments.view", pay.PropertyID, invoiceUnitTx(ctx, tx, pay.InvoiceID))
		}
		return apperr.NotFound("Dokumen")
	})
	if err != nil {
		return nil, err
	}
	return s.linkFor(docToken{Kind: kind, ID: id, Org: p.OrganizationID})
}

// StatementLink: tautan statement (staf per tenant/unit; tenant → pihaknya sendiri).
func (s *Service) StatementLink(ctx context.Context, propertyID *uuid.UUID, tenantID, unitID *uuid.UUID, from, to *string, tenant bool) (*DocumentLink, error) {
	p := authctx.Must(ctx)
	var st *Statement
	var err error
	if tenant {
		st, err = s.TenantStatement(ctx, from, to)
	} else {
		if propertyID == nil {
			return nil, apperr.Validation("property_id wajib").WithField("property_id", "wajib")
		}
		st, err = s.Statement(ctx, *propertyID, tenantID, unitID, from, to)
	}
	if err != nil {
		return nil, err
	}
	pid := st.PropertyID
	t := docToken{Kind: "statement", Org: p.OrganizationID, Property: &pid, Tenant: st.TenantID, Unit: st.UnitLocationID, From: st.From, To: st.To}
	if tenant && st.TenantID == nil {
		// tenant user tanpa tenant: statement scope unit (hanya invoice tanpa tenant reservasi pihak lain — dihitung ulang via unit)
		t.Unit = st.UnitLocationID
	}
	return s.linkFor(t)
}

// RenderSigned: endpoint publik — verifikasi token lalu render sebagai principal sistem organisasi token.
func (s *Service) RenderSigned(ctx context.Context, token string) (*Document, error) {
	t, err := s.verifyToken(token)
	if err != nil {
		return nil, err
	}
	ctx = authctx.With(ctx, authctx.System(t.Org))
	var out *Document
	err = s.DB.WithOrgTx(ctx, t.Org, func(ctx context.Context, tx pgx.Tx) error {
		switch t.Kind {
		case "invoice":
			inv, err := s.getTx(ctx, tx, t.ID)
			if err != nil {
				return err
			}
			out, err = s.renderInvoiceTx(ctx, tx, inv)
			return err
		case "receipt":
			pay, err := s.getPaymentTx(ctx, tx, t.ID)
			if err != nil {
				return err
			}
			out, err = s.renderReceiptTx(ctx, tx, pay)
			return err
		case "statement":
			if t.Property == nil {
				return apperr.NotFound("Dokumen")
			}
			loc := property.PropertyTimezone(ctx, tx, *t.Property)
			f, to, err := statementRange(&t.From, &t.To, loc)
			if err != nil {
				return err
			}
			invCond, ledgerCond := "i.property_id = $1 AND i.tenant_id = $2", "ce.property_id = $1 AND ce.tenant_id = $2"
			args := []any{*t.Property, t.Tenant}
			if t.Tenant == nil {
				invCond, ledgerCond = "i.property_id = $1 AND i.unit_location_id = $2 AND (i.tenant_id IS NOT NULL OR i.source NOT IN ('rental','unit_sale','hotel'))", "ce.property_id = $1 AND ce.unit_location_id = $2 AND ce.tenant_id IS NULL"
				args = []any{*t.Property, t.Unit}
			}
			st, err := statementTx(ctx, tx, invCond, ledgerCond, args, f, to, loc)
			if err != nil {
				return err
			}
			st.PropertyID, st.TenantID, st.UnitLocationID = *t.Property, t.Tenant, t.Unit
			if t.Tenant != nil {
				_ = tx.QueryRow(ctx, `SELECT name FROM tenants WHERE id = $1`, *t.Tenant).Scan(&st.TenantName)
			}
			if t.Unit != nil {
				_ = tx.QueryRow(ctx, `SELECT COALESCE('Unit ' || un.unit_number, l.name) FROM locations l LEFT JOIN units un ON un.location_id = l.id WHERE l.id = $1`, *t.Unit).Scan(&st.UnitLabel)
			}
			out, err = s.renderStatementTx(ctx, tx, st)
			return err
		}
		return apperr.NotFound("Dokumen")
	})
	return out, err
}
