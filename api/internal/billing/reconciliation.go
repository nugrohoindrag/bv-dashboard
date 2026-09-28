package billing

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/csv"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/xuri/excelize/v2"

	"github.com/buildingvision/api/internal/audit"
	"github.com/buildingvision/api/internal/platform/apperr"
	"github.com/buildingvision/api/internal/platform/authctx"
	"github.com/buildingvision/api/internal/platform/httpx"
	"github.com/buildingvision/api/internal/property"
)

// ---------- Rekonsiliasi: impor mutasi rekening (PRD P4 v2.1 §6.4 P4-REC-01..03, D-P4-04) ----------
// File CSV/XLSX ekspor internet banking (format bank umum: kolom tanggal, keterangan, debet/kredit atau jumlah + CR/DB, saldo)
// → baris mutasi. Kolom dideteksi otomatis dari judul (dapat dipetakan manual). Mutasi kredit dicocokkan otomatis ke pembayaran
// pending / invoice terbuka (nomor dokumen di berita transfer, jumlah, tanggal) → saran dengan skor; Finance mengonfirmasi
// (satu per satu atau massal skor ≥ 90), mencocokkan manual, atau mengabaikan. Konfirmasi membuat/memverifikasi pembayaran.

type ImportInput struct {
	PropertyID    uuid.UUID         `json:"property_id"`
	BankAccountID *uuid.UUID        `json:"bank_account_id"`
	FileName      string            `json:"file_name"`
	ContentBase64 string            `json:"content_base64"`
	Content       string            `json:"content"` // alternatif: teks CSV
	Columns       map[string]string `json:"columns"` // date, description, reference, amount, credit, debit, balance, type → judul kolom atau #indeks (mulai 1)
	DateFormat    string            `json:"date_format"`
}

type StatementImport struct {
	ID            uuid.UUID  `json:"id"`
	PropertyID    uuid.UUID  `json:"property_id"`
	PropertyName  string     `json:"property_name"`
	BankAccountID *uuid.UUID `json:"bank_account_id"`
	BankAccount   *string    `json:"bank_account"`
	FileName      *string    `json:"file_name"`
	Format        string     `json:"format"`
	PeriodFrom    *string    `json:"period_from"`
	PeriodTo      *string    `json:"period_to"`
	LineCount     int        `json:"line_count"`
	CreditCount   int        `json:"credit_count"`
	MatchedCount  int        `json:"matched_count"`
	SuggestedCnt  int        `json:"suggested_count"`
	UnmatchedCnt  int        `json:"unmatched_count"`
	IgnoredCount  int        `json:"ignored_count"`
	CreditTotal   int64      `json:"credit_total"`
	MatchedTotal  int64      `json:"matched_total"`
	Duplicates    int        `json:"duplicates_skipped,omitempty"`
	Status        string     `json:"status"`
	ImportedBy    *string    `json:"imported_by_name"`
	ImportedAt    time.Time  `json:"imported_at"`
	Columns       []string   `json:"detected_columns,omitempty"`
	Lines         []StmtLine `json:"lines,omitempty"`
}

type StmtLine struct {
	ID               uuid.UUID  `json:"id"`
	LineNo           int        `json:"line_no"`
	TxnDate          string     `json:"txn_date"`
	Description      *string    `json:"description"`
	Reference        *string    `json:"reference"`
	Amount           int64      `json:"amount"`
	Balance          *int64     `json:"balance"`
	Status           string     `json:"status"`
	MatchType        *string    `json:"match_type"`
	SuggestedPayment *uuid.UUID `json:"suggested_payment_id"`
	SuggestedPayNum  *string    `json:"suggested_payment_number"`
	SuggestedInvoice *uuid.UUID `json:"suggested_invoice_id"`
	SuggestedInvNum  *string    `json:"suggested_invoice_number"`
	SuggestedParty   *string    `json:"suggested_party"`
	MatchScore       *int       `json:"match_score"`
	PaymentID        *uuid.UUID `json:"payment_id"`
	PaymentNumber    *string    `json:"payment_number"`
	InvoiceID        *uuid.UUID `json:"invoice_id"`
	InvoiceNumber    *string    `json:"invoice_number"`
	Difference       *int64     `json:"difference"`
	MatchedByName    *string    `json:"matched_by_name"`
	MatchedAt        *time.Time `json:"matched_at"`
	Note             *string    `json:"note"`
	AllowedActions   []string   `json:"allowed_actions"`
}

// ---- parsing ----

var colSynonyms = map[string][]string{
	"date":        {"tanggal transaksi", "tgl transaksi", "tanggal", "tgl", "transaction date", "trans date", "post date", "posting date", "date", "tanggal mutasi"},
	"description": {"keterangan", "deskripsi", "description", "uraian", "uraian transaksi", "remarks", "remark", "berita", "transaction description", "detail"},
	"reference":   {"no. referensi", "no referensi", "referensi", "reference", "ref", "ref no", "no. ref", "nomor referensi", "reference no"},
	"credit":      {"kredit", "credit", "mutasi kredit", "cr", "kredit (idr)", "credit amount", "masuk"},
	"debit":       {"debet", "debit", "mutasi debet", "db", "debet (idr)", "debit amount", "keluar"},
	"amount":      {"jumlah", "amount", "nominal", "mutasi", "nilai", "transaction amount"},
	"balance":     {"saldo", "balance", "saldo akhir", "running balance", "ending balance"},
	"type":        {"cr/db", "db/cr", "d/k", "k/d", "dk", "tipe", "type", "jenis"},
}

func normHeader(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(strings.Trim(s, "\ufeff\"")))), " ")
}

func readRows(fileName string, data []byte) ([][]string, string, error) {
	name := strings.ToLower(fileName)
	if strings.HasSuffix(name, ".xlsx") || strings.HasSuffix(name, ".xlsm") || bytes.HasPrefix(data, []byte("PK\x03\x04")) {
		f, err := excelize.OpenReader(bytes.NewReader(data))
		if err != nil {
			return nil, "", apperr.Validation("File Excel tidak dapat dibaca: " + err.Error())
		}
		defer f.Close()
		sheets := f.GetSheetList()
		if len(sheets) == 0 {
			return nil, "", apperr.Validation("File Excel kosong")
		}
		rows, err := f.GetRows(sheets[0], excelize.Options{RawCellValue: false})
		if err != nil {
			return nil, "", apperr.Validation("Sheet tidak dapat dibaca")
		}
		return rows, "xlsx", nil
	}
	text := string(bytes.TrimPrefix(data, []byte("\ufeff")))
	lines := strings.SplitN(text, "\n", 20)
	best, bestN := ',', -1
	for _, d := range []rune{',', ';', '\t', '|'} {
		n := 0
		for _, l := range lines {
			n += strings.Count(l, string(d))
		}
		if n > bestN {
			best, bestN = d, n
		}
	}
	r := csv.NewReader(strings.NewReader(text))
	r.Comma = best
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	r.TrimLeadingSpace = true
	rows, err := r.ReadAll()
	if err != nil {
		return nil, "", apperr.Validation("CSV tidak dapat dibaca: " + err.Error())
	}
	return rows, "csv", nil
}

// detectColumns: cari baris judul (maks. 20 baris pertama) & indeks kolom; override dari mapping manual.
func detectColumns(rows [][]string, mapping map[string]string) (int, map[string]int, error) {
	pick := func(row []string, key string) int {
		if v, ok := mapping[key]; ok && strings.TrimSpace(v) != "" {
			v = strings.TrimSpace(v)
			if strings.HasPrefix(v, "#") {
				if n, err := strconv.Atoi(v[1:]); err == nil && n >= 1 {
					return n - 1
				}
			}
			for i, h := range row {
				if normHeader(h) == normHeader(v) {
					return i
				}
			}
			return -2 // dipetakan tapi tidak ditemukan
		}
		for _, syn := range colSynonyms[key] {
			for i, h := range row {
				if normHeader(h) == syn {
					return i
				}
			}
		}
		return -1
	}
	for ri := 0; ri < len(rows) && ri < 20; ri++ {
		row := rows[ri]
		cols := map[string]int{}
		for k := range colSynonyms {
			cols[k] = pick(row, k)
		}
		if cols["date"] >= 0 && (cols["amount"] >= 0 || cols["credit"] >= 0) {
			for k, v := range cols {
				if v == -2 {
					return 0, nil, apperr.Validation("Kolom '"+mapping[k]+"' tidak ditemukan").WithField("columns."+k, "tidak ditemukan")
				}
			}
			return ri, cols, nil
		}
	}
	return 0, nil, apperr.Validation("Baris judul tidak ditemukan: file harus memiliki kolom tanggal dan kredit/jumlah (atau petakan kolom secara manual)").WithField("columns", "tidak terdeteksi")
}

var amountClean = regexp.MustCompile(`[^0-9.,\-]`)

// ParseAmount: "1.500.000,00", "1,500,000.00", "1500000", "Rp 1.500.000", "(1.000)", "1,500.00 CR" → rupiah (dibulatkan).
func ParseAmount(raw string) (int64, string, bool) {
	s := strings.ToUpper(strings.TrimSpace(raw))
	if s == "" || s == "-" {
		return 0, "", false
	}
	sign := ""
	switch {
	case strings.HasSuffix(s, "CR") || strings.HasSuffix(s, " K"):
		sign = "CR"
	case strings.HasSuffix(s, "DB") || strings.HasSuffix(s, "DR") || strings.HasSuffix(s, " D"):
		sign = "DB"
	}
	neg := strings.HasPrefix(s, "(") && strings.HasSuffix(strings.TrimRight(s, " CRDBK"), ")")
	s = amountClean.ReplaceAllString(s, "")
	if strings.HasPrefix(s, "-") {
		neg = true
	}
	s = strings.ReplaceAll(s, "-", "")
	if s == "" {
		return 0, "", false
	}
	lastDot, lastComma := strings.LastIndex(s, "."), strings.LastIndex(s, ",")
	intPart, frac := s, ""
	switch {
	case lastDot >= 0 && lastComma >= 0:
		if lastDot > lastComma {
			intPart, frac = s[:lastDot], s[lastDot+1:]
		} else {
			intPart, frac = s[:lastComma], s[lastComma+1:]
		}
	case lastDot >= 0 || lastComma >= 0:
		sep := "."
		idx := lastDot
		if lastComma >= 0 {
			sep, idx = ",", lastComma
		}
		if strings.Count(s, sep) == 1 && len(s)-idx-1 != 3 {
			intPart, frac = s[:idx], s[idx+1:]
		}
	}
	intPart = strings.NewReplacer(".", "", ",", "").Replace(intPart)
	if intPart == "" {
		intPart = "0"
	}
	v, err := strconv.ParseInt(intPart, 10, 64)
	if err != nil {
		return 0, "", false
	}
	if len(frac) > 0 && frac[0] >= '5' && frac[0] <= '9' {
		v++
	}
	if neg {
		v = -v
	}
	return v, sign, true
}

var dateLayouts = []string{"02/01/2006", "2/1/2006", "02-01-2006", "2-1-2006", "2006-01-02", "2006/01/02", "02/01/06", "02-01-06", "02 Jan 2006", "2 Jan 2006", "02-Jan-2006", "02 January 2006", "02.01.2006", "01-02-06", "01/02/2006 15:04", "02/01/2006 15:04:05", "2006-01-02 15:04:05", "2006-01-02T15:04:05"}

var bulanID = strings.NewReplacer("Mei", "May", "Agu", "Aug", "Agt", "Aug", "Okt", "Oct", "Des", "Dec", "Januari", "January", "Februari", "February", "Maret", "March", "Juni", "June", "Juli", "July", "Agustus", "August", "Oktober", "October", "Desember", "December")

func parseTxnDate(raw, layout string, fallbackYear int) (time.Time, bool) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return time.Time{}, false
	}
	s = bulanID.Replace(s)
	if layout != "" {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	// serial Excel
	if n, err := strconv.ParseFloat(s, 64); err == nil && n > 30000 && n < 80000 {
		return time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC).AddDate(0, 0, int(n)), true
	}
	for _, l := range dateLayouts {
		if t, err := time.Parse(l, s); err == nil {
			return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC), true
		}
	}
	// BCA: "05/09" tanpa tahun
	if t, err := time.Parse("02/01", s); err == nil && fallbackYear > 0 {
		return time.Date(fallbackYear, t.Month(), t.Day(), 0, 0, 0, 0, time.UTC), true
	}
	return time.Time{}, false
}

type parsedLine struct {
	no        int
	date      time.Time
	desc, ref string
	amount    int64
	balance   *int64
}

func parseStatement(rows [][]string, mapping map[string]string, layout string) ([]parsedLine, []string, error) {
	hdr, cols, err := detectColumns(rows, mapping)
	if err != nil {
		return nil, nil, err
	}
	get := func(row []string, key string) string {
		i := cols[key]
		if i < 0 || i >= len(row) {
			return ""
		}
		return strings.TrimSpace(row[i])
	}
	detected := []string{}
	for _, k := range []string{"date", "description", "reference", "amount", "credit", "debit", "type", "balance"} {
		if cols[k] >= 0 && cols[k] < len(rows[hdr]) {
			detected = append(detected, k+": "+strings.TrimSpace(rows[hdr][cols[k]]))
		}
	}
	year := time.Now().Year()
	var out []parsedLine
	for ri := hdr + 1; ri < len(rows); ri++ {
		row := rows[ri]
		d, ok := parseTxnDate(get(row, "date"), layout, year)
		if !ok {
			continue // baris ringkasan/penutup (Saldo Awal, Total, dsb.)
		}
		var amount int64
		if cols["credit"] >= 0 || cols["debit"] >= 0 {
			cr, _, okc := ParseAmount(get(row, "credit"))
			db, _, okd := ParseAmount(get(row, "debit"))
			if !okc && !okd {
				continue
			}
			if cr < 0 {
				cr = -cr
			}
			if db < 0 {
				db = -db
			}
			amount = cr - db
		} else {
			v, sign, ok := ParseAmount(get(row, "amount"))
			if !ok {
				continue
			}
			t := strings.ToUpper(get(row, "type"))
			switch {
			case sign == "DB" || strings.HasPrefix(t, "D") || strings.HasPrefix(t, "DB"):
				if v > 0 {
					v = -v
				}
			case sign == "CR" || strings.HasPrefix(t, "C") || strings.HasPrefix(t, "K"):
				if v < 0 {
					v = -v
				}
			}
			amount = v
		}
		if amount == 0 {
			continue
		}
		l := parsedLine{no: ri + 1, date: d, desc: get(row, "description"), ref: get(row, "reference"), amount: amount}
		if b, _, ok := ParseAmount(get(row, "balance")); ok {
			l.balance = &b
		}
		out = append(out, l)
	}
	if len(out) == 0 {
		return nil, detected, apperr.Validation("Tidak ada baris mutasi yang terbaca (periksa format tanggal/jumlah atau pemetaan kolom)").WithField("columns", "tidak ada baris")
	}
	return out, detected, nil
}

// ---- import & match ----

func (s *Service) ImportStatement(ctx context.Context, in ImportInput) (*StatementImport, error) {
	p := authctx.Must(ctx)
	if !p.HasOnProperty("billing.reconciliation.manage", in.PropertyID) {
		return nil, apperr.Forbidden("Memerlukan billing.reconciliation.manage")
	}
	var data []byte
	switch {
	case in.ContentBase64 != "":
		b, err := base64.StdEncoding.DecodeString(in.ContentBase64)
		if err != nil {
			return nil, apperr.Validation("content_base64 tidak valid").WithField("content_base64", "bukan base64")
		}
		data = b
	case in.Content != "":
		data = []byte(in.Content)
	default:
		return nil, apperr.Validation("File mutasi wajib").WithField("content_base64", "wajib")
	}
	if len(data) > 8<<20 {
		return nil, apperr.Validation("File maksimal 8 MB")
	}
	rows, format, err := readRows(in.FileName, data)
	if err != nil {
		return nil, err
	}
	lines, detected, err := parseStatement(rows, in.Columns, in.DateFormat)
	if err != nil {
		return nil, err
	}
	var out *StatementImport
	err = s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if in.BankAccountID != nil {
			var pid *uuid.UUID
			if err := tx.QueryRow(ctx, `SELECT property_id FROM bank_accounts WHERE id = $1`, *in.BankAccountID).Scan(&pid); err != nil || (pid != nil && *pid != in.PropertyID) {
				return apperr.Validation("bank_account_id tidak valid untuk property ini").WithField("bank_account_id", "tidak valid")
			}
		}
		minD, maxD := lines[0].date, lines[0].date
		for _, l := range lines {
			if l.date.Before(minD) {
				minD = l.date
			}
			if l.date.After(maxD) {
				maxD = l.date
			}
		}
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `INSERT INTO bank_statement_imports (organization_id, property_id, bank_account_id, file_name, format, period_from, period_to, imported_by)
			VALUES ($1,$2,$3,NULLIF($4,''),$5,$6,$7,$8) RETURNING id`, p.OrganizationID, in.PropertyID, in.BankAccountID, in.FileName, format, minD, maxD, p.UserID).Scan(&id); err != nil {
			return err
		}
		dups := 0
		for _, l := range lines {
			var dup bool
			_ = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM bank_statement_lines bl JOIN bank_statement_imports bi ON bi.id = bl.import_id WHERE bl.property_id = $1 AND bl.import_id <> $2
				AND bl.txn_date = $3 AND bl.amount = $4 AND COALESCE(bl.description,'') = $5 AND COALESCE(bl.reference,'') = $6 AND bl.balance IS NOT DISTINCT FROM $7
				AND bi.bank_account_id IS NOT DISTINCT FROM $8)`, in.PropertyID, id, l.date, l.amount, l.desc, l.ref, l.balance, in.BankAccountID).Scan(&dup)
			if dup {
				dups++
				continue
			}
			status, note := "unmatched", ""
			if l.amount < 0 {
				status, note = "ignored", "Mutasi debit (uang keluar)"
			}
			if _, err := tx.Exec(ctx, `INSERT INTO bank_statement_lines (organization_id, import_id, property_id, line_no, txn_date, description, reference, amount, balance, status, note)
				VALUES ($1,$2,$3,$4,$5,NULLIF($6,''),NULLIF($7,''),$8,$9,$10,NULLIF($11,''))`, p.OrganizationID, id, in.PropertyID, l.no, l.date, l.desc, l.ref, l.amount, l.balance, status, note); err != nil {
				return err
			}
		}
		if err := s.autoMatchTx(ctx, tx, id); err != nil {
			return err
		}
		if err := refreshImportTx(ctx, tx, id); err != nil {
			return err
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditCreate, EntityType: "bank_statement_import", EntityID: &id, EntityLabel: in.FileName, After: map[string]any{"lines": len(lines), "duplicates": dups, "format": format}})
		var err error
		out, err = s.importTx(ctx, tx, id, true)
		if err == nil {
			out.Duplicates = dups
			out.Columns = detected
		}
		return err
	})
	return out, err
}

var docNumberRe = regexp.MustCompile(`(INV|PAY|RCV)[-/ ]?(\d{4})[-/ ]?(\d{3,6})`)

// autoMatchTx: saran pencocokan untuk mutasi kredit yang belum cocok.
func (s *Service) autoMatchTx(ctx context.Context, tx pgx.Tx, importID uuid.UUID) error {
	rows, err := tx.Query(ctx, `SELECT id, property_id, txn_date, COALESCE(description,'') || ' ' || COALESCE(reference,''), amount FROM bank_statement_lines WHERE import_id = $1 AND status IN ('unmatched','suggested') AND amount > 0`, importID)
	if err != nil {
		return err
	}
	type ln struct {
		id, pid uuid.UUID
		date    time.Time
		text    string
		amount  int64
	}
	var list []ln
	for rows.Next() {
		var l ln
		if rows.Scan(&l.id, &l.pid, &l.date, &l.text, &l.amount) == nil {
			l.text = strings.ToUpper(l.text)
			list = append(list, l)
		}
	}
	rows.Close()
	for _, l := range list {
		type cand struct {
			kind      string
			paymentID *uuid.UUID
			invoiceID *uuid.UUID
			score     int
			diff      int64
		}
		var best *cand
		consider := func(c cand) {
			if best == nil || c.score > best.score {
				cc := c
				best = &cc
			}
		}
		// nomor dokumen di berita transfer (INV-2026-000123, PAY-…)
		var numbers []string
		for _, m := range docNumberRe.FindAllStringSubmatch(l.text, -1) {
			seq, _ := strconv.Atoi(m[3])
			numbers = append(numbers, fmt.Sprintf("%s-%s-%06d", m[1], m[2], seq))
		}
		if len(numbers) > 0 {
			prow, err := tx.Query(ctx, `SELECT p.id, p.amount, p.invoice_id FROM payments p JOIN invoices i ON i.id = p.invoice_id WHERE p.property_id = $1 AND p.status IN ('initiated','pending') AND p.provider_code = 'manual'
				AND (p.payment_number = ANY($2) OR i.invoice_number = ANY($2))`, l.pid, numbers)
			if err == nil {
				for prow.Next() {
					var pid, iid uuid.UUID
					var amt int64
					if prow.Scan(&pid, &amt, &iid) == nil {
						sc := 85
						if amt == l.amount {
							sc = 100
						}
						consider(cand{kind: "pending_payment", paymentID: &pid, invoiceID: &iid, score: sc, diff: l.amount - amt})
					}
				}
				prow.Close()
			}
			irow, err := tx.Query(ctx, `SELECT id, total_amount - paid_amount - credited_amount FROM invoices WHERE property_id = $1 AND invoice_number = ANY($2) AND status IN ('issued','partially_paid','overdue')`, l.pid, numbers)
			if err == nil {
				for irow.Next() {
					var iid uuid.UUID
					var out int64
					if irow.Scan(&iid, &out) == nil {
						sc := 80
						if out == l.amount {
							sc = 95
						}
						consider(cand{kind: "invoice", invoiceID: &iid, score: sc, diff: l.amount - out})
					}
				}
				irow.Close()
			}
		}
		if best == nil || best.score < 90 {
			// pembayaran pending dengan jumlah sama di sekitar tanggal mutasi (unik)
			var pid, iid uuid.UUID
			var n int
			if err := tx.QueryRow(ctx, `SELECT count(*) OVER (), p.id, p.invoice_id FROM payments p WHERE p.property_id = $1 AND p.status IN ('initiated','pending') AND p.provider_code = 'manual' AND p.amount = $2
				AND p.created_at::date BETWEEN $3::date - 10 AND $3::date + 1 LIMIT 1`, l.pid, l.amount, l.date).Scan(&n, &pid, &iid); err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return err
			}
			if n == 1 {
				consider(cand{kind: "pending_payment", paymentID: &pid, invoiceID: &iid, score: 70})
			}
			// invoice terbuka dengan sisa tagihan sama persis (unik)
			n = 0
			if err := tx.QueryRow(ctx, `SELECT count(*) OVER (), id FROM invoices WHERE property_id = $1 AND status IN ('issued','partially_paid','overdue') AND total_amount - paid_amount - credited_amount = $2 LIMIT 1`, l.pid, l.amount).Scan(&n, &iid); err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return err
			}
			if n == 1 {
				consider(cand{kind: "invoice", invoiceID: &iid, score: 60})
			}
		}
		if best == nil || best.score < 50 {
			// nomor unit di berita transfer → invoice terbuka tertua pihak tersebut (alokasi FIFO saat dikonfirmasi)
			var iid *uuid.UUID
			var n int
			if err := tx.QueryRow(ctx, `WITH m AS (SELECT DISTINCT un.location_id FROM units un JOIN locations l ON l.id = un.location_id
					WHERE l.property_id = $1 AND length(un.unit_number) >= 3 AND position(upper(un.unit_number) IN $2) > 0)
				SELECT (SELECT count(*) FROM m)::int, (SELECT i.id FROM invoices i WHERE i.unit_location_id IN (SELECT location_id FROM m) AND i.status IN ('issued','partially_paid','overdue') ORDER BY i.due_at LIMIT 1)`,
				l.pid, l.text).Scan(&n, &iid); err != nil {
				return err
			}
			if n == 1 && iid != nil {
				consider(cand{kind: "invoice", invoiceID: iid, score: 50})
			}
		}
		if best == nil {
			continue
		}
		diff := best.diff
		if _, err := tx.Exec(ctx, `UPDATE bank_statement_lines SET status = 'suggested', match_type = $2, suggested_payment_id = $3, suggested_invoice_id = $4, match_score = $5, difference = $6 WHERE id = $1`,
			l.id, best.kind, best.paymentID, best.invoiceID, best.score, diff); err != nil {
			return err
		}
	}
	return nil
}

func refreshImportTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) error {
	_, err := tx.Exec(ctx, `UPDATE bank_statement_imports bi SET line_count = x.n, credit_count = x.cr, matched_count = x.m,
		status = CASE WHEN x.open = 0 THEN 'completed' ELSE 'open' END
		FROM (SELECT count(*) AS n, count(*) FILTER (WHERE amount > 0) AS cr, count(*) FILTER (WHERE status = 'matched') AS m,
		  count(*) FILTER (WHERE status IN ('unmatched','suggested')) AS open FROM bank_statement_lines WHERE import_id = $1) x WHERE bi.id = $1`, id)
	return err
}

const importSelect = `SELECT bi.id, bi.property_id, pl.name, bi.bank_account_id, ba.bank_name || ' ' || ba.account_number, bi.file_name, bi.format, to_char(bi.period_from,'YYYY-MM-DD'), to_char(bi.period_to,'YYYY-MM-DD'),
	bi.line_count, bi.credit_count, bi.matched_count,
	(SELECT count(*) FROM bank_statement_lines x WHERE x.import_id = bi.id AND x.status = 'suggested'), (SELECT count(*) FROM bank_statement_lines x WHERE x.import_id = bi.id AND x.status = 'unmatched'),
	(SELECT count(*) FROM bank_statement_lines x WHERE x.import_id = bi.id AND x.status = 'ignored'),
	COALESCE((SELECT sum(amount) FROM bank_statement_lines x WHERE x.import_id = bi.id AND x.amount > 0),0), COALESCE((SELECT sum(amount) FROM bank_statement_lines x WHERE x.import_id = bi.id AND x.status = 'matched'),0),
	bi.status, u.full_name, bi.imported_at
	FROM bank_statement_imports bi JOIN locations pl ON pl.id = bi.property_id LEFT JOIN bank_accounts ba ON ba.id = bi.bank_account_id LEFT JOIN users u ON u.id = bi.imported_by`

func scanImport(row pgx.Row) (*StatementImport, error) {
	var v StatementImport
	if err := row.Scan(&v.ID, &v.PropertyID, &v.PropertyName, &v.BankAccountID, &v.BankAccount, &v.FileName, &v.Format, &v.PeriodFrom, &v.PeriodTo, &v.LineCount, &v.CreditCount, &v.MatchedCount,
		&v.SuggestedCnt, &v.UnmatchedCnt, &v.IgnoredCount, &v.CreditTotal, &v.MatchedTotal, &v.Status, &v.ImportedBy, &v.ImportedAt); err != nil {
		return nil, err
	}
	return &v, nil
}

func (s *Service) importTx(ctx context.Context, tx pgx.Tx, id uuid.UUID, withLines bool) (*StatementImport, error) {
	v, err := scanImport(tx.QueryRow(ctx, importSelect+` WHERE bi.id = $1`, id))
	if err != nil {
		return nil, apperr.NotFound("Impor mutasi")
	}
	if !withLines {
		return v, nil
	}
	canManage := authctx.Must(ctx).HasOnProperty("billing.reconciliation.manage", v.PropertyID)
	rows, err := tx.Query(ctx, `SELECT bl.id, bl.line_no, to_char(bl.txn_date,'YYYY-MM-DD'), bl.description, bl.reference, bl.amount, bl.balance, bl.status, bl.match_type,
		bl.suggested_payment_id, sp.payment_number, bl.suggested_invoice_id, si.invoice_number, COALESCE(st.name, 'Unit ' || su.unit_number), bl.match_score,
		bl.payment_id, pp.payment_number, bl.invoice_id, pi.invoice_number, bl.difference, mu.full_name, bl.matched_at, bl.note
		FROM bank_statement_lines bl LEFT JOIN payments sp ON sp.id = bl.suggested_payment_id LEFT JOIN invoices si ON si.id = bl.suggested_invoice_id
		LEFT JOIN tenants st ON st.id = si.tenant_id LEFT JOIN units su ON su.location_id = si.unit_location_id
		LEFT JOIN payments pp ON pp.id = bl.payment_id LEFT JOIN invoices pi ON pi.id = bl.invoice_id LEFT JOIN users mu ON mu.id = bl.matched_by
		WHERE bl.import_id = $1 ORDER BY bl.txn_date, bl.line_no`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	v.Lines = []StmtLine{}
	for rows.Next() {
		var l StmtLine
		if err := rows.Scan(&l.ID, &l.LineNo, &l.TxnDate, &l.Description, &l.Reference, &l.Amount, &l.Balance, &l.Status, &l.MatchType, &l.SuggestedPayment, &l.SuggestedPayNum,
			&l.SuggestedInvoice, &l.SuggestedInvNum, &l.SuggestedParty, &l.MatchScore, &l.PaymentID, &l.PaymentNumber, &l.InvoiceID, &l.InvoiceNumber, &l.Difference, &l.MatchedByName, &l.MatchedAt, &l.Note); err != nil {
			return nil, err
		}
		l.AllowedActions = []string{"view"}
		if canManage && l.Amount > 0 {
			switch l.Status {
			case "suggested":
				l.AllowedActions = append(l.AllowedActions, "confirm", "match", "ignore")
			case "unmatched":
				l.AllowedActions = append(l.AllowedActions, "match", "ignore")
			case "ignored":
				l.AllowedActions = append(l.AllowedActions, "restore")
			}
		}
		v.Lines = append(v.Lines, l)
	}
	return v, rows.Err()
}

func (s *Service) GetImport(ctx context.Context, id uuid.UUID) (*StatementImport, error) {
	var out *StatementImport
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		v, err := s.importTx(ctx, tx, id, true)
		if err != nil {
			return err
		}
		if !authctx.Must(ctx).HasAnyOnProperty("billing.reconciliation.view", v.PropertyID) {
			return apperr.Forbidden("")
		}
		out = v
		return nil
	})
	return out, err
}

func (s *Service) ListImports(ctx context.Context, propertyID *uuid.UUID, page httpx.Page) ([]StatementImport, *string, error) {
	p := authctx.Must(ctx)
	var out []StatementImport
	var next *string
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		where, args := " WHERE true", []any{}
		if propertyID != nil {
			if !p.HasAnyOnProperty("billing.reconciliation.view", *propertyID) {
				return apperr.Forbidden("")
			}
			args = append(args, *propertyID)
			where += fmt.Sprintf(" AND bi.property_id = $%d", len(args))
		} else if pids, all := p.PropertyIDsFor("billing.reconciliation.view"); !all {
			args = append(args, pids)
			where += fmt.Sprintf(" AND bi.property_id = ANY($%d)", len(args))
		}
		if page.Cursor != nil {
			args = append(args, page.Cursor.Value, page.Cursor.ID)
			where += fmt.Sprintf(" AND (bi.imported_at, bi.id) < ($%d::timestamptz, $%d)", len(args)-1, len(args))
		}
		rows, err := tx.Query(ctx, importSelect+where+fmt.Sprintf(" ORDER BY bi.imported_at DESC, bi.id DESC LIMIT %d", page.Limit+1), args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			v, err := scanImport(rows)
			if err != nil {
				return err
			}
			out = append(out, *v)
		}
		if len(out) > page.Limit {
			last := out[page.Limit-1]
			c := httpx.EncodeCursor(last.ImportedAt.UTC().Format(time.RFC3339Nano), last.ID)
			next = &c
			out = out[:page.Limit]
		}
		return rows.Err()
	})
	if out == nil {
		out = []StatementImport{}
	}
	return out, next, err
}

type MatchInput struct {
	PaymentID *uuid.UUID `json:"payment_id"` // pembayaran pending tenant
	InvoiceID *uuid.UUID `json:"invoice_id"` // invoice terbuka (sisa → saldo kredit)
	TenantID  *uuid.UUID `json:"tenant_id"`  // alokasi FIFO ke tagihan tertua pihak
	UnitID    *uuid.UUID `json:"unit_location_id"`
	Note      string     `json:"note"`
}

// MatchLine: konfirmasi saran (input kosong) atau cocokkan manual → pembayaran dibuat/diverifikasi (P4-REC-03).
func (s *Service) MatchLine(ctx context.Context, lineID uuid.UUID, in MatchInput) (*StatementImport, error) {
	var out *StatementImport
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		importID, err := s.matchLineTx(ctx, tx, lineID, in)
		if err != nil {
			return err
		}
		out, err = s.importTx(ctx, tx, importID, true)
		return err
	})
	return out, err
}

func (s *Service) matchLineTx(ctx context.Context, tx pgx.Tx, lineID uuid.UUID, in MatchInput) (uuid.UUID, error) {
	p := authctx.Must(ctx)
	var importID, propertyID uuid.UUID
	var status string
	var amount int64
	var txnDate time.Time
	var ref, desc *string
	var sugPay, sugInv *uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT import_id, property_id, status, amount, txn_date, reference, description, suggested_payment_id, suggested_invoice_id FROM bank_statement_lines WHERE id = $1 FOR UPDATE`, lineID).
		Scan(&importID, &propertyID, &status, &amount, &txnDate, &ref, &desc, &sugPay, &sugInv); err != nil {
		return uuid.Nil, apperr.NotFound("Baris mutasi")
	}
	if !p.HasOnProperty("billing.reconciliation.manage", propertyID) {
		return uuid.Nil, apperr.Forbidden("Memerlukan billing.reconciliation.manage")
	}
	if status != "unmatched" && status != "suggested" {
		return uuid.Nil, apperr.InvalidTransition("Baris berstatus " + status + " tidak dapat dicocokkan")
	}
	if amount <= 0 {
		return uuid.Nil, apperr.Validation("Hanya mutasi kredit yang dapat dicocokkan")
	}
	if in.PaymentID == nil && in.InvoiceID == nil && in.TenantID == nil && in.UnitID == nil {
		if status != "suggested" {
			return uuid.Nil, apperr.Validation("Pilih pembayaran, invoice, atau tenant/unit").WithField("invoice_id", "wajib")
		}
		in.PaymentID, in.InvoiceID = sugPay, sugInv
		if in.PaymentID != nil {
			in.InvoiceID = nil
		}
	}
	loc := property.PropertyTimezone(ctx, tx, propertyID)
	paidAt := time.Date(txnDate.Year(), txnDate.Month(), txnDate.Day(), 12, 0, 0, 0, loc)
	reference := strings.TrimSpace(strPtrVal(ref) + " " + strPtrVal(desc))
	if len(reference) > 200 {
		reference = reference[:200]
	}
	var paymentID, invoiceID *uuid.UUID
	var diff int64
	matchType := "manual"
	// invoice dengan pembayaran tenant menunggu verifikasi → verifikasi pembayaran itu (hindari ganda)
	if in.PaymentID == nil && in.InvoiceID != nil {
		var pid uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT id FROM payments WHERE invoice_id = $1 AND status IN ('initiated','pending') AND provider_code = 'manual' ORDER BY created_at DESC LIMIT 1`, *in.InvoiceID).Scan(&pid); err == nil {
			in.PaymentID = &pid
		}
	}
	switch {
	case in.PaymentID != nil:
		var pPid, pInv uuid.UUID
		var pAmt int64
		var pStatus string
		if err := tx.QueryRow(ctx, `SELECT property_id, invoice_id, amount, status FROM payments WHERE id = $1`, *in.PaymentID).Scan(&pPid, &pInv, &pAmt, &pStatus); err != nil || pPid != propertyID {
			return uuid.Nil, apperr.Validation("payment_id tidak valid").WithField("payment_id", "tidak valid")
		}
		recv := amount
		if _, err := s.settleTx(ctx, tx, *in.PaymentID, paidAt, "staff_manual", &p.UserID, nil, &recv); err != nil {
			return uuid.Nil, err
		}
		_, _ = tx.Exec(ctx, `UPDATE payments SET bank_statement_line_id = $2, reference = COALESCE(reference, NULLIF($3,'')) WHERE id = $1`, *in.PaymentID, lineID, reference)
		paymentID, invoiceID = in.PaymentID, &pInv
		diff = amount - pAmt
		matchType = "pending_payment"
	default:
		ri := ReceiveInput{PropertyID: propertyID, Amount: amount, Method: "transfer", PaidAt: &paidAt, Reference: &reference}
		if in.Note != "" {
			ri.Notes = &in.Note
		}
		if in.InvoiceID != nil {
			var tid, uid *uuid.UUID
			var ipid uuid.UUID
			var out int64
			var st string
			if err := tx.QueryRow(ctx, `SELECT property_id, tenant_id, unit_location_id, total_amount - paid_amount - credited_amount, status FROM invoices WHERE id = $1`, *in.InvoiceID).Scan(&ipid, &tid, &uid, &out, &st); err != nil || ipid != propertyID {
				return uuid.Nil, apperr.Validation("invoice_id tidak valid").WithField("invoice_id", "tidak valid")
			}
			if out <= 0 || !(st == "issued" || st == "partially_paid" || st == "overdue") {
				return uuid.Nil, apperr.Conflict("INVOICE_NOT_PAYABLE", "Invoice tidak memiliki sisa tagihan")
			}
			ri.TenantID, ri.UnitID = tid, uid
			if tid != nil {
				ri.UnitID = nil
			}
			a := amount
			if a > out {
				a = out
			}
			ri.Allocations = []Allocation{{InvoiceID: *in.InvoiceID, Amount: a}}
			invoiceID = in.InvoiceID
			diff = amount - out
			matchType = "invoice"
			if in.InvoiceID != nil && sugInv != nil && *in.InvoiceID != *sugInv {
				matchType = "manual"
			}
		} else {
			ri.TenantID, ri.UnitID = in.TenantID, in.UnitID
		}
		res, err := s.receiveTx(ctx, tx, ri, &lineID)
		if err != nil {
			return uuid.Nil, err
		}
		if len(res.Payments) > 0 {
			id := res.Payments[0].ID
			paymentID = &id
			if invoiceID == nil {
				iid := res.Payments[0].InvoiceID
				invoiceID = &iid
			}
		}
		if in.InvoiceID == nil {
			diff = res.CreditAmount
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE bank_statement_lines SET status = 'matched', match_type = $2, payment_id = $3, invoice_id = $4, difference = $5, matched_by = $6, matched_at = now(), note = COALESCE(NULLIF($7,''), note) WHERE id = $1`,
		lineID, matchType, paymentID, invoiceID, diff, p.UserID, in.Note); err != nil {
		return uuid.Nil, err
	}
	if err := refreshImportTx(ctx, tx, importID); err != nil {
		return uuid.Nil, err
	}
	_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditStatusChange, EntityType: "bank_statement_line", EntityID: &lineID, EntityLabel: txnDate.Format("2006-01-02") + " " + rupiahText(amount),
		Before: map[string]any{"status": status}, After: map[string]any{"status": "matched", "match_type": matchType, "payment_id": paymentID, "invoice_id": invoiceID, "difference": diff}})
	return importID, nil
}

// ConfirmSuggestions: konfirmasi massal saran dengan skor ≥ minScore (default 90).
func (s *Service) ConfirmSuggestions(ctx context.Context, importID uuid.UUID, minScore int) (*StatementImport, int, error) {
	if minScore <= 0 {
		minScore = 90
	}
	var out *StatementImport
	n := 0
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		v, err := s.importTx(ctx, tx, importID, false)
		if err != nil {
			return err
		}
		if !authctx.Must(ctx).HasOnProperty("billing.reconciliation.manage", v.PropertyID) {
			return apperr.Forbidden("Memerlukan billing.reconciliation.manage")
		}
		rows, err := tx.Query(ctx, `SELECT id FROM bank_statement_lines WHERE import_id = $1 AND status = 'suggested' AND match_score >= $2 ORDER BY txn_date, line_no`, importID, minScore)
		if err != nil {
			return err
		}
		var lineIDs []uuid.UUID
		for rows.Next() {
			var id uuid.UUID
			if rows.Scan(&id) == nil {
				lineIDs = append(lineIDs, id)
			}
		}
		rows.Close()
		for _, id := range lineIDs {
			if _, err := s.matchLineTx(ctx, tx, id, MatchInput{}); err != nil {
				return err
			}
			n++
		}
		out, err = s.importTx(ctx, tx, importID, true)
		return err
	})
	return out, n, err
}

// IgnoreLine / RestoreLine: mutasi bukan pembayaran tenant (mis. bunga bank, transfer internal).
func (s *Service) SetLineIgnored(ctx context.Context, lineID uuid.UUID, ignore bool, note string) (*StatementImport, error) {
	p := authctx.Must(ctx)
	var out *StatementImport
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var importID, propertyID uuid.UUID
		var status string
		var amount int64
		if err := tx.QueryRow(ctx, `SELECT import_id, property_id, status, amount FROM bank_statement_lines WHERE id = $1 FOR UPDATE`, lineID).Scan(&importID, &propertyID, &status, &amount); err != nil {
			return apperr.NotFound("Baris mutasi")
		}
		if !p.HasOnProperty("billing.reconciliation.manage", propertyID) {
			return apperr.Forbidden("Memerlukan billing.reconciliation.manage")
		}
		if ignore {
			if status != "unmatched" && status != "suggested" {
				return apperr.InvalidTransition("Baris berstatus " + status + " tidak dapat diabaikan")
			}
			if strings.TrimSpace(note) == "" {
				return apperr.Validation("note wajib (alasan diabaikan)").WithField("note", "wajib")
			}
			if _, err := tx.Exec(ctx, `UPDATE bank_statement_lines SET status = 'ignored', note = $2, matched_by = $3, matched_at = now() WHERE id = $1`, lineID, strings.TrimSpace(note), p.UserID); err != nil {
				return err
			}
		} else {
			if status != "ignored" || amount <= 0 {
				return apperr.InvalidTransition("Hanya mutasi kredit yang diabaikan yang dapat dipulihkan")
			}
			if _, err := tx.Exec(ctx, `UPDATE bank_statement_lines SET status = 'unmatched', note = NULL, matched_by = NULL, matched_at = NULL, match_type = NULL, suggested_payment_id = NULL, suggested_invoice_id = NULL, match_score = NULL WHERE id = $1`, lineID); err != nil {
				return err
			}
			if err := s.autoMatchTx(ctx, tx, importID); err != nil {
				return err
			}
		}
		if err := refreshImportTx(ctx, tx, importID); err != nil {
			return err
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditStatusChange, EntityType: "bank_statement_line", EntityID: &lineID, EntityLabel: rupiahText(amount), Before: map[string]any{"status": status}, After: map[string]any{"ignored": ignore, "note": note}})
		var err error
		out, err = s.importTx(ctx, tx, importID, true)
		return err
	})
	return out, err
}
