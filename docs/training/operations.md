# Operations — Work Order & Task

- Status: New → Assigned → Scheduled/In Progress → On Hold → Completed → Closed (Cancelled dari status non-terminal). Tombol hanya muncul bila diizinkan server (`allowed_actions`).
- Buat Work Order: judul, tipe (maintenance/corrective/repair/service), lokasi (wajib), aset, prioritas (SLA mengikuti policy), jadwal/due, checklist template, wajib evidence, team/assignee.
- Tugaskan: team dan/atau user (user harus anggota team). Bulk assign: centang baris → Tugaskan.
- Detail: tab Detail (deskripsi, biaya/vendor untuk WO, komentar) · Checklist (supervisor boleh mengisi/koreksi; Not OK → Finding otomatis) · Evidence (Before/After, unggah dari web dikompresi ≤1600px) · Activity (timeline: sumber web/mobile/sync, alasan) · Terkait (link dua arah; Buat WO/Finding/Incident dari sini).
- Alasan wajib untuk Batalkan/Tunda/Buka Kembali; catatan opsional untuk Selesaikan.
- Evidence belum lengkap: banner kuning; Selesaikan ditolak sampai foto/checklist wajib terpenuhi.
- Filter: status, tipe, prioritas, lokasi (subtree), assignee, team, tanggal; preset Open/Overdue/SLA Risk/Milik tim saya/Hari ini. Export (xlsx) mengikuti filter; tautan unduh muncul di Inbox.
- Service Request: Baru → Terima (acknowledge = response SLA) → Tugaskan → Mulai → Menunggu Tenant → Resolve → Tutup. "Buat Work Order" dari SR → SR otomatis resolved saat WO ditutup.
- Incident: Report Incident (severity → prioritas default) → Tugaskan → Mulai → Resolve → Tutup; Buat WO dari Incident.
- Finding: dari patrol/inspeksi/checklist; Buat WO dari Finding / Buat Incident; Resolve/Tutup.
