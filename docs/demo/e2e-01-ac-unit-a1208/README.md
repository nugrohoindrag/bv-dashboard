# E2E-01 — "AC tidak dingin" Unit A-1208, Vision Residence

Satu skenario end-to-end yang dijalankan **langsung di production** (`app.buildingvision.web.id`, organization demo `demo`) pada **26 September 2026**, melintasi seluruh aplikasi BuildingVision:

**Tenant App → Dashboard (Tenant Relation & Building Manager) → Staff App (teknisi, HP Android) → Dashboard (Engineering Supervisor) → Tenant App (konfirmasi + CSAT) → Dashboard (Feedback)**

Galeri pelengkap mencakup fitur Staff App lainnya, BVRooms (customer booking), dan Website.

| Objek yang terbentuk | Nilai |
|---|---|
| Service Request | **SR-2026-000045** — "AC kamar utama tidak dingin" (Air Conditioning, via `tenant_app`) |
| Work Order | **WO-2026-000040** — "Perbaikan AC kamar utama Unit A-1208 — tidak dingin" (corrective, prioritas Tinggi) |
| Lokasi | Podium Vision Residence › Tower A › Lantai 12 › Unit A-1208 |
| Hasil | Checklist 5/5, 2 part (AC Filter + Refrigerant R32 = Rp 405.000), 5 foto evidence, selesai dalam SLA, CSAT 5★ |
| Durasi | Tiket dibuat 02:18 WIB → WO dimulai 02:43 → selesai 03:15 → ditutup tenant 03:23 |

## Aktor & akun (password semua `Demo12345!`)

| Peran | Akun | Aplikasi |
|---|---|---|
| Tenant — Bapak Hendra Wibowo (Keluarga Wibowo, A-1208) | `apartment.tenant@buildingvision.local` | Tenant App |
| Tenant Relation Officer Vision Residence | `apartment.tenantrelation@buildingvision.local` | Dashboard |
| Building Manager Vision Residence | `apartment.building.manager@buildingvision.local` | Dashboard |
| Teknisi — Budi Santoso | `apartment.technician@buildingvision.local` | Staff App (APK, Xiaomi Android 13) |
| Engineering Supervisor | `engineering.demo@buildingvision.local` | Dashboard |

## Struktur folder

```
e2e-01-ac-unit-a1208/
├── README.md                     ← dokumen ini
└── screenshots/
    ├── tenant-app/               ← NN-tenant-app-<layar>.png   (viewport HP 390×844 @3x)
    ├── dashboard/                ← NN-dashboard-<layar>.png    (desktop 1440×900 @2x)
    ├── staff-app/                ← NN-staff-app-<layar>.png    (tangkapan layar HP asli 1080×2400)
    │   ├── fitur-lain/           ← Fxx — galeri fitur Staff App di luar skenario
    │   └── kendala/              ← Kxx — bukti bug yang ditemukan saat skenario (sudah diperbaiki)
    ├── bvrooms-app/              ← Bxx — galeri BVRooms customer app
    └── website/                  ← Wxx — galeri website landing
```

**Konvensi nama file:** `NN-<aplikasi>-<layar>-<keadaan>.png`. `NN` adalah nomor langkah **global 01–57** yang berurutan lintas folder, jadi mengurutkan semua file berdasarkan nomor sama dengan urutan kronologis skenario.

## Alur skenario

### Fase 1 — Tenant melaporkan masalah (Tenant App)

| # | Layar | Yang terjadi |
|---|---|---|
| 01 | [Onboarding](screenshots/tenant-app/01-tenant-app-onboarding-welcome.png) | Tenant membuka Tenant App untuk pertama kali. |
| 02 | [Login](screenshots/tenant-app/02-tenant-app-login.png) | Login dengan email & password. |
| 03 | [Home](screenshots/tenant-app/03-tenant-app-home.png) | Home menampilkan Unit A-1208, ringkasan My Requests, akses cepat, dan tombol **Report an Issue**. |
| 04 | [Pilih lokasi](screenshots/tenant-app/04-tenant-app-lapor-pilih-lokasi.png) | Unit tenant terisi otomatis; opsi Common Area tersedia. |
| 05 | [Pilih kategori](screenshots/tenant-app/05-tenant-app-lapor-pilih-kategori.png) | Kategori **Air Conditioning**. |
| 06 | [Uraian masalah](screenshots/tenant-app/06-tenant-app-lapor-uraian-masalah.png) | Judul, uraian, preferensi kontak "Pesan di aplikasi", waktu kunjungan 13:00, catatan akses. |
| 07 | [Lampirkan foto](screenshots/tenant-app/07-tenant-app-lapor-lampirkan-foto.png) | Foto unit AC dilampirkan. |
| 08 | [Konfirmasi](screenshots/tenant-app/08-tenant-app-lapor-konfirmasi.png) | Ringkasan sebelum kirim. |
| 09 | [Berhasil terkirim](screenshots/tenant-app/09-tenant-app-lapor-berhasil-terkirim.png) | Tiket **SR-2026-000045** dibuat. |
| 10 | [Detail tiket — Terkirim](screenshots/tenant-app/10-tenant-app-detail-tiket-status-terkirim.png) | Tracking dimulai: "Ticket dikirim". |

### Fase 2 — Triage & penugasan (Dashboard)

| # | Layar | Yang terjadi |
|---|---|---|
| 11 | [Login dashboard](screenshots/dashboard/11-dashboard-login.png) | Staf building management login ke dashboard web. |
| 12 | [Overview](screenshots/dashboard/12-dashboard-overview.png) | SR-2026-000045 langsung muncul di panel **Tenant Requests** (Baru, prioritas Sedang). |
| 13 | [Daftar Service Request](screenshots/dashboard/13-dashboard-service-request-list-tiket-baru.png) | Tenant Relation melihat tiket baru, SLA berjalan, belum ditugaskan. |
| 14 | [Detail SR — Baru](screenshots/dashboard/14-dashboard-service-request-detail-baru.png) | Detail tiket, pemohon, lokasi, SLA response & resolution. |
| 15 | [SR diterima + pesan ke tenant](screenshots/dashboard/15-dashboard-service-request-diterima-pesan-ke-tenant.png) | Tenant Relation menekan **Terima** lalu mengirim pesan ke tenant: teknisi datang pukul 13:00. |
| 16 | [Buat Work Order dari SR](screenshots/dashboard/16-dashboard-buat-work-order-dari-service-request.png) | Building Manager membuat WO corrective prioritas Tinggi, checklist "PM Bulanan AC / HVAC", wajib foto, team Engineering, assignee **Budi Santoso**. |
| 17 | [Detail WO — Ditugaskan](screenshots/dashboard/17-dashboard-work-order-detail-ditugaskan.png) | **WO-2026-000040** terbentuk, checklist 0/5. |
| 18 | [SR tertaut ke WO](screenshots/dashboard/18-dashboard-service-request-terkait-work-order.png) | Tab Terkait pada SR menunjukkan WO hasil turunan. |
| 19 | [Daftar WO Engineering](screenshots/dashboard/19-dashboard-work-order-list-engineering.png) | Engineering Supervisor melihat WO baru di antrean. |
| 20 | [Tenant — tiket diproses](screenshots/tenant-app/20-tenant-app-detail-tiket-diproses-ada-pesan.png) | Di Tenant App status menjadi **Diterima**, ada 1 pesan belum dibaca. |
| 21 | [Tenant — pesan](screenshots/tenant-app/21-tenant-app-pesan-dengan-building-management.png) | Tenant membaca pesan dan membalas "Jam 13:00 ada ART di unit". |

### Fase 3 — Teknisi mengerjakan (Staff App, HP asli)

| # | Layar | Yang terjadi |
|---|---|---|
| 22 | [Login](screenshots/staff-app/22-staff-app-login.png) | Budi login ke Staff App (build prod). |
| 23 | [Home teknisi](screenshots/staff-app/23-staff-app-home-teknisi.png) | Ringkasan pekerjaan hari ini dan menu cepat. |
| 24 | [Inbox — WO ditugaskan](screenshots/staff-app/24-staff-app-inbox-notifikasi-wo-ditugaskan.png) | Notifikasi **Work Order Assigned — WO-2026-000040** di posisi teratas. |
| 25 | [Detail WO](screenshots/staff-app/25-staff-app-detail-wo-ditugaskan.png) | Prioritas, jadwal 13:00–17:00, lokasi, evidence wajib foto. |
| 26 | [Konfirmasi mulai](screenshots/staff-app/26-staff-app-konfirmasi-mulai-pekerjaan.png) | Dialog "Konfirmasi Memulai Pengerjaan". |
| 27 | [Sedang dikerjakan](screenshots/staff-app/27-staff-app-wo-sedang-dikerjakan.png) | Status **Sedang Dikerjakan**; foto sebelum pengerjaan diunggah. |
| 28 | [Checklist kosong](screenshots/staff-app/28-staff-app-form-checklist-kosong.png) | Form checklist 5 item. |
| 29 | [Checklist terisi](screenshots/staff-app/29-staff-app-form-checklist-terisi.png) | Filter & belt baik, suhu supply 14,5 °C, foto kondisi unit, catatan teknisi. |
| 30 | [Pilih spare part](screenshots/staff-app/30-staff-app-pilih-spare-part.png) | Daftar stok gudang engineering. |
| 31 | [Catat AC Filter](screenshots/staff-app/31-staff-app-catat-pemakaian-ac-filter.png) | 1 pcs × Rp 85.000. |
| 32 | [Parts tercatat](screenshots/staff-app/32-staff-app-parts-usage-tercatat.png) | + Refrigerant R32 1 kg × Rp 320.000 → total **Rp 405.000** (stok berkurang otomatis). |
| 33 | [Daftar corrective](screenshots/staff-app/33-staff-app-daftar-corrective-sedang-dikerjakan.png) | WO tampil di jadwal corrective hari ini. |
| 34 | [Ringkasan pengerjaan](screenshots/staff-app/34-staff-app-ringkasan-pengerjaan-checklist-parts.png) | Checklist, parts usage, activity report dalam satu halaman. |
| 35 | [Activity report](screenshots/staff-app/35-staff-app-activity-report-isi-laporan-foto.png) | Laporan kegiatan + foto sesudah. |
| 36 | [Dashboard — WO berjalan](screenshots/dashboard/36-dashboard-work-order-sedang-dikerjakan-parts-biaya.png) | Supervisor memantau progres, biaya part, dan checklist secara real-time. |
| 37 | [Checklist 5/5](screenshots/staff-app/37-staff-app-checklist-lengkap-5-dari-5-setelah-fix.png) | Setelah perbaikan bug (lihat *Temuan*), checklist terbaca lengkap. |
| 38 | [Konfirmasi selesai](screenshots/staff-app/38-staff-app-konfirmasi-selesaikan-pekerjaan.png) | Dialog "Konfirmasi Menyelesaikan Pengerjaan". |
| 39 | [WO selesai](screenshots/staff-app/39-staff-app-wo-selesai.png) | Status **Selesai**; SR otomatis menjadi *resolved*. |
| 40 | [Checklist baca-saja](screenshots/staff-app/40-staff-app-checklist-read-only-setelah-selesai.png) | Setelah selesai checklist terkunci. |
| 41 | [Riwayat activity report](screenshots/staff-app/41-staff-app-activity-report-riwayat.png) | Laporan & foto tersimpan. |
| 42 | [Home setelah selesai](screenshots/staff-app/42-staff-app-home-setelah-wo-selesai.png) | Pekerjaan berjalan kembali 0. |

### Fase 4 — Verifikasi & penutupan WO (Dashboard, Engineering Supervisor)

| # | Layar | Yang terjadi |
|---|---|---|
| 43 | [WO selesai](screenshots/dashboard/43-dashboard-work-order-selesai-detail-biaya-parts.png) | Biaya aktual Rp 405.000, parts, vendor internal. |
| 44 | [Checklist 5/5](screenshots/dashboard/44-dashboard-work-order-checklist-5-dari-5.png) | Hasil checklist per item beserta penjawab & waktu. |
| 45 | [Evidence](screenshots/dashboard/45-dashboard-work-order-evidence-foto.png) | 5 foto (before, checklist ×2, after, foto laporan) berstatus *ready*; **Selesai dalam SLA**. |
| 46 | [Activity log](screenshots/dashboard/46-dashboard-work-order-activity-log.png) | Jejak audit lengkap dari pembuatan sampai selesai. |
| 47 | [Verifikasi & tutup](screenshots/dashboard/47-dashboard-verifikasi-tutup-work-order.png) | Supervisor menulis catatan verifikasi lalu **Tutup**. |
| 48 | [WO ditutup](screenshots/dashboard/48-dashboard-work-order-ditutup.png) | Status **Ditutup**. |

### Fase 5 — Konfirmasi tenant & CSAT

| # | Layar | Yang terjadi |
|---|---|---|
| 49 | [Home tenant](screenshots/tenant-app/49-tenant-app-home-perlu-konfirmasi.png) | Kartu My Requests menandai tiket perlu konfirmasi. |
| 50 | [Inbox tenant](screenshots/tenant-app/50-tenant-app-inbox-notifikasi-tiket-selesai.png) | Notifikasi *Ticket Updated*, pesan, *Ticket Received*, *Ticket Created*. |
| 51 | [Minta konfirmasi](screenshots/tenant-app/51-tenant-app-tiket-terselesaikan-minta-konfirmasi.png) | "Pekerjaan dilaporkan selesai", dengan pilihan **Konfirmasi selesai** atau **Buka kembali**. |
| 52 | [Konfirmasi](screenshots/tenant-app/52-tenant-app-konfirmasi-pekerjaan-selesai.png) | "Ya, sudah selesai". |
| 53 | [Penilaian 5★](screenshots/tenant-app/53-tenant-app-beri-penilaian-csat-5-bintang.png) | CSAT 5 bintang + komentar. |
| 54 | [Tiket ditutup](screenshots/tenant-app/54-tenant-app-tiket-ditutup-dengan-penilaian.png) | Status **Ditutup**, timeline lengkap. |
| 55 | [SR ditutup (dashboard)](screenshots/dashboard/55-dashboard-service-request-ditutup-feedback-5-bintang.png) | SR berstatus Ditutup dengan feedback 5★. |
| 56 | [Timeline SR](screenshots/dashboard/56-dashboard-service-request-activity-timeline.png) | Riwayat aktivitas SR. |
| 57 | [Feedback / CSAT](screenshots/dashboard/57-dashboard-tenant-feedback-csat.png) | Feedback muncul di halaman Tenant Relation › Feedback. |

### Siklus status

| Objek | Perjalanan status |
|---|---|
| Service Request | Baru → Diterima → *(WO dibuat)* → Terselesaikan (otomatis saat WO selesai) → Ditutup (dikonfirmasi tenant) |
| Work Order | Ditugaskan → Sedang Dikerjakan → Selesai → Ditutup (verifikasi supervisor) |

## Temuan & perbaikan

Selama skenario ditemukan beberapa bug. Semuanya sudah diperbaiki, di-commit, dan di-push ke `master` masing-masing repo.

| # | Aplikasi | Masalah | Akar masalah | Perbaikan | Commit |
|---|---|---|---|---|---|
| 1 | Staff App | Foto evidence tertahan *pending* tanpa thumbnail; di Status Sinkronisasi "Rejected · unknown field sha256" ([K02](screenshots/staff-app/kendala/K02-staff-app-sync-foto-rejected-unknown-field-sha256.png)) | Body `POST /attachments/{id}/confirm` mengirim `sha256` (server `DisallowUnknownFields`) dan `captured_at` lokal tanpa offset (gagal parse RFC3339) → 400 → file dianggap ditolak | Kirim `captured_at` UTC tanpa `sha256`; fake server di test kini meniru validasi server | bv-mobile-staff `423c7d6` |
| 2 | Staff App | "Pekerjaan Selesai" terblokir "Masih ada 1 item checklist wajib yang belum dijawab" walau server mencatat 5/5 ([K01](screenshots/staff-app/kendala/K01-staff-app-checklist-foto-dianggap-belum-dijawab.png)) | `ChecklistRunItem.isAnswered` mengabaikan item bertipe `photo` | Item foto terjawab bila punya `attachment_id`/`answered_at` | bv-mobile-staff `423c7d6` |
| 3 | Staff App | Menu More → Scan QR kembali ke Home | Redirect router menganggap `/scan/full` bagian tab Scan (yang tidak dimiliki teknisi) | Cek tab hanya untuk path persis | bv-mobile-staff `423c7d6` |
| 4 | Staff App + API | Menu News membuka Inbox notifikasi | Belum ada endpoint pengumuman untuk staf | `GET /api/v1/staff/announcements(/{id})` (audience staff\|all) + halaman News & detail | bv-dashboard `af24548`, bv-mobile-staff `423c7d6` |
| 5 | Dashboard | Dropdown Team/Assignee/Aset di drawer "Buat Work Order" tidak terlihat | Popover `z-[997]` di bawah drawer `z-[998]` | Naikkan ke `z-[1100]` | bv-dashboard `af24548` |
| 6 | Dashboard | Feedback/CSAT menampilkan kode kategori (`air_conditioning`) | Respons feedback tanpa nama kategori | Tambah `category_name` | bv-dashboard `2d54195` |
| 7 | Tenant App | Preferensi kontak tampil `app_message` ([08](screenshots/tenant-app/08-tenant-app-lapor-konfirmasi.png), [53](screenshots/tenant-app/53-tenant-app-beri-penilaian-csat-5-bintang.png)) | Kode ditampilkan apa adanya | Label tunggal `contactPreferenceLabel` | bv-tenant-app `80341d0` |

Screenshot di atas diambil **sebelum** perbaikan dideploy, sehingga bug 5–7 masih terlihat pada beberapa layar. Empat foto yang tertahan akibat bug 1 sudah di-*confirm* ulang di server; setelah itu tidak ada lagi lampiran berstatus pending di production.

Catatan lain (bukan bug kode):

- **BVRooms** di `rooms.buildingvision.web.id` dibangun dengan `organization_slug=buildingvision` (default `infra/scripts/deploy-pwa.sh`), bukan `demo`. Akibatnya akun customer demo ditolak "Nomor belum terdaftar", dan `app-config` org `buildingvision` mengembalikan 500. Galeri BVRooms diambil dengan mengalihkan slug ke `demo` di sisi browser; deployment belum diubah.
- **Angka overdue/SLA di dashboard tinggi** (mis. 173 overdue) karena data demo terakhir di-seed 17 Sep 2026. Jalankan *Reset & Reseed* sebelum demo langsung.
- Engineering Supervisor tidak punya izin melihat Service Request, sehingga WO dibuat oleh Building Manager. Ini sesuai matriks permission, bukan bug.

## Galeri pelengkap

**Staff App — fitur lain** (`screenshots/staff-app/fitur-lain/`): [F01 Create Tiket (form WO)](screenshots/staff-app/fitur-lain/F01-staff-app-create-tiket-form-work-order.png) · [F02 Menu More](screenshots/staff-app/fitur-lain/F02-staff-app-menu-more.png) · [F03 History](screenshots/staff-app/fitur-lain/F03-staff-app-history-pekerjaan.png) · [F04 Akun](screenshots/staff-app/fitur-lain/F04-staff-app-akun-profil.png) · [F05 Preventive](screenshots/staff-app/fitur-lain/F05-staff-app-daftar-preventive-maintenance.png) · [F06 Detail PM genset](screenshots/staff-app/fitur-lain/F06-staff-app-detail-preventive-genset.png) · [F07 Tenant Relation](screenshots/staff-app/fitur-lain/F07-staff-app-tenant-relation.png)

**BVRooms customer app** (`screenshots/bvrooms-app/`, customer 1 · login HP + PIN): [B01 Welcome](screenshots/bvrooms-app/B01-bvrooms-onboarding-welcome.png) · [B02 Masuk/Daftar/Guest](screenshots/bvrooms-app/B02-bvrooms-pilihan-masuk-daftar-guest.png) · [B03 Login PIN](screenshots/bvrooms-app/B03-bvrooms-login-nomor-hp-pin.png) · [B04 Katalog](screenshots/bvrooms-app/B04-bvrooms-home-katalog-properti.png) · [B05 Detail properti](screenshots/bvrooms-app/B05-bvrooms-detail-properti-grand-vision-hotel.png) · [B06 Tipe kamar](screenshots/bvrooms-app/B06-bvrooms-pilih-tipe-kamar.png) · [B07 Daftar booking](screenshots/bvrooms-app/B07-bvrooms-daftar-booking.png) · [B08 Booking PAID](screenshots/bvrooms-app/B08-bvrooms-detail-booking-paid.png) · [B09 Saved](screenshots/bvrooms-app/B09-bvrooms-saved-wishlist.png) · [B10 Inbox](screenshots/bvrooms-app/B10-bvrooms-inbox-notifikasi.png) · [B11 Akun](screenshots/bvrooms-app/B11-bvrooms-akun-profil.png)

**Website** (`screenshots/website/`): [W01 Hero](screenshots/website/W01-website-landing-hero.png) · [W02 Landing penuh](screenshots/website/W02-website-landing-full-page.png) · [W03 Solusi Apartment](screenshots/website/W03-website-solusi-apartment.png) · [W04 Download Apps](screenshots/website/W04-website-download-apps.png) · [W05 Start Free Trial](screenshots/website/W05-website-start-free-trial-signup.png)

## Cara mengulang skenario

1. Pastikan data demo segar: Dashboard `internal@…` → Settings › Demo Data › **Reset & Reseed** (atau `bvctl demo reset --reseed`).
2. Ikuti Fase 1–5 dengan akun di atas. Tenant App dapat dibuka di `tenant.buildingvision.web.id`; Staff App memakai `apk/bv-staff-release.apk` (build dengan perbaikan #1–#4).
3. Pada langkah 16, pilih checklist **PM Bulanan AC / HVAC** agar Fase 3 memuat checklist berfoto.

## Catatan pengambilan gambar

- Tenant App, BVRooms, Dashboard, dan Website diambil dengan Chrome headless (Playwright): mobile 390×844 @3x, desktop 1440×900 @2x. Staff App diambil via `adb screencap` dari HP Xiaomi (Android 13) dengan APK rilis prod.
- Foto AC pada laporan & evidence: "Panasonic AIR CONDITIONER INDOOR UNIT CS-C10KJ2 (2)" oleh **Dinkun Chen**, [CC BY-SA 4.0](https://creativecommons.org/licenses/by-sa/4.0), via Wikimedia Commons (foto "sesudah" adalah potongan dari foto yang sama).
