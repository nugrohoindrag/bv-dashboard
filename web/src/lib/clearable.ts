// Konvensi PATCH server BuildingVision (PRD P2 v2.1): field yang tidak dikirim / null = tidak berubah; UUID nol
// mengosongkan referensi opsional (shift, team, assignee, file); string kosong mengosongkan teks & tanggal.
export const CLEAR_ID = "00000000-0000-0000-0000-000000000000";

/** Referensi opsional: nilai baru, UUID nol bila sebelumnya terisi lalu dikosongkan, selain itu null (tidak berubah). */
export function clearableId(value: string | null | undefined, previous: string | null | undefined): string | null {
  if (value) return value;
  return previous ? CLEAR_ID : null;
}

/** Teks/tanggal opsional: nilai (trim), "" bila sebelumnya terisi lalu dikosongkan, selain itu null (tidak berubah). */
export function clearableText(value: string | null | undefined, previous: string | null | undefined): string | null {
  const v = (value ?? "").trim();
  if (v) return v;
  return previous ? "" : null;
}
