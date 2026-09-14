# Engineering — PM, Aset, Inspeksi

- **Assets**: register (kode `AST-{CAT}-{SEQ}` otomatis), equipment (kategori/tipe), lokasi, status, kritikalitas; detail: riwayat WO/Task/PM, QR (cetak/rotate), Buat WO korektif.
- **Preventive Maintenance**: tab Plan → Buat Plan (aset, frekuensi, mulai, lead time, checklist, team) → Publikasikan (jadwal 60 hari dibuat). Tab Jadwal: due/overdue; WO dibuat otomatis oleh worker `lead_time_days` sebelum due (atau tombol "Buat WO jadwal due"); Lewati dengan alasan.
- **Corrective Maintenance**: daftar WO tipe corrective/repair. **Inspections**: task tipe inspection (nomor INS-), hasil pass/fail dari checklist; Not OK → Finding → WO.
- **Equipment**: master kategori (HVAC, ELEC, …) & tipe; kritikalitas default.
- Latihan: buat plan bulanan untuk Chiller-01 → publikasikan → jadwal muncul → run-due → WO PM muncul di Work Orders dan di riwayat aset.
