// Metadata tipe lokasi (PRD §8; PRD P0 v2 §7): path REST typed per level.
import type { Location } from "@/api/types";

export const TYPED_PATH: Record<Location["location_type"], string> = { property: "properties", building: "buildings", tower: "towers", floor: "floors", area: "areas", space: "spaces", unit: "units" };
