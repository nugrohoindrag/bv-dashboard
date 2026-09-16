# BuildingVision — Demo Guide (Demo Seed Database v1.1)

Panduan menyiapkan, mereset, memverifikasi, dan mendemokan **tiga environment demo** BuildingVision: Hotel, Apartment, Office —
termasuk **BVRooms Customer Booking** (hotel & sewa harian apartemen). Implementasi: `api/internal/demo/` (seed lewat service layer aplikasi,
bukan SQL langsung; event domain dikirim sinkron ke Notification & Search sehingga inbox terisi tanpa worker).

## 1. Cara seed

Seluruh environment berada dalam satu organization demo **`demo` — "BuildingVision Demo"** (agar Property Switcher dan BVRooms white-label konsisten; RLS org-scoped tetap berlaku).

CLI (`api/`, koneksi role aplikasi `bv_app` — bukan superuser, karena RLS dilewati superuser):

```bash
cd api
go run ./cmd/bvctl migrate            # skema terbaru (koneksi owner: BV_ADMIN_DATABASE_URL)
go run ./cmd/bvctl seed               # katalog permission + role sistem (termasuk platform.demo_data.* untuk admin_internal)
go run ./cmd/bvctl demo seed                       # Seed All Profiles (Hotel + Apartment + Office) ± 30–45 detik
go run ./cmd/bvctl demo seed --profile hotel       # per profile: hotel | apartment | office (koma untuk beberapa)
go run ./cmd/bvctl demo status
```

Env yang dipakai: `BV_DATABASE_URL` (default `bv_app` lokal), `BV_ENV=local` (OTP mock mengembalikan `dev_code`), `BV_S3_ENDPOINT=memory` bila MinIO tidak berjalan (foto/evidence demo tetap tercatat, file diunggah bila storage tersedia).

Dashboard: **Settings → Demo Data** (hanya role `admin_internal` pada organization internal; `bvctl seed --internal` membuat `internal@buildingvision.id / Internal12345!`). Tombol **Seed All Profiles**, **Seed {Hotel|Apartment|Office} Demo**, **Verify**, **Reset & Reseed** (konfirmasi mengetik `RESET`).

Seed **idempotent**: profile yang sudah ada dilewati (tidak menggandakan). Untuk memperbarui dataset, lakukan reset lalu seed.

## 2. Cara reset

```bash
go run ./cmd/bvctl demo reset                       # hapus seluruh organization demo (91 tabel org-scoped, urutan FK otomatis)
go run ./cmd/bvctl demo reset --reseed              # Reset & Reseed All
go run ./cmd/bvctl demo reset --profile hotel       # hapus organization demo, lalu seed ulang profile lain (apartment, office)
```

Guard (§39): hanya organization ber-marker `settings.demo` dengan slug `demo` yang dapat dihapus; organization lain (customer/production) tidak pernah disentuh. API `POST /api/v1/admin/demo/reset` wajib `confirm: "RESET"`. Di production tooling demo nonaktif kecuali `BV_DEMO_ENABLED=true`.

## 3. Demo credentials

Password seluruh akun demo: **`Demo12345!`** (dev/demo saja). Login dashboard `client: web`; Tenant App `client: tenant_app`.

| Peran | Email | Scope |
|---|---|---|
| Organization Admin (demo) | admin.demo@buildingvision.local | seluruh property demo |
| Hotel Manager | hotel.manager@buildingvision.local | Grand Vision Hotel |
| Apartment Manager | apartment.manager@buildingvision.local | Vision Residence |
| Office Manager | office.manager@buildingvision.local | Vision Business Center |
| Building Manager per profile | {hotel,apartment,office}.building.manager@buildingvision.local | per property |
| Operations Manager | operations.demo@buildingvision.local | semua |
| Engineering Supervisor | engineering.demo@buildingvision.local | semua |
| Technician per profile | {hotel,apartment,office}.technician@… dan …technician2@… | per property |
| Housekeeping Supervisor / Staff | housekeeping.demo@… / {profile}.housekeeper@…, …housekeeper2@… | semua / per property |
| Security Supervisor / Officer | security.demo@… / {profile}.security@…, …security2@… | semua / per property |
| Tenant Relation Manager / Officer (GRO) | tenantrelation.demo@… / {profile}.tenantrelation@… | semua / per property |
| Finance Manager / Staff | finance.demo@… / {profile}.finance@… | semua / per property |
| **Hotel Guest (Tenant/Guest App)** | hotel.guest@buildingvision.local | Room 501, stay aktif |
| **Apartment Tenant** | apartment.tenant@buildingvision.local | Unit A-1208 (owner, 3 occupant) |
| Apartment tenant lain | dian.puspita@resident.test, irwan.santoso@resident.test, gita.weekly@rent.test (penyewa mingguan aktif), andi.buyer@prospect.test (pemilik baru hasil handover) | |
| **Apartment Prospect** | apartment.prospect@buildingvision.local | pendaftaran mandiri → *pending validation* (login ditolak sampai divalidasi Tenant Relation) |
| **Office Tenant (PIC, tenant_admin)** | office.tenant@buildingvision.local | PT Nusantara Digital — unit 501 & 502 |
| Office Tenant user kedua | office.tenant2@buildingvision.local | hanya unit 502 (uji isolasi lintas unit) |
| PIC office lain | bambang@sinarlogistik.test, laras@kreatifmedia.test, melati@konsultanprima.test, gilang@startupvision.test | |
| Admin Internal (Demo Data) | internal@buildingvision.id / Internal12345! (`bvctl seed --internal`) | organization internal |

**BVRooms customer** (organization `demo`, OTP provider `mock`):

| Customer | Email | Nomor HP (OTP) | Kondisi |
|---|---|---|---|
| 1 | bvrooms.customer@buildingvision.local | +62812-0000-0101 | upcoming PAID (transfer diverifikasi), riwayat CHECK OUT + review 5★, wishlist, notifikasi sudah dibaca |
| 2 | bvrooms.customer2@buildingvision.local | +62812-0000-0102 | UNPAID (transfer dipilih, bukti belum), riwayat 4★, wishlist kosong; apartemen: pay-at-property → CHECK IN |
| 3 | bvrooms.customer3@buildingvision.local | +62812-0000-0103 | multi-room (Standard + Deluxe, breakfast + extra bed) CHECK IN, riwayat 3★ |
| 4 | bvrooms.customer4@buildingvision.local | +62812-0000-0104 | EXPIRED (sweep), CANCELLED, riwayat 2★; apartemen UNPAID |
| 5 | bvrooms.customer5@buildingvision.local | +62812-0000-0105 | pay-at-property Suite pada tanggal fully booked, bukti transfer menunggu verifikasi, riwayat 1★ |

Cara memperoleh OTP mock (dev): `POST /api/v1/bvrooms/auth/otp/request {"organization_slug":"demo","phone":"081200000101","purpose":"login"}` → respons memuat `dev_code` (hanya `BV_ENV` local/test; log API juga mencetaknya). Lanjutkan `POST /bvrooms/auth/otp/verify` dengan kode tersebut. Cooldown resend 60 detik per nomor.

## 4. Property yang tersedia

| Profile | Property | Struktur | Marker |
|---|---|---|---|
| Hotel | **Grand Vision Hotel** (Senayan, Jakarta Selatan) | Main Tower, LG + lantai 0–10; 30 kamar: 301–310 Standard, 401–410 Deluxe, 501–506 Executive, 507–510 Suite; status kamar bervariasi (available/occupied/dirty/clean/inspected/out_of_order/out_of_service) | `properties.settings.demo_dataset = DEMO-HOTEL-V1` |
| Apartment | **Vision Residence** (Kemang) | Podium (LG, GF–2) + Tower A & B lantai 3–12, 41 unit residential (A/B-xx01, xx02, A-1208); fasilitas kolam, gym, function room, BBQ | `DEMO-APARTMENT-V1` |
| Office | **Vision Business Center** (Rasuna Said) | Main Tower lantai 0–10, 30 office unit (x01–x03), Meeting Room A/B, Conference, Training, Lounge, Visitor Reception, Pos Keamanan | `DEMO-OFFICE-V1` |

Setiap property memiliki: tim Engineering/Security/Housekeeping/Tenant Relation/Management/Finance, 10 aset (AC/AHU, lift ×2, generator, pompa, panel LVMDP, fire alarm, boiler (inactive), CCTV; status active/under_maintenance/inactive), 4 PM plan (AC bulanan, genset mingguan, pompa bulanan, lift triwulan) dengan WO PM selesai + overdue, gudang engineering + 11 item stok (opening stock, stock in, adjustment, usage, 2 item low stock), 4 checkpoint + rute patroli + 2 jadwal + patroli selesai/berjalan/missed checkpoint, 2 insiden, 3 jadwal cleaning + cleaning selesai (evidence + inspeksi)/berjalan/menunggu, 4–5 fasilitas dengan booking confirmed/pending/cancelled/completed + skenario konflik slot, 5 visitor (upcoming/checked-in/checked-out/cancelled/pending verification), 7 invoice (paid/overdue/due soon + payment pending/outstanding/failed payment/draft/cancelled), 4 pengumuman, 14–15 tiket lintas domain & status.

## 5. Tenant yang tersedia

- **Hotel**: 11 reservasi Front Office (3 in-house termasuk Andi Prasetyo Room 501 = akun Guest App, 2 confirmed, 1 upcoming, 3 checked-out dengan turnover housekeeping, 2 cancelled) + 3 blok corporate Suite pada H+15 (fully booked) + 15 booking BVRooms.
- **Apartment**: 10 tenant (Keluarga Wibowo A-1208 owner 3 occupant; Dian Puspita; Keluarga Halim; Bagus Wicaksono; Keluarga Santoso 3 occupant; Nurul Hidayah; Keluarga Tan; Rafi Ramadhan; Keluarga Kusnadi; Vera Anggraeni), penyewa hasil rental (Fajar — harian selesai, Gita — mingguan aktif), pemilik hasil sales handover (Andi Kurniawan A-1101), prospect pending validation.
- **Office**: 10 tenant company (PT Nusantara Digital 501/502, PT Sinar Logistik 101, CV Kreatif Media 201, PT Bank Vision Syariah 301/302, PT Konsultan Prima 401, PT Global Insurance 601/602, PT Teknologi Maju 701, Kantor Hukum Adi & Rekan 801, PT Ekspor Pangan 901/902, PT Startup Vision 1001) dengan PIC dan authorized users.

## 6. Skenario E2E (SC-001…SC-037)

| Kode | Skenario | Data demo |
|---|---|---|
| E2E-01 / SC-001, 003, 007, 009, 015 | Apartment A-1208 "AC tidak dingin" → Engineering WO → teknisi → parts usage (AC filter, refrigerant) → foto sebelum/sesudah + checklist → resolved → konfirmasi tenant → closed → CSAT 5★ | tiket pertama Vision Residence |
| E2E-02 / SC-004 | Hotel Room 501 cleaning request → HK task → checklist → evidence → completed → notifikasi tamu | tiket pertama Grand Vision Hotel |
| E2E-03 / SC-005, 012 | Office visitor access issue → Security task → verifikasi → resolusi → notifikasi | tiket pertama Vision Business Center |
| E2E-04 / SC-006 | Complaint → Tenant Relation → komunikasi (komentar staf) → follow-up → resolusi → feedback 4★ | tiket kedua Office; complaint dengan **reopen** (SC-008) di Hotel & Apartment |
| SC-002 | Laporan area umum | "Common area cleaning" (lobby lift, koridor) |
| SC-010, 023 | Facility booking + konflik slot ditolak | setiap property; office: Meeting Room A/B |
| SC-011, 012, 024 | Visitor registration → pass → verifikasi security → entry/exit | setiap property |
| SC-013, 014 | Invoice → pembayaran manual → verifikasi (paid); pending; failed; overdue | setiap property |
| SC-016 | Preventive maintenance: schedule upcoming/due/completed/overdue | PM AC bulanan (WO selesai + WO overdue) |
| SC-017, 018 | Hotel reservation → room assignment → check-in → stay → check-out → turnover | Front Office Grand Vision Hotel |
| SC-019 | Unit sales: listing → lead (contact → site visit → qualify) → reservasi → kontrak → sold → handover (+ akun Tenant App pemilik) | A-1101; B-1101 reserved; leads contacted/lost |
| SC-020..022 | Rental daily (selesai), weekly (aktif + onboarding), monthly (confirmed) + inquiry | A-0701, A-0702, B-0701 |
| SC-025, 026 | Cross-tenant / cross-unit authorization | hotel.guest vs apartment; office.tenant2 hanya unit 502 |
| SC-027..037 | BVRooms: guest access (katalog publik), OTP login, availability (Suite fully booked H+15 vs tipe lain tersedia), multi-room, manual payment (bukti → verifikasi Finance), pay at property, payment expiry (sweep), lifecycle PAID→CHECK IN→CHECK OUT→review, wishlist, notifikasi | customer 1–5 |

## 7. Recommended demo journey (golden path)

**Hotel** — login `hotel.guest@…` (Tenant/Guest App): lihat stay Room 501 → buat laporan → dashboard `engineering.demo@…`: WO → teknisi `hotel.technician@…` start → parts → foto → complete → resolve → tamu konfirmasi → CSAT. Lalu Front Office (`hotel.manager@…`): reservasi Sinta Dewi (confirmed) → assign room → check-in → housekeeping turnover kamar 303 → check-out. BVRooms: customer 1 login OTP → katalog → Grand Vision Hotel → pilih tanggal → tipe kamar → breakfast → booking → transfer → unggah bukti → `finance.demo@…` verify → PAID.

**Apartment** — login `apartment.tenant@…`: laporkan AC → E2E-01 → CSAT. Kemudian booking Function Room → registrasi visitor → tagihan service charge → bayar manual. Commercial (`apartment.manager@…`): Unit Sales (lead → reservasi B-1101) & Unit Rental (kuotasi daily/weekly/monthly → booking → onboarding).

**Office** — login `office.tenant@…`: laporkan pantry/lobby → Tenant Relation/Housekeeping → resolusi. Booking Meeting Room A → registrasi tamu → `security.demo@…` verifikasi masuk/keluar.

## 8. Admin Internal → Demo Data

Menu **Settings → Demo Data** (hanya `admin_internal`; role lain tidak melihat menu dan endpoint `/api/v1/admin/demo*` menolak 403). Kartu per profile menampilkan Status, Seed Version, Last Seeded, Record Count, Last Reset. Aksi: Seed All Profiles, Seed {profile} Demo, Reset & Reseed (dialog konfirmasi: "This action will remove all demo data… Production/customer data will not be affected", ketik `RESET`), Verify (laporan §40 ditampilkan di halaman).

Endpoint: `GET /api/v1/admin/demo`, `POST /api/v1/admin/demo/seed {profiles}`, `POST /api/v1/admin/demo/reset {profiles, reseed, confirm:"RESET"}`, `POST /api/v1/admin/demo/verify`.

## 9. Verifikasi

```bash
go run ./cmd/bvctl demo verify          # exit code 3 bila FAIL
go run ./cmd/bvctl demo verify --json
```

Laporan mencakup HOTEL / APARTMENT / OFFICE (≈30 pemeriksaan per profile: property, kamar/unit, tenant, housekeeping, security, engineering, tenant relation, tiket, WO, evidence, checklist, SLA, fasilitas, visitor, billing, payments, vendor, inventory, low stock, parts usage, aset, notifikasi, pengumuman, CSAT), E2E SCENARIOS, BVROOMS CUSTOMER BOOKING (§21A.25 lengkap: customer, mock OTP, sesi, listing, foto, tipe kamar, kamar, rate, availability, add-on, promo, banner, wishlist, booking, multi-room, booking rooms, hotel reservation link, manual payment, proof, verifikasi, pay at property, expiry, check-in/out, review, notifikasi, riwayat, isolasi customer & organization, double booking prevention), SECURITY. `RESULT: PASS` bila seluruh pemeriksaan lulus. Integration test: `go test ./internal/app -run TestDemoSeed` (seed → verify → login semua akun → otorisasi → idempotensi → reset sebagian & total).

## 10. Known limitations

- Ketiga profile berbagi satu organization demo; **reset sebagian** = hapus organization demo lalu seed ulang profile lain (±45 detik total).
- File evidence/foto adalah JPEG 1×1 deterministik; bila object storage (MinIO/S3) tidak aktif, baris attachment/foto tetap ada namun URL unduh tidak dapat dibuka.
- Tanggal seed relatif terhadap hari eksekusi (tiket "kemarin", PM "40 hari lalu", stay hotel "H-3…H+2"); jalankan reseed bila demo dipakai setelah beberapa hari agar tetap terlihat "hari ini".
- Pembayaran online (VA/gateway) tetap **ON HOLD**: BVRooms & Billing memakai alur manual (transfer + verifikasi Finance) dan pay-at-property; VA tampil sebagai opsi mockup nonaktif.
- OTP SMS memakai provider mock (`dev_code` hanya di env local/test); Web Push aktif hanya bila kunci VAPID dikonfigurasi.
- Notifikasi seed dikirim sinkron; notifikasi yang dipicu interaksi UI setelah seed tetap membutuhkan worker (`worker.exe`) seperti biasa.
- `bvctl demo` menolak koneksi superuser Postgres (RLS dilewati superuser); gunakan role `bv_app`.
