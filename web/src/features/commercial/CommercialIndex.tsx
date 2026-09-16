// /commercial: arahkan ke modul komersial sesuai profile property (Hotel → Hotel Booking; Apartment → Unit Sales; lainnya → info).
import { Navigate } from "react-router-dom";
import { Alert } from "@/components/ui/primitives";
import { useProfile } from "@/lib/profile";

export default function CommercialIndex() {
  const { has: hasCap } = useProfile();
  if (hasCap("hotel_booking")) return <Navigate to="/commercial/hotel/reservations" replace />;
  if (hasCap("unit_sales")) return <Navigate to="/commercial/sales/listings" replace />;
  if (hasCap("unit_rental")) return <Navigate to="/commercial/rental/listings" replace />;
  return <Alert variant="info" title="Commercial">Modul komersial (Hotel Booking / Unit Sales & Rental) hanya aktif pada profile Hotel atau Apartment. Ubah profile property di Settings.</Alert>;
}
