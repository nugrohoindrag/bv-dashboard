# ADR-013 — Website publik, self-serve onboarding & free trial, App Downloads (Website PRD v1.1)

Status: **Diterima** · Tanggal: 2026-09-16 · Sumber: *BuildingVision Website Landing, Self-Serve Onboarding & Free Trial PRD v1.1*

Konteks: PRD website terpisah dari PRD P1 (§53). Website harus mengonsumsi public API, Dashboard mengelola konfigurasi administratif,
mobile tetap repo terpisah (§46). Keputusan user: website ikut di repo `buildingvision` (folder `website/`), bukan repo baru.

Keputusan:

1. **TD-WEB-001 Website = paket `website/` di monorepo, situs statis prerender.** Vite + React + Tailwind v4 (alias token DS), dua
   build (client + SSR) lalu `scripts/prerender.mjs` menulis HTML per route + `sitemap.xml`/`robots.txt` (SEO §39: title, description,
   canonical, OG, JSON-LD). Tidak ada SSR runtime. Website tidak memuat komponen dashboard; hanya token, font, dan logo dari `packages/ui`.
   Icon subset Material Symbols kini juga memindai `website/src` (`packages/ui` `fonts:vendor`).
2. **TD-WEB-002 Signup & onboarding berada di Dashboard (`web/`), bukan di website.** Website hanya mengarahkan `Start Free Trial` →
   `{APP_URL}/signup?source=…` dan `Login` → `{APP_URL}/login` (§25, §35). Alasan: sesi, cookie refresh, dan auth sudah ada di dashboard;
   setelah verifikasi email user langsung berada di dashboard `/onboarding`.
3. **TD-WEB-003 Akun dibuat saat verifikasi email, bukan saat signup.** `signups` (global, tanpa RLS) menyimpan hash password + token
   verifikasi (sha256, 24 jam, resend ≤ 5). `POST /public/signup/verify` membuat organization (trial 14 hari), seed baseline organization
   (role sistem, equipment, SLA, kategori SR, provider manual), user `organization_admin` (`email_verified_at`), sesi web (cookie refresh),
   lalu mengirim email Welcome + Trial started. Organization dibuat pada langkah "Create Organization" PRD dengan nama dari form signup;
   nama bisa diubah di wizard onboarding.
4. **TD-WEB-004 Profile tetap milik Property (§26–§27).** Wizard onboarding: Organization → pilih profile → buat property (`details.profile`,
   timezone, alamat) memakai endpoint property yang ada. Kontak disimpan di `organizations.settings.contact`.
5. **TD-WEB-005 Checklist onboarding dihitung dari data, bukan flag.** `GET /onboarding` menghitung 8 langkah dari tabel nyata (property,
   lokasi, staf, SR, WO/Task, selesai, attachment, tenant user) dengan label kontekstual per profile; aktivasi (§29) = property + SR + work +
   completed + evidence, dicatat sekali di `settings.onboarding.activated_at`. Sample data (§30) = `POST /onboarding/sample-data` menambah
   struktur + aset + WO/Task/SR berprefix `[Sample]` ke property pilihan, tanpa membuat user; sekali per organization.
6. **TD-WEB-006 Siklus trial di `organizations`** (`trial_status`, `trial_started_at/ends_at/converted_at/cancelled_at`, `plan_code`).
   Worker `trial.sweep` (1 jam): `trial_ending_soon` (≤ 3 hari), `trial_expired`, pengingat setup (48 jam tanpa property). Notifikasi in-app
   ke Organization Admin + email (§32). Expired/cancelled = **read-only**: middleware `growth.Guard` menolak mutasi dengan 402 `TRIAL_LOCKED`
   (GET tetap boleh; `/trial/*` dikecualikan) agar "Choose a plan" bisa dilakukan. Convert (§33) tanpa pembayaran online (di-hold): status
   `converted` + `plan_code`, email ke user & sales.
7. **TD-WEB-007 App Downloads global, role `admin_internal` pada organization `is_internal`.** Tabel `app_downloads` tanpa organization
   (konfigurasi platform). Role `admin_internal` hanya di-seed ke organization dengan `organizations.is_internal = true`
   (`bvctl seed --internal`); middleware `RequireInternalAdmin` memeriksa role **dan** flag organization di server (permission `*`
   organization_admin tidak cukup). Menu Settings › App Downloads hanya tampil bila `/me` `is_internal_admin`. URL harus https di
   `drive.google.com|docs.google.com|drive.usercontent.google.com` (§21); tidak ada DELETE (deaktivasi, §44); audit
   `APP_DOWNLOAD_CREATED|UPDATED|ACTIVATED|DEACTIVATED` dengan `previous_url`/`new_url` (§20). Public `GET /public/app-downloads`
   hanya tautan aktif + `Cache-Control: max-age=60`; website menampilkan fallback "currently unavailable" / pesan API gagal (§47).
8. **TD-WEB-008 Analytics funnel tanpa data pribadi** (§40): tabel `growth_events` (event whitelist, `anonymous_id` acak sisi klien,
   properti ≤ 4 KB). Website memakai `sendBeacon` ke `POST /public/events`; dashboard `POST /growth/events`; event server-side
   (`account_created`, `email_verified`, `trial_started`, `subscription_started`, `demo_requested`, …) dicatat oleh service.
9. **TD-WEB-009 Email transaksional lewat `platform/mailer`** (SMTP net/smtp, STARTTLS opsional; kosong = log-only, dev memakai Mailpit).
   Pada `BV_ENV=local` respons signup menyertakan `dev_verification_url` agar alur bisa diuji tanpa kotak masuk. Copy email/onboarding/trial
   berbahasa Inggris, tanpa em dash (§37–§38); website punya `check:copy` yang gagal bila ada em dash.
10. **TD-WEB-010 Book a Demo** = form website → `POST /public/demo-requests` (tabel `demo_requests`, rate-limit IP) + email ke sales dan
    konfirmasi ke pemohon (§34). Penjadwalan eksternal dapat ditambahkan tanpa mengubah kontrak.

Konsekuensi: satu API untuk tiga permukaan (website, dashboard, mobile) tetap terjaga; deploy website terpisah (image `bv-website`,
domain `BV_WEBSITE_DOMAIN`) namun tidak perlu redeploy saat tautan unduhan berubah. Migrasi 00016 backward-compatible.
Belum termasuk: pembayaran online (di-hold), penghapusan otomatis workspace expired (retensi manual), integrasi scheduler demo.
