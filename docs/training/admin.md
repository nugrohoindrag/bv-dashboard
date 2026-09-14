# Org Admin — Settings

- **Organization**: nama, public intake per property (rotate key).
- **Users**: buat user (email atau username untuk petugas lapangan), role per property (kosong = semua), team, aktif/nonaktif, reset password.
- **Roles**: role sistem read-only (organization_admin, property_manager, building_manager, operations_manager, engineering_*, security_*, housekeeping_*, technician …); role kustom dengan matriks permission `modul.objek.aksi`.
- **Teams**: domain (engineering/security/housekeeping/management), property, anggota & lead (lead menerima notifikasi supervisor).
- **Checklists**: builder item (OK/NotOK/NA, Ya/Tidak, Angka dengan min/max, Teks, Foto), section, wajib/foto; Publikasikan (versi baru; run lama tetap memakai snapshot versinya); Arsipkan.
- **SLA Policies**: matriks object × prioritas; response/resolution menit, ambang risk %, kalender 24x7 / jam kerja; override per property.
- **Master Data**: kategori SR (seed/import), tautan equipment/lokasi.
- **Notifications**: inbox penuh & preferensi in-app/push per tipe.
- **Sync Conflicts**: mutasi offline yang konflik (C2–C5) — tinjau lalu "Tinjau" (acknowledge); tindak lanjut lewat aksi normal (reassign/complete on-behalf).
- **Audit Log**: siapa mengubah apa (before/after), filter entitas/aksi/aktor/tanggal.
- Data awal: `bvctl import --org <slug> --type locations|assets --file x.csv --dry-run` (OD-008).
