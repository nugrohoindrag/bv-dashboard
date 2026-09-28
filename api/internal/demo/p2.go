package demo

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/asset"
	"github.com/buildingvision/api/internal/engineering"
	"github.com/buildingvision/api/internal/housekeeping"
	"github.com/buildingvision/api/internal/inventory"
	"github.com/buildingvision/api/internal/operations"
	"github.com/buildingvision/api/internal/property"
	"github.com/buildingvision/api/internal/security"
	"github.com/buildingvision/api/internal/workforce"
)

// seedP2: data Workforce Operations (PRD P2 v2.1) per property — kontak darurat, shift per domain + roster + on-duty,
// serah terima, dokumen equipment & warranty, health asset, inspeksi engineering terjadwal, cleaning route
// (run hari ini berprogres), consumable per cleaning task, riwayat emergency, parkir, lost & found. Relatif terhadap waktu seed.
func (s *Service) seedP2(ctx context.Context, env *Env, p *prop, sp propSpec, logf func(string, ...any)) error {
	admin := p.admin
	secSpv := s.asEmail(ctx, env, "security.demo@buildingvision.local")
	hkSpv := s.asEmail(ctx, env, "housekeeping.demo@buildingvision.local")
	engSpv := s.asEmail(ctx, env, "engineering.demo@buildingvision.local")
	secID, sec2ID := env.Users[p.Staff["sec"]], env.Users[p.Staff["sec2"]]
	hkID, hk2ID := env.Users[p.Staff["hk"]], env.Users[p.Staff["hk2"]]
	secCtx, hkCtx := s.as(ctx, env, secID), s.as(ctx, env, hkID)
	var loc *time.Location
	_ = s.DB.WithOrgTx(ctx, env.OrgID, func(ctx context.Context, tx pgx.Tx) error {
		loc = property.PropertyTimezone(ctx, tx, p.ID)
		return nil
	})
	if loc == nil {
		loc, _ = time.LoadLocation("Asia/Jakarta")
	}
	now := time.Now().In(loc)
	day := func(offset int) string { return now.AddDate(0, 0, offset).Format("2006-01-02") }

	// ---- kontak darurat (P2-EMG-07) ----
	for i, c := range []struct{ name, typ, phone string }{
		{"Pemadam Kebakaran", "fire", "113"}, {"Ambulans / Gawat Darurat", "ambulance", "119"}, {"Polisi", "police", "110"},
		{"PLN — gangguan listrik", "electricity", "123"}, {"Pos Keamanan " + sp.Name, "internal", "+62 21 5550 0100"},
	} {
		if _, err := s.Sec.SaveEmergencyContact(admin, nil, security.EmergencyContactInput{PropertyID: &p.ID, Name: ptr(c.name), ContactType: ptr(c.typ), Phone: ptr(c.phone), SortOrder: ptr(i + 1)}); err != nil {
			return fmt.Errorf("emergency contact: %w", err)
		}
	}

	// ---- shift per domain (D-P2-05) + roster 7 hari + on-duty ----
	mkShift := func(domain, code, name, start, end string, min int) (uuid.UUID, error) {
		sd, err := s.Workforce.SaveShift(admin, domain, nil, workforce.ShiftInput{PropertyID: &p.ID, Code: ptr(code), Name: ptr(name), StartTime: ptr(start), EndTime: ptr(end), MinStaff: ptr(min), BreakMinutes: ptr(60)})
		if err != nil {
			return uuid.Nil, fmt.Errorf("shift %s %s: %w", domain, code, err)
		}
		return sd.ID, nil
	}
	secPG, err := mkShift("security", "PG", "Pagi", "07:00", "15:00", 2)
	if err != nil {
		return err
	}
	secSG, err := mkShift("security", "SG", "Siang", "15:00", "23:00", 2)
	if err != nil {
		return err
	}
	secML, err := mkShift("security", "ML", "Malam", "23:00", "07:00", 1)
	if err != nil {
		return err
	}
	hkPG, err := mkShift("housekeeping", "PG", "Pagi", "06:00", "14:00", 2)
	if err != nil {
		return err
	}
	hkSG, err := mkShift("housekeeping", "SG", "Siang", "14:00", "22:00", 1)
	if err != nil {
		return err
	}
	// shift berjalan saat seed: officer 1 on-duty (clock-in), officer 2 shift berikutnya
	h := now.Hour()
	curSec, nextSec, rosterStart := secPG, secSG, 0
	switch {
	case h >= 15 && h < 23:
		curSec, nextSec = secSG, secML
	case h >= 23:
		curSec, nextSec = secML, secPG
	case h < 7:
		curSec, nextSec, rosterStart = secML, secPG, -1 // shift malam dimulai kemarin
	}
	dates := func(from int) []string {
		out := []string{}
		for d := from; d < 7; d++ {
			out = append(out, day(d))
		}
		return out
	}
	secTeam, hkTeam := p.Teams["security"], p.Teams["housekeeping"]
	if _, err := s.Workforce.AssignRoster(admin, "security", workforce.RosterAssignInput{ShiftID: curSec, Dates: dates(rosterStart), UserIDs: []uuid.UUID{secID}, TeamID: &secTeam, Post: ptr("Pos Lobby")}); err != nil {
		return fmt.Errorf("roster security: %w", err)
	}
	if _, err := s.Workforce.AssignRoster(admin, "security", workforce.RosterAssignInput{ShiftID: nextSec, Dates: dates(0), UserIDs: []uuid.UUID{sec2ID}, TeamID: &secTeam, Post: ptr("Pos Parkir")}); err != nil {
		return fmt.Errorf("roster security 2: %w", err)
	}
	if _, err := s.Workforce.ClockIn(secCtx, workforce.ClockInput{PropertyID: &p.ID, Domain: "security", GPSStatus: "unavailable", Note: ptr("Mulai jaga")}); err != nil {
		s.Log.Warn("demo clock-in security", "err", err)
	}
	curHK, nextHK, hkOnDuty := hkPG, hkSG, false
	switch {
	case h >= 6 && h < 14:
		hkOnDuty = true
	case h >= 14 && h < 22:
		curHK, nextHK, hkOnDuty = hkSG, hkPG, true
	}
	if _, err := s.Workforce.AssignRoster(admin, "housekeeping", workforce.RosterAssignInput{ShiftID: curHK, Dates: dates(0), UserIDs: []uuid.UUID{hkID}, TeamID: &hkTeam}); err != nil {
		return fmt.Errorf("roster housekeeping: %w", err)
	}
	if _, err := s.Workforce.AssignRoster(admin, "housekeeping", workforce.RosterAssignInput{ShiftID: nextHK, Dates: dates(0), UserIDs: []uuid.UUID{hk2ID}, TeamID: &hkTeam}); err != nil {
		return fmt.Errorf("roster housekeeping 2: %w", err)
	}
	if hkOnDuty {
		if _, err := s.Workforce.ClockIn(hkCtx, workforce.ClockInput{PropertyID: &p.ID, Domain: "housekeeping", GPSStatus: "unavailable"}); err != nil {
			s.Log.Warn("demo clock-in housekeeping", "err", err)
		}
	}
	// serah terima dari shift sebelumnya (menunggu diterima officer on-duty)
	if _, err := s.Workforce.CreateHandover(s.as(ctx, env, sec2ID), "security", workforce.HandoverInput{PropertyID: p.ID, ReceivedBy: &secID, Post: ptr("Pos Lobby"),
		Notes: "Serah terima: CCTV basement B1 kamera 3 buram (sudah dilaporkan ke engineering); kunci ruang panel dititipkan di pos."}); err != nil {
		s.Log.Warn("demo handover", "err", err)
	}

	// ---- dokumen equipment & warranty (P2-DOC-01..02) ----
	for _, d := range []struct {
		key, typ, title, number, issuer string
		issued                          int
		expires                         *int
	}{
		{"ac", "manual", "Manual Operasi & Perawatan AHU", "", "Daikin", -900, nil},
		{"genset", "permit", "SLO Genset 500 kVA", "SLO-GEN-2023-0098", "Kementerian ESDM", -1075, ptr(18)},
		{"lift", "certificate", "Izin Operasi Lift (Riksa Uji)", "RU-LIFT-2025-0311", "Disnaker DKI", -370, ptr(-5)},
		{"fire", "inspection_report", "Laporan Uji Fungsi Fire Alarm", "UF-FA-2026-07", "Vendor Proteksi", -30, nil},
	} {
		aid, ok := p.Assets[d.key]
		if !ok {
			continue
		}
		in := asset.AssetDocumentInput{DocumentType: ptr(d.typ), Title: ptr(d.title), Issuer: ptr(d.issuer), IssuedOn: ptr(day(d.issued))}
		if d.number != "" {
			in.DocumentNumber = ptr(d.number)
		}
		if d.expires != nil {
			in.ExpiresOn = ptr(day(*d.expires))
		}
		if _, err := s.Asset.CreateDocument(engSpv, aid, in); err != nil {
			return fmt.Errorf("asset document %s: %w", d.title, err)
		}
	}
	if aid, ok := p.Assets["cctv"]; ok {
		_ = s.DB.WithOrgTx(ctx, env.OrgID, func(ctx context.Context, tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `UPDATE assets SET warranty_until = $2 WHERE id = $1`, aid, now.AddDate(0, 0, 25))
			return err
		})
	}

	// ---- inspeksi engineering terjadwal (P2-INS-02) ----
	if aid, ok := p.Assets["fire"]; ok {
		engTeam := p.Teams["engineering"]
		pl, err := s.Eng.CreatePlan(admin, engineering.PlanInput{Name: ptr("Inspeksi Bulanan Fire Alarm"), AssetID: &aid, Frequency: ptr("monthly"), StartDate: ptr(day(0)), DefaultPriority: ptr("high"),
			ResponsibleTeamID: &engTeam, LeadTimeDays: ptr(0), DurationMinutes: ptr(60), OutputType: ptr("inspection")})
		if err != nil {
			return fmt.Errorf("inspection plan: %w", err)
		}
		if _, err := s.Eng.SetPlanStatus(admin, pl.ID, "published"); err != nil {
			return err
		}
		if _, err := s.Eng.CreateDueWorkOrders(admin, env.OrgID); err != nil {
			return fmt.Errorf("scheduled inspection: %w", err)
		}
	}

	// ---- cleaning route: run hari ini berprogres (P2-RTE-01..03) ----
	start := now.Add(-30 * time.Minute).Truncate(5 * time.Minute)
	if start.Day() != now.Day() {
		start = time.Date(now.Year(), now.Month(), now.Day(), 0, 5, 0, 0, loc)
	}
	route, err := s.HK.CreateRoute(hkSpv, housekeeping.RouteInput{PropertyID: &p.ID, Name: ptr("Route Lobby – Toilet – Koridor"), CleaningType: ptr("routine"), ShiftID: &hkPG, StartTime: ptr(start.Format("15:04")),
		ResponsibleTeamID: &hkTeam, DefaultAssigneeUserID: &hkID, RequiresPhoto: ptr(false), Stops: &[]housekeeping.RouteStopInput{
			{LocationID: ptr(p.Areas["lobby"]), EstimatedMinutes: ptr(20), Notes: ptr("Lantai, kaca pintu, meja resepsionis")},
			{LocationID: ptr(p.Areas["toilet"]), EstimatedMinutes: ptr(25), Notes: ptr("Cek sabun & tisu")},
			{LocationID: ptr(p.Areas["corridor"]), EstimatedMinutes: ptr(20)},
		}})
	if err != nil {
		return fmt.Errorf("cleaning route: %w", err)
	}
	if route.TodayRun == nil {
		if runs, err := s.HK.ListRuns(hkSpv, housekeeping.RunFilter{RouteID: &route.ID}); err == nil && len(runs) > 0 {
			route.TodayRun = &runs[0]
		}
	}
	if route.TodayRun != nil && len(route.TodayRun.Stops) >= 2 {
		first := route.TodayRun.Stops[0].TaskID
		if _, err := s.Ops.Transition(hkCtx, operations.ObjTask, first, "start", operations.TransitionInput{}); err == nil {
			// consumable per cleaning task (P2-CNS-02)
			if itemID, ok := env.Items["chemical"]; ok {
				if _, err := s.Inventory.AddConsumable(hkCtx, first, inventory.ConsumableUsageInput{ItemID: itemID, Quantity: 1.5, Note: ptr("Pel lantai lobby")}); err != nil {
					s.Log.Warn("demo consumable", "err", err)
				}
			}
			if _, err := s.Ops.Transition(hkCtx, operations.ObjTask, first, "complete", operations.TransitionInput{CompletionNotes: ptr("Lobby bersih"), GPSStatus: "unavailable"}); err != nil {
				s.Log.Warn("demo route stop complete", "err", err)
			}
		}
		_, _ = s.Ops.Transition(hkCtx, operations.ObjTask, route.TodayRun.Stops[1].TaskID, "start", operations.TransitionInput{})
	}

	// ---- riwayat emergency (P2-EMG-02..06): medis selesai + alarm palsu ----
	mkEmergency := func(typ, area, note, final string, age time.Duration) error {
		a, err := s.Sec.RaiseEmergency(secCtx, security.RaiseEmergencyInput{PropertyID: &p.ID, EmergencyType: typ, LocationID: ptr(p.Areas[area]), Description: ptr(note), Channel: "mobile", GPSStatus: "unavailable"})
		if err != nil {
			return fmt.Errorf("emergency: %w", err)
		}
		if final == "cancel" {
			if _, err := s.Sec.ActEmergency(secSpv, a.ID, "cancel", security.EmergencyActionInput{Reason: "Alarm palsu: detektor asap terpicu uap dari pantry"}); err != nil {
				return err
			}
		} else {
			for _, act := range []string{"acknowledge", "respond"} {
				if _, err := s.Sec.ActEmergency(secSpv, a.ID, act, security.EmergencyActionInput{}); err != nil {
					return err
				}
			}
			if _, err := s.Sec.ActEmergency(secSpv, a.ID, "resolve", security.EmergencyActionInput{Resolution: "Korban ditangani P3K, dijemput ambulans; keluarga dihubungi"}); err != nil {
				return err
			}
		}
		return s.backdate(ctx, env, "emergency_alerts", a.ID, now.Add(-age), map[string]time.Time{"raised_at": now.Add(-age)})
	}
	if err := mkEmergency("medical", "lobby", "Pengunjung pingsan di lobby", "resolve", 50*time.Hour); err != nil {
		return err
	}
	if err := mkEmergency("fire", "corridor", "Alarm asap berbunyi di koridor", "cancel", 30*time.Hour); err != nil {
		return err
	}

	// ---- parkir sisi security (P2-PRK-01..04) ----
	areaP1, err := s.Sec.SaveParkingArea(admin, nil, security.ParkingAreaInput{PropertyID: &p.ID, LocationID: ptr(p.Areas["parking"]), Code: ptr("P1"), Name: ptr("Parkir Mobil LG"), AreaType: ptr("tenant"), Capacity: ptr(120)})
	if err != nil {
		return fmt.Errorf("parking area: %w", err)
	}
	if _, err := s.Sec.SaveParkingArea(admin, nil, security.ParkingAreaInput{PropertyID: &p.ID, Code: ptr("MTR"), Name: ptr("Parkir Motor"), AreaType: ptr("mixed"), Capacity: ptr(80)}); err != nil {
		return fmt.Errorf("parking area motor: %w", err)
	}
	for _, v := range []struct{ plate, typ, brand, color, owner string }{
		{"B 1208 VSN", "car", "Toyota Innova", "Hitam", "Penghuni / Tenant"},
		{"B 5021 NDG", "car", "Honda CR-V", "Putih", "Tenant korporat"},
		{"B 3344 KMG", "motorcycle", "Honda Vario", "Merah", "Staf " + sp.Name},
	} {
		ot := "tenant"
		if v.typ == "motorcycle" {
			ot = "staff"
		}
		if _, err := s.Sec.SaveVehicle(admin, nil, security.VehicleInput{PropertyID: &p.ID, PlateNumber: ptr(v.plate), VehicleType: ptr(v.typ), Brand: ptr(v.brand), Color: ptr(v.color), OwnerType: ptr(ot), OwnerName: ptr(v.owner), ParkingAreaID: &areaP1.ID, PermitUntil: ptr(day(180))}); err != nil {
			return fmt.Errorf("vehicle: %w", err)
		}
	}
	if _, err := s.Sec.RecordParkingEntry(secCtx, security.ParkingEntryInput{PropertyID: &p.ID, ParkingAreaID: &areaP1.ID, PlateNumber: "B 1208 VSN", Gate: ptr("Gate LG")}); err != nil {
		s.Log.Warn("demo parking entry", "err", err)
	}
	if _, err := s.Sec.RecordParkingEntry(secCtx, security.ParkingEntryInput{PropertyID: &p.ID, ParkingAreaID: &areaP1.ID, PlateNumber: "B 7711 TMU", Gate: ptr("Gate LG"), Note: ptr("Tamu")}); err == nil {
		_, _ = s.Sec.RecordParkingExit(secCtx, nil, &p.ID, "B 7711 TMU", nil)
	}
	if _, err := s.Sec.CreateParkingViolation(secCtx, security.ParkingViolationInput{PropertyID: &p.ID, ParkingAreaID: &areaP1.ID, PlateNumber: ptr("B 9876 XYZ"), ViolationType: ptr("blocking"),
		Description: ptr("Mobil menghalangi jalur keluar ambulans di LG"), ActionTaken: ptr("warning")}); err != nil {
		s.Log.Warn("demo parking violation", "err", err)
	}

	// ---- lost & found (P2-LNF-01..03) ----
	foundAt := now.Add(-6 * time.Hour)
	if _, err := s.Sec.CreateLostFoundItem(secCtx, security.LostFoundItemInput{PropertyID: &p.ID, Category: ptr("wallet"), Description: ptr("Dompet kulit coklat berisi kartu identitas"), FoundLocationID: ptr(p.Areas["lobby"]), FoundAt: &foundAt, FinderName: ptr("Petugas cleaning"), StorageLocation: ptr("Lemari pos keamanan, rak 2")}); err != nil {
		return fmt.Errorf("lost & found: %w", err)
	}
	keys, err := s.Sec.CreateLostFoundItem(secCtx, security.LostFoundItemInput{PropertyID: &p.ID, Category: ptr("keys"), Description: ptr("Kunci mobil Toyota dengan gantungan biru"), FoundLocationID: ptr(p.Areas["parking"]), FoundAt: &foundAt, StorageLocation: ptr("Lemari pos keamanan, rak 1")})
	if err != nil {
		return fmt.Errorf("lost & found keys: %w", err)
	}
	lostAt := now.Add(-8 * time.Hour)
	if rep, err := s.Sec.CreateLostReport(secCtx, security.LostReportInput{PropertyID: &p.ID, Category: ptr("keys"), Description: ptr("Kehilangan kunci mobil Toyota, gantungan biru"), LostLocationID: ptr(p.Areas["parking"]), LostAt: &lostAt, ReporterName: ptr("Pemilik kendaraan B 1208 VSN"), ReporterContact: ptr("+62 812 0000 1208")}); err == nil {
		_, _ = s.Sec.ActLostFoundItem(secSpv, keys.ID, "match", security.LostFoundActionInput{ReportID: &rep.ID})
	}

	// health asset setelah seluruh riwayat terbentuk + pengingat dokumen/warranty (masuk inbox supervisor)
	if _, err := s.Asset.RecomputeAllHealth(ctx, env.OrgID); err != nil {
		s.Log.Warn("demo health", "err", err)
	}
	if _, err := s.Asset.DocumentSweep(ctx, env.OrgID); err != nil {
		s.Log.Warn("demo document sweep", "err", err)
	}
	s.flush(ctx)
	logf("%s: P2 — kontak darurat 5, shift 5 + roster, dokumen 4, route 1, emergency 2, parkir, lost & found", sp.Prefix)
	return nil
}
