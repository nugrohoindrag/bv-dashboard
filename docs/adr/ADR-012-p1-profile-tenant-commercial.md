# ADR-012 — P1 v1.3: Property Profile, identitas tenant, objek komersial, dan keputusan teknis TD-P1-001..012

Status: **Diterima** · Tanggal: 2026-09-16 · Sumber: PRD P1 v1.3, Naming Convention v2.0, Profile Selection & Product Onboarding Brief · Detail: `docs/p1-v1.3-gap-analysis.md` §E

Konteks: P1 menambahkan Property Profile (Hotel / Apartment / Office), Mobile Tenant, Tenant Relation, Facility Booking, Visitor,
Billing & Payment, Vendor, Inventory, Hotel Booking Management, Apartment Unit Sales & Rental, dan Reports di atas arsitektur P0
(modular monolith Go + Postgres RLS + Task Engine + River). Prioritas kebenaran: arsitektur yang sudah berjalan → PRD v1.3 → NC v2.0.
Semua keputusan di bawah menghindari rewrite besar dan menjaga vocabulary kanonik.

Keputusan (ringkas; rasional & konsekuensi ada di gap analysis):

1. **TD-P1-001 Profile di Property.** `properties.profile` NOT NULL (`office` untuk data lama) + `property_profile_configs`. Capability
   dihitung server (`internal/profile`), terminologi = presentation layer (`GET /properties/{id}/capabilities`, `/tenant/me`).
   Perubahan profile = aksi admin `POST /properties/{id}/profile` (permission `property.properties.change_profile`, reason wajib)
   dengan guard modul (hotel/commercial) → 409 `PROFILE_CHANGE_BLOCKED`. Housekeeping/Security/Engineering/Tenant Relation selalu aktif.
2. **TD-P1-002 Permission lama tidak di-rename.** `tenant.service_requests.*` tetap; objek baru memakai modul baru
   (`tenant_relation`, `tenant_app`, `booking`, `billing`, `vendor`, `inventory`, `hotel`, `commercial`, `reports`).
3. **TD-P1-003 Identitas tenant = `users` + `tenant_users` + `tenant_access`.** Login client `tenant_app` (refresh token di body),
   akun tenant tidak bisa masuk dashboard/staff app dan sebaliknya (403 `NOT_TENANT_ACCOUNT` / `TENANT_ACCOUNT_ONLY`), scope data
   dari `tenant_access` (unit/area) — bukan dari organization saja.
4. **TD-P1-004 Booking generik vs Reservation domain.** `bookings` = Facility Booking; `hotel_reservations`, `unit_rental_reservations`,
   `unit_sale_reservations` domain-specific. Konflik dicegah di DB (EXCLUDE gist tstzrange/daterange).
5. **TD-P1-005 HotelRoom = ekstensi 1:1 Unit** (`hotel_rooms.location_id` → `units`), sehingga SR/WO/cleaning task menunjuk lokasi yang sama;
   check-out → Dirty → turnover cleaning task → Clean → HK inspection → Available.
6. **TD-P1-006 Komunikasi tenant terpisah** (`service_request_messages`) dari `comments` internal; tenant tidak pernah menerima WO/Task,
   komentar staf, biaya, atau assignment terbatas.
7. **TD-P1-007 Payment provider abstraction.** `payment_providers` per org; `manual` (verifikasi staf) dan `mock_gateway` (HMAC-SHA256
   `X-BV-Signature`); status pembayaran hanya berubah lewat callback terverifikasi; kredensial kartu/rekening tidak disimpan.
8. **TD-P1-008 Tenant-facing status = presentation layer.** Status DB kanonik; mapping `service_request_tenant` di
   `contracts/status-map.yaml` (generated ke web & PWA) dan `tenantapp.TenantStatus`.
9. **TD-P1-009 Rental inquiry vs reservation.** `unit_rental_reservations.status='new'` (inquiry) tidak memblokir unit; `reserved`/`active`
   memblokir (EXCLUDE). Berbeda dari Hotel Reservation yang menahan kamar sejak `new`.
10. **TD-P1-010 Onboarding = Tenant/Occupant bersama.** Aktivasi sewa & serah terima unit membuat `tenants` + `occupants` + `unit_occupants`
    (+ akun Tenant App opsional, akses sampai akhir sewa); invoice reservasi hanya terlihat oleh tenant setelah ter-link `tenant_id`.
11. **TD-P1-011 Scope Mobile Tenant = PRD §7.** Halaman Figma di luar PRD (News, Property Listing, kategori tenant, wallet Tarik/Top Up/Kirim)
    dihapus; News → Pengumuman (Tenant Relation › Communication). Nav: Home | Requests | Facilities | Visitors | Bills (+ Inbox, Profile di header).
12. **TD-P1-012 Reports read-only di DB transaksional.** `internal/reports` menghitung KPI langsung (rentang ≤ 366 hari, zona waktu property);
    tanpa data warehouse — cukup untuk P1, dievaluasi bila volume > ~1 juta baris/org.

Konsekuensi: migrasi 00009–00015 backward-compatible (ADD COLUMN/CREATE TABLE); `contracts/permissions.yaml` & `status-map.yaml`
bertambah (CI byte-identik dengan salinan embedded); Web Push (VAPID) dan video pada ticket belum diimplementasikan (in-app + polling).
