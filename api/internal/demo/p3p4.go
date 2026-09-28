package demo

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/billing"
	"github.com/buildingvision/api/internal/finance"
	"github.com/buildingvision/api/internal/metering"
	"github.com/buildingvision/api/internal/parcels"
	"github.com/buildingvision/api/internal/security"
	"github.com/buildingvision/api/internal/tenantapp"
	"github.com/buildingvision/api/internal/tenantrelation"
	"github.com/buildingvision/api/internal/tenantservice"
)

// ---------- PRD P3 v2.1 (Tenant Experience) — data demo: paket, izin parkir tenant, feedback umum, pengumuman bertarget/
// alert, isu berulang. Dipanggil untuk profile office & apartment. ----------

func (s *Service) seedP3(ctx context.Context, env *Env, p *prop, tenants []tenantRef, logf func(string, ...any)) error {
	if len(tenants) == 0 {
		return nil
	}
	tr := s.asEmail(ctx, env, p.Staff["tr"])
	admin := p.admin
	var app []tenantRef
	for _, t := range tenants {
		if t.AppEmail != "" {
			app = append(app, t)
		}
	}
	// ---- Package: menunggu diambil, sudah diambil, dikembalikan (P3-PKG-01..04) ----
	pk := func(t tenantRef, courier, desc, storage string, age time.Duration) (*parcels.Package, error) {
		at := time.Now().Add(-age)
		return s.Parcels.Create(tr, parcels.Input{PropertyID: &p.ID, UnitLocationID: &t.UnitID, TenantID: &t.TenantID, Courier: ptr(courier), TrackingNumber: ptr(fmt.Sprintf("JP%d", at.Unix()%1000000000)),
			Description: ptr(desc), StorageLocation: ptr(storage), ReceivedAt: &at})
	}
	if len(app) > 0 {
		t0 := app[0]
		if _, err := pk(t0, "JNE", "Paket dokumen", "Rak A-3 lobby", 3*time.Hour); err != nil {
			return fmt.Errorf("package: %w", err)
		}
		picked, err := pk(t0, "SiCepat", "Kardus sedang", "Rak B-1 lobby", 26*time.Hour)
		if err != nil {
			return fmt.Errorf("package: %w", err)
		}
		if _, err := s.Parcels.Act(tr, picked.ID, "pickup", parcels.ActionInput{PickedUpByName: t0.Name}); err != nil {
			return fmt.Errorf("package pickup: %w", err)
		}
		old, err := pk(tenants[len(tenants)-1], "J&T", "Paket makanan kering", "Rak C-2 lobby", 5*24*time.Hour)
		if err != nil {
			return fmt.Errorf("package: %w", err)
		}
		_ = old
	}
	// ---- izin parkir tenant: kendaraan terdaftar → permohonan → disetujui / menunggu (P3-PRK-01..02) ----
	if len(app) > 0 {
		for i, t := range app {
			if i > 1 {
				break
			}
			tctx := s.asEmail(ctx, env, t.AppEmail)
			plate := fmt.Sprintf("B %d %s", 1200+i*37, map[int]string{0: "KLM", 1: "TRX"}[i])
			v, err := s.Sec.RegisterTenantVehicle(tctx, security.TenantVehicleInput{PlateNumber: ptr(plate), VehicleType: ptr("car"), Brand: ptr("Toyota"), Color: ptr("Hitam"), UnitID: &t.UnitID})
			if err != nil {
				s.Log.Warn("demo tenant vehicle", "err", err)
				continue
			}
			pp, err := s.Sec.RequestParkingPermit(tctx, security.TenantPermitInput{VehicleID: v.ID, PermitType: "monthly", Notes: ptr("Parkir harian kantor")})
			if err != nil {
				s.Log.Warn("demo parking permit", "err", err)
				continue
			}
			if i == 0 {
				until := today().AddDate(0, 3, 0).Format("2006-01-02")
				if _, err := s.Sec.ActParkingPermit(admin, pp.ID, "approve", security.PermitDecisionInput{ValidFrom: ptr(today().Format("2006-01-02")), ValidUntil: &until, StickerNumber: ptr("STK-0042"), FeeAmount: ptr(int64(350000))}); err != nil {
					return fmt.Errorf("approve permit: %w", err)
				}
			}
		}
		s.flush(ctx)
	}
	// ---- feedback umum (P3-FDB-01..03): saran dijawab, keluhan anonim baru ----
	if len(app) > 0 {
		tctx := s.asEmail(ctx, env, app[0].AppEmail)
		fb, err := s.TenantApp.CreateGeneralFeedback(tctx, tenantapp.GeneralFeedbackInput{Category: "suggestion", Subject: "Tambah rak sepeda di basement", Body: "Banyak karyawan bersepeda; mohon disediakan rak sepeda yang aman."})
		if err != nil {
			return fmt.Errorf("feedback: %w", err)
		}
		s.flush(ctx)
		if _, err := s.TR.ActGeneralFeedback(tr, fb.ID, "respond", tenantrelation.FeedbackActionInput{Response: "Terima kasih. Rak sepeda 20 slot dipasang di basement B1 bulan depan."}); err != nil {
			return fmt.Errorf("feedback respond: %w", err)
		}
		if len(app) > 1 {
			if _, err := s.TenantApp.CreateGeneralFeedback(s.asEmail(ctx, env, app[1].AppEmail), tenantapp.GeneralFeedbackInput{Category: "complaint", Subject: "Musik di lobby terlalu keras", Body: "Volume musik lobby mengganggu tamu yang menunggu.", IsAnonymous: true}); err != nil {
				return fmt.Errorf("feedback anonim: %w", err)
			}
		}
		s.flush(ctx)
	}
	// ---- pengumuman bertarget + wajib dibaca, dan alert darurat (P3-ANN-01..05) ----
	news, err := s.TR.CreateAnnouncement(tr, tenantrelation.AnnouncementInput{PropertyID: &p.ID, Title: ptr("Berita: Program daur ulang sampah elektronik"), Excerpt: ptr("Drop box tersedia di lobby"),
		Body: ptr("Mulai minggu ini tersedia drop box e-waste di lobby utama. Baterai & perangkat kecil dapat dibuang di sana."), Category: ptr("news"), Importance: ptr("normal"),
		TargetTenantIDs: &[]uuid.UUID{tenants[0].TenantID}, RequiresAck: ptr(true)})
	if err != nil {
		return fmt.Errorf("announcement news: %w", err)
	}
	if _, err := s.TR.TransitionAnnouncement(tr, news.ID, "publish"); err != nil {
		return fmt.Errorf("publish news: %w", err)
	}
	exp := time.Now().Add(6 * time.Hour)
	if _, err := s.TR.Broadcast(tr, tenantrelation.BroadcastInput{PropertyID: p.ID, Title: "Peringatan: Uji coba alarm kebakaran", Body: "Uji coba alarm pukul 14:00–14:15. Tidak perlu evakuasi.", Severity: "warning", ExpiresAt: &exp}); err != nil {
		return fmt.Errorf("broadcast: %w", err)
	}
	s.flush(ctx)
	// ---- isu berulang: 3 keluhan kategori & lokasi sama dalam 30 hari → recurring issue (P3-CMP-02..03) ----
	loc := tenants[0].UnitID
	for i, title := range []string{"Air keran berbau kaporit", "Air keran keruh lagi", "Air keran masih keruh"} {
		if _, err := s.TS.Create(admin, tenantservice.CreateInput{PropertyID: &p.ID, CategoryCode: "complaint", RequestType: "complaint", Title: title, TenantID: &tenants[0].TenantID, LocationID: &loc,
			Priority: "medium", Channel: []string{"phone", "walk_in", "phone"}[i]}); err != nil {
			return fmt.Errorf("SR berulang: %w", err)
		}
	}
	s.flush(ctx)
	logf("%s: P3 — paket 3, izin parkir 2, feedback 2, pengumuman bertarget + alert, isu berulang", p.Prefix)
	return nil
}

// ---------- PRD P4 v2.1 (Financial Operations) — data demo: PPN & rekening, billing rule + run (draft menunggu terbit), meter &
// pembacaan (satu flagged), denda, janji bayar, credit note menunggu persetujuan, impor mutasi dengan saran, budget & biaya,
// sinking fund (apartment), deposit. ----------

func (s *Service) seedP4(ctx context.Context, env *Env, p *prop, tenants []tenantRef, logf func(string, ...any)) error {
	if len(tenants) == 0 {
		return nil
	}
	fin := s.asEmail(ctx, env, "finance.demo@buildingvision.local")
	apartment := p.Profile == ProfileApartment
	taxOn := !apartment
	if _, err := s.Billing.PutSettings(fin, &p.ID, billing.SettingsInput{TaxEnabled: &taxOn, TaxRate: ptr(11.0), DefaultDueDays: ptr(14), ReminderOffsets: ptr([]int32{-3, 1, 7, 14, 30}),
		SellerName: ptr("PT " + p.Name + " Pengelola"), PaymentInstructions: ptr("Transfer ke rekening virtual/bank pengelola; cantumkan nomor invoice pada berita transfer.")}); err != nil {
		return fmt.Errorf("billing settings: %w", err)
	}
	if _, err := s.Billing.SaveBankAccount(fin, nil, billing.BankAccountInput{PropertyID: &p.ID, BankName: ptr("BCA"), AccountNumber: ptr(map[string]string{ProfileOffice: "8801234567", ProfileApartment: "8802345678", ProfileHotel: "8803456789"}[p.Profile]), AccountName: ptr("PT " + p.Name + " Pengelola"), IsDefault: ptr(true)}); err != nil {
		return fmt.Errorf("bank account: %w", err)
	}
	// ---- billing rule: per m² (office service charge / apartment IPL) + sinking fund 10% (apartment) + listrik meter ----
	chargeType, code, name, rate := "service_charge", "SC", "Service Charge", 85000.0
	if apartment {
		chargeType, code, name, rate = "ipl", "IPL", "Iuran Pengelolaan Lingkungan", 15000
	}
	base, err := s.Billing.SaveRule(fin, nil, billing.RuleInput{PropertyID: &p.ID, Code: ptr(code), Name: ptr(name), ChargeType: ptr(chargeType), Basis: ptr("per_area_m2"), Rate: &rate, Prorate: ptr(true), AutoGenerate: ptr(true)})
	if err != nil {
		return fmt.Errorf("rule %s: %w", code, err)
	}
	ruleIDs := []uuid.UUID{base.ID}
	if apartment {
		zero := 0.0
		sf, err := s.Billing.SaveRule(fin, nil, billing.RuleInput{PropertyID: &p.ID, Code: ptr("SF"), Name: ptr("Sinking Fund"), ChargeType: ptr("sinking_fund"), Basis: ptr("percentage"), Rate: ptr(10.0), BaseRuleID: &base.ID, TaxRate: &zero, AutoGenerate: ptr(true)})
		if err != nil {
			return fmt.Errorf("rule SF: %w", err)
		}
		ruleIDs = append(ruleIDs, sf.ID)
		// meter listrik 3 unit + tarif; satu pembacaan melonjak (flagged) menunggu review
		tariff, err := s.Metering.SaveTariff(fin, nil, metering.TariffInput{PropertyID: &p.ID, Code: ptr("PLN-R1"), Name: ptr("Listrik R-1 2.200 VA"), MeterType: ptr("electricity"), Rate: ptr(1444.7), FixedCharge: ptr(int64(40000))})
		if err != nil {
			return fmt.Errorf("tariff: %w", err)
		}
		tech := s.asEmail(ctx, env, p.Staff["tech"])
		for i, t := range tenants {
			if i >= 3 {
				break
			}
			m, err := s.Metering.SaveMeter(fin, nil, metering.MeterInput{LocationID: &t.UnitID, MeterType: ptr("electricity"), MeterNumber: ptr(fmt.Sprintf("PLN-%s", t.UnitLabel)), InitialReading: ptr(float64(12000 + i*800)), TariffID: &tariff.ID})
			if err != nil {
				return fmt.Errorf("meter: %w", err)
			}
			prev := time.Now().AddDate(0, -1, 0)
			if _, err := s.Metering.Record(tech, m.ID, metering.ReadingInput{ReadingValue: float64(12000+i*800) + 180, ReadAt: &prev, Source: "staff_app"}); err != nil {
				return fmt.Errorf("reading: %w", err)
			}
			usage := 210.0
			if i == 2 {
				usage = 1450 // lonjakan → flagged
			}
			now := time.Now().Add(-2 * time.Hour)
			if _, err := s.Metering.Record(tech, m.ID, metering.ReadingInput{ReadingValue: float64(12000+i*800) + 180 + usage, ReadAt: &now, Source: "staff_app"}); err != nil {
				return fmt.Errorf("reading: %w", err)
			}
		}
		el, err := s.Billing.SaveRule(fin, nil, billing.RuleInput{PropertyID: &p.ID, Code: ptr("EL"), Name: ptr("Listrik"), ChargeType: ptr("electricity"), Basis: ptr("meter_usage"), MeterType: ptr("electricity"), TariffID: &tariff.ID})
		if err != nil {
			return fmt.Errorf("rule EL: %w", err)
		}
		// run listrik periode berjalan: diterbitkan (pembacaan flagged menjadi pengecualian)
		elRun, err := s.Billing.CreateRun(fin, billing.RunInput{PropertyID: p.ID, Period: time.Now().Format("2006-01"), RuleIDs: []uuid.UUID{el.ID}, InvoiceType: "electricity"})
		if err != nil {
			return fmt.Errorf("run listrik: %w", err)
		}
		if _, err := s.Billing.GenerateRun(fin, elRun.ID); err == nil {
			if _, err := s.Billing.IssueRun(fin, elRun.ID); err != nil {
				return fmt.Errorf("issue run listrik: %w", err)
			}
		} else {
			s.Log.Warn("demo generate run listrik", "err", err)
		}
	}
	// run periode berikutnya: draft invoice menunggu diterbitkan Finance (Attention dashboard)
	next := time.Date(time.Now().Year(), time.Now().Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, 1, 0)
	run, err := s.Billing.CreateRun(fin, billing.RunInput{PropertyID: p.ID, Period: next.Format("2006-01"), RuleIDs: ruleIDs})
	if err != nil {
		return fmt.Errorf("run: %w", err)
	}
	if _, err := s.Billing.GenerateRun(fin, run.ID); err != nil {
		s.Log.Warn("demo generate run", "err", err)
	}
	s.flush(ctx)
	// ---- denda + sweep (akrual pada invoice overdue) ----
	if _, err := s.Billing.SavePenaltyRule(fin, nil, billing.PenaltyRuleInput{PropertyID: &p.ID, Name: ptr("Denda keterlambatan 2%/bulan"), Method: ptr("percent"), Rate: ptr(2.0), Period: ptr("per_month"), GraceDays: ptr(3), MaxPct: ptr(10.0)}); err != nil {
		return fmt.Errorf("penalty rule: %w", err)
	}
	if err := s.Billing.Sweep(ctx, env.OrgID); err != nil {
		s.Log.Warn("demo billing sweep", "err", err)
	}
	s.flush(ctx)
	// ---- janji bayar untuk tunggakan (P4-COL-03) ----
	var overdueID uuid.UUID
	var ovTenant *uuid.UUID
	_ = s.scanOne(ctx, env.OrgID, `SELECT id, tenant_id FROM invoices WHERE property_id = $1 AND status = 'overdue' ORDER BY due_at LIMIT 1`, []any{p.ID}, &overdueID, &ovTenant)
	if overdueID != uuid.Nil && ovTenant != nil {
		promise := today().AddDate(0, 0, 5).Format("2006-01-02")
		if _, err := s.Billing.CreateCollectionLog(fin, billing.CollectionLogInput{PropertyID: p.ID, TenantID: ovTenant, InvoiceIDs: []uuid.UUID{overdueID}, Channel: "whatsapp", Outcome: "promise_to_pay",
			ContactPerson: ptr("Bagian keuangan tenant"), Notes: ptr("Menunggu pencairan dana; janji transfer minggu ini."), PromiseDate: &promise}); err != nil {
			return fmt.Errorf("collection log: %w", err)
		}
	}
	// ---- credit note menunggu persetujuan (invoice lunas) ----
	var paidID uuid.UUID
	_ = s.scanOne(ctx, env.OrgID, `SELECT id FROM invoices WHERE property_id = $1 AND status = 'paid' AND invoice_number IS NOT NULL ORDER BY paid_at DESC LIMIT 1`, []any{p.ID}, &paidID)
	if paidID != uuid.Nil {
		if _, err := s.Billing.RequestCreditNote(fin, paidID, billing.CreditNoteInput{Amount: 150000, Reason: "Koreksi tarif — luas unit terverifikasi ulang"}); err != nil {
			return fmt.Errorf("credit note: %w", err)
		}
	}
	// ---- impor mutasi bank: satu mutasi cocok dengan pembayaran tenant yang menunggu verifikasi ----
	var payNum string
	var payAmt int64
	_ = s.scanOne(ctx, env.OrgID, `SELECT p.payment_number, p.amount FROM payments p WHERE p.property_id = $1 AND p.status = 'pending' AND p.provider_code = 'manual' ORDER BY p.created_at LIMIT 1`, []any{p.ID}, &payNum, &payAmt)
	if payNum != "" {
		d := today().Format("02/01/2006")
		csv := "Tanggal,Keterangan,Debet,Kredit,Saldo\n" +
			d + ",TRSF E-BANKING CR " + payNum + ",," + fmt.Sprint(payAmt) + ",\n" +
			d + ",BUNGA REKENING,,87500,\n" +
			d + ",BIAYA ADM BANK,15000,,\n"
		if _, err := s.Billing.ImportStatement(fin, billing.ImportInput{PropertyID: p.ID, FileName: "mutasi-" + today().Format("20060102") + ".csv", Content: csv}); err != nil {
			return fmt.Errorf("impor mutasi: %w", err)
		}
	}
	// ---- sinking fund & deposit ----
	if apartment {
		if _, err := s.Billing.AddSinkingFundEntry(fin, billing.FundEntryInput{PropertyID: p.ID, EntryType: "opening", Amount: 850_000_000, Description: "Saldo awal sinking fund (migrasi)", EntryDate: ptr(today().AddDate(0, -3, 0).Format("2006-01-02"))}); err != nil {
			return fmt.Errorf("sinking fund opening: %w", err)
		}
		if _, err := s.Billing.AddSinkingFundEntry(fin, billing.FundEntryInput{PropertyID: p.ID, EntryType: "usage", Amount: 125_000_000, Description: "Pengecatan ulang fasad Tower A", Reference: ptr("SPK-2026-014")}); err != nil {
			return fmt.Errorf("sinking fund usage: %w", err)
		}
	}
	if _, err := s.Billing.AddDepositEntry(fin, billing.DepositEntryInput{PropertyID: p.ID, TenantID: &tenants[0].TenantID, UnitID: &tenants[0].UnitID, EntryType: "received", Amount: 5_000_000, Reason: "Deposit fit-out & kerusakan"}); err != nil {
		return fmt.Errorf("deposit: %w", err)
	}
	// ---- budget tahun berjalan (approved) + biaya manual ----
	year := time.Now().Year()
	revCat := chargeType
	monthly := func(v int64) [12]int64 {
		var m [12]int64
		for i := range m {
			m[i] = v
		}
		return m
	}
	revBudget := int64(rate * 2400)
	lines := []finance.BudgetLineInput{{Kind: "revenue", Category: revCat, Months: monthly(revBudget)}, {Kind: "cost", Category: "maintenance", Months: monthly(revBudget / 5)},
		{Kind: "cost", Category: "staff", Months: monthly(revBudget / 4)}, {Kind: "cost", Category: "utility", Months: monthly(revBudget / 6)}, {Kind: "cost", Category: "cleaning", Months: monthly(revBudget / 12)}}
	bg, err := s.Finance.CreateBudget(fin, finance.BudgetInput{PropertyID: &p.ID, FiscalYear: year, Name: ptr(fmt.Sprintf("Budget operasional %d", year)), Lines: &lines})
	if err != nil {
		return fmt.Errorf("budget: %w", err)
	}
	if _, err := s.Finance.ActBudget(fin, bg.ID, "approve"); err != nil {
		return fmt.Errorf("approve budget: %w", err)
	}
	for _, c := range []struct {
		cat, desc, payee string
		amount           int64
		daysAgo          int
	}{
		{"staff", "Tagihan outsourcing security bulan ini", "PT Garda Aman", revBudget / 4, 3},
		{"utility", "Tagihan PLN area bersama", "PLN", revBudget / 6, 8},
		{"cleaning", "Chemical & alat kebersihan", "CV Bersih Jaya", revBudget / 15, 12},
		{"admin", "Biaya administrasi bank & materai", "Bank BCA", 450000, 5},
	} {
		if _, err := s.Finance.SaveCost(fin, nil, finance.CostInput{PropertyID: &p.ID, Category: &c.cat, EntryDate: ptr(today().AddDate(0, 0, -c.daysAgo).Format("2006-01-02")), Amount: &c.amount, Payee: &c.payee, Description: &c.desc}); err != nil {
			return fmt.Errorf("cost entry: %w", err)
		}
	}
	s.flush(ctx)
	logf("%s: P4 — PPN/rekening, billing rule %d, run draft, meter & pembacaan, denda, janji bayar, credit note, impor mutasi, budget, biaya", p.Prefix, len(ruleIDs))
	return nil
}

var _ = pgx.ErrNoRows
