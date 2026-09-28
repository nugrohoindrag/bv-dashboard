// Teks UI fitur PRD P2 v2.1 (Security: Emergency, Parking, Lost & Found; incident lengkap; Workforce: dashboard domain,
// shift per domain, kompetensi, Cleaning Route, dokumen equipment). NC §8/§15–§17, §76.4: nama module & submodule.
// id = default, en = terjemahan.
export const p2v21Id: Record<string, string> = {
  "nav.emergency": "Emergency",
  "nav.parking": "Parking",
  "nav.lost_found": "Lost & Found",
  // aksi Emergency Alert di Attention Required (allowed_actions server)
  "action.respond": "Tiba / Tangani",
  // PRD P2 v2.1 §5.1/§6.1/§7.1 dashboard domain; §8 workforce; §7.3 Cleaning Route; §5.6 dokumen equipment
  "nav.dashboard": "Dashboard",
  "nav.asset_documents": "Dokumen Equipment",
  "nav.security_shifts": "Shift Management",
  "nav.housekeeping_shifts": "Shift",
  "nav.cleaning_routes": "Cleaning Route",
  "domain.workforce": "Workforce",
};

export const p2v21En: Record<string, string> = {
  "nav.emergency": "Emergency",
  "nav.parking": "Parking",
  "nav.lost_found": "Lost & Found",
  "action.respond": "Respond on site",
  "nav.dashboard": "Dashboard",
  "nav.asset_documents": "Equipment Documents",
  "nav.security_shifts": "Shift Management",
  "nav.housekeeping_shifts": "Shift",
  "nav.cleaning_routes": "Cleaning Route",
  "domain.workforce": "Workforce",
};
