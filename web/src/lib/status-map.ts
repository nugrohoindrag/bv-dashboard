// GENERATED — jangan edit manual. Sumber: contracts/status-map.yaml. Jalankan `npm run gen`.
export type Semantic = "success" | "warning" | "critical" | "info" | "neutral";
export type Variant = "solid" | "soft" | "outline";
export interface StatusDef { label_id: string; label_en: string; semantic: Semantic; variant: Variant; icon?: string }
export type ObjectType = "common" | "task" | "work_order" | "service_request" | "service_request_tenant" | "booking" | "bvrooms_booking_customer" | "bvrooms_payment" | "visitor" | "invoice" | "payment" | "maintenance_schedule" | "incident" | "finding" | "inspection" | "checkpoint" | "asset" | "authoring" | "flags" | "sla_status" | "facility" | "occupancy" | "priority" | "severity" | "sync_state" | "tenant_user" | "hotel_reservation" | "hotel_room" | "unit_listing" | "rental_listing" | "sales_lead" | "sale_reservation" | "rental_reservation" | "organization" | "location_status" | "export" | "emergency_alert" | "parking_violation" | "vehicle" | "lost_found_item" | "lost_report" | "investigation" | "roster_attendance" | "attendance" | "shift_handover" | "asset_health" | "validity" | "cleaning_route_run" | "announcement" | "package" | "parking_permit" | "tenant_feedback" | "recurring_issue" | "billing_run" | "meter_reading" | "credit_note" | "invoice_penalty" | "collection_promise" | "bank_statement_line" | "budget" | "webhook_delivery";
export const statusMap: Record<ObjectType, Record<string, StatusDef>> = {
  "common": {
    "new": {
      "label_id": "Baru",
      "label_en": "New",
      "semantic": "neutral",
      "variant": "soft",
      "icon": "circle"
    },
    "in_progress": {
      "label_id": "Sedang Dikerjakan",
      "label_en": "In Progress",
      "semantic": "info",
      "variant": "solid",
      "icon": "play"
    },
    "pending": {
      "label_id": "Pending",
      "label_en": "Pending",
      "semantic": "warning",
      "variant": "soft",
      "icon": "pause"
    },
    "completed": {
      "label_id": "Selesai",
      "label_en": "Completed",
      "semantic": "success",
      "variant": "soft",
      "icon": "check"
    },
    "closed": {
      "label_id": "Ditutup",
      "label_en": "Closed",
      "semantic": "success",
      "variant": "solid",
      "icon": "check-check"
    },
    "cancelled": {
      "label_id": "Dibatalkan",
      "label_en": "Cancelled",
      "semantic": "neutral",
      "variant": "outline",
      "icon": "x-circle"
    },
    "overdue": {
      "label_id": "Overdue",
      "label_en": "Overdue",
      "semantic": "critical",
      "variant": "solid",
      "icon": "alarm-clock"
    },
    "critical": {
      "label_id": "Kritis",
      "label_en": "Critical",
      "semantic": "critical",
      "variant": "solid",
      "icon": "alert-triangle"
    }
  },
  "task": {
    "new": {
      "label_id": "Baru",
      "label_en": "New",
      "semantic": "neutral",
      "variant": "soft"
    },
    "scheduled": {
      "label_id": "Terjadwal",
      "label_en": "Scheduled",
      "semantic": "info",
      "variant": "soft"
    },
    "assigned": {
      "label_id": "Ditugaskan",
      "label_en": "Assigned",
      "semantic": "info",
      "variant": "soft"
    },
    "in_progress": {
      "label_id": "Sedang Dikerjakan",
      "label_en": "In Progress",
      "semantic": "info",
      "variant": "solid"
    },
    "on_hold": {
      "label_id": "Pending",
      "label_en": "Pending",
      "semantic": "warning",
      "variant": "soft"
    },
    "completed": {
      "label_id": "Selesai",
      "label_en": "Completed",
      "semantic": "success",
      "variant": "soft"
    },
    "closed": {
      "label_id": "Ditutup",
      "label_en": "Closed",
      "semantic": "success",
      "variant": "solid"
    },
    "cancelled": {
      "label_id": "Dibatalkan",
      "label_en": "Cancelled",
      "semantic": "neutral",
      "variant": "outline"
    }
  },
  "work_order": {
    "draft": {
      "label_id": "Draft",
      "label_en": "Draft",
      "semantic": "neutral",
      "variant": "outline"
    },
    "new": {
      "label_id": "Terbuka",
      "label_en": "Open",
      "semantic": "neutral",
      "variant": "soft"
    },
    "scheduled": {
      "label_id": "Terjadwal",
      "label_en": "Scheduled",
      "semantic": "info",
      "variant": "soft"
    },
    "assigned": {
      "label_id": "Ditugaskan",
      "label_en": "Assigned",
      "semantic": "info",
      "variant": "soft"
    },
    "in_progress": {
      "label_id": "Sedang Dikerjakan",
      "label_en": "In Progress",
      "semantic": "info",
      "variant": "solid"
    },
    "on_hold": {
      "label_id": "Pending",
      "label_en": "Pending",
      "semantic": "warning",
      "variant": "soft"
    },
    "completed": {
      "label_id": "Selesai",
      "label_en": "Completed",
      "semantic": "success",
      "variant": "soft"
    },
    "closed": {
      "label_id": "Ditutup",
      "label_en": "Closed",
      "semantic": "success",
      "variant": "solid"
    },
    "cancelled": {
      "label_id": "Dibatalkan",
      "label_en": "Cancelled",
      "semantic": "neutral",
      "variant": "outline"
    }
  },
  "service_request": {
    "new": {
      "label_id": "Baru",
      "label_en": "New",
      "semantic": "neutral",
      "variant": "soft"
    },
    "acknowledged": {
      "label_id": "Ditriase",
      "label_en": "Triaged",
      "semantic": "info",
      "variant": "soft"
    },
    "assigned": {
      "label_id": "Ditugaskan",
      "label_en": "Assigned",
      "semantic": "info",
      "variant": "soft"
    },
    "in_progress": {
      "label_id": "Sedang Dikerjakan",
      "label_en": "In Progress",
      "semantic": "info",
      "variant": "solid"
    },
    "waiting_for_tenant": {
      "label_id": "Menunggu Tenant",
      "label_en": "Waiting for Tenant",
      "semantic": "warning",
      "variant": "soft"
    },
    "resolved": {
      "label_id": "Terselesaikan",
      "label_en": "Resolved",
      "semantic": "success",
      "variant": "soft"
    },
    "closed": {
      "label_id": "Ditutup",
      "label_en": "Closed",
      "semantic": "success",
      "variant": "solid"
    },
    "cancelled": {
      "label_id": "Dibatalkan",
      "label_en": "Cancelled",
      "semantic": "neutral",
      "variant": "outline"
    }
  },
  "service_request_tenant": {
    "submitted": {
      "label_id": "Terkirim",
      "label_en": "Submitted",
      "semantic": "neutral",
      "variant": "soft"
    },
    "received": {
      "label_id": "Diterima",
      "label_en": "Received",
      "semantic": "info",
      "variant": "soft"
    },
    "being_assigned": {
      "label_id": "Sedang Ditugaskan",
      "label_en": "Being Assigned",
      "semantic": "info",
      "variant": "soft"
    },
    "in_progress": {
      "label_id": "Sedang Dikerjakan",
      "label_en": "In Progress",
      "semantic": "info",
      "variant": "solid"
    },
    "need_your_response": {
      "label_id": "Butuh Respons Anda",
      "label_en": "Need Your Response",
      "semantic": "warning",
      "variant": "solid"
    },
    "resolved": {
      "label_id": "Selesai",
      "label_en": "Resolved",
      "semantic": "success",
      "variant": "soft"
    },
    "closed": {
      "label_id": "Ditutup",
      "label_en": "Closed",
      "semantic": "success",
      "variant": "solid"
    },
    "cancelled": {
      "label_id": "Dibatalkan",
      "label_en": "Cancelled",
      "semantic": "neutral",
      "variant": "outline"
    }
  },
  "booking": {
    "pending": {
      "label_id": "Menunggu Persetujuan",
      "label_en": "Pending",
      "semantic": "warning",
      "variant": "soft"
    },
    "confirmed": {
      "label_id": "Dikonfirmasi",
      "label_en": "Confirmed",
      "semantic": "info",
      "variant": "soft"
    },
    "checked_in": {
      "label_id": "Berlangsung",
      "label_en": "Checked In",
      "semantic": "info",
      "variant": "solid"
    },
    "completed": {
      "label_id": "Selesai",
      "label_en": "Completed",
      "semantic": "success",
      "variant": "soft"
    },
    "cancelled": {
      "label_id": "Dibatalkan",
      "label_en": "Cancelled",
      "semantic": "neutral",
      "variant": "outline"
    },
    "rejected": {
      "label_id": "Ditolak",
      "label_en": "Rejected",
      "semantic": "critical",
      "variant": "soft"
    },
    "no_show": {
      "label_id": "Tidak Hadir",
      "label_en": "No Show",
      "semantic": "critical",
      "variant": "outline"
    }
  },
  "bvrooms_booking_customer": {
    "UNPAID": {
      "label_id": "Belum Dibayar",
      "label_en": "Unpaid",
      "semantic": "critical",
      "variant": "solid"
    },
    "PAID": {
      "label_id": "Sudah Dibayar",
      "label_en": "Paid",
      "semantic": "success",
      "variant": "solid"
    },
    "CHECK IN": {
      "label_id": "Check In",
      "label_en": "Checked In",
      "semantic": "warning",
      "variant": "solid"
    },
    "CHECK OUT": {
      "label_id": "Check Out",
      "label_en": "Checked Out",
      "semantic": "neutral",
      "variant": "solid"
    },
    "CANCELLED": {
      "label_id": "Dibatalkan",
      "label_en": "Cancelled",
      "semantic": "neutral",
      "variant": "solid"
    },
    "EXPIRED": {
      "label_id": "Hangus",
      "label_en": "Expired",
      "semantic": "neutral",
      "variant": "outline"
    }
  },
  "bvrooms_payment": {
    "pending": {
      "label_id": "Menunggu Pembayaran",
      "label_en": "Pending",
      "semantic": "warning",
      "variant": "soft"
    },
    "proof_submitted": {
      "label_id": "Bukti Diverifikasi",
      "label_en": "Proof Submitted",
      "semantic": "info",
      "variant": "soft"
    },
    "paid": {
      "label_id": "Lunas",
      "label_en": "Paid",
      "semantic": "success",
      "variant": "soft"
    },
    "expired": {
      "label_id": "Kedaluwarsa",
      "label_en": "Expired",
      "semantic": "neutral",
      "variant": "outline"
    },
    "failed": {
      "label_id": "Ditolak",
      "label_en": "Rejected",
      "semantic": "critical",
      "variant": "soft"
    },
    "cancelled": {
      "label_id": "Dibatalkan",
      "label_en": "Cancelled",
      "semantic": "neutral",
      "variant": "outline"
    }
  },
  "visitor": {
    "pending_approval": {
      "label_id": "Menunggu Persetujuan",
      "label_en": "Pending Approval",
      "semantic": "warning",
      "variant": "soft"
    },
    "registered": {
      "label_id": "Terdaftar",
      "label_en": "Registered",
      "semantic": "info",
      "variant": "soft"
    },
    "checked_in": {
      "label_id": "Sudah Masuk",
      "label_en": "Checked In",
      "semantic": "info",
      "variant": "solid"
    },
    "checked_out": {
      "label_id": "Sudah Keluar",
      "label_en": "Checked Out",
      "semantic": "success",
      "variant": "soft"
    },
    "expired": {
      "label_id": "Kedaluwarsa",
      "label_en": "Expired",
      "semantic": "neutral",
      "variant": "outline"
    },
    "cancelled": {
      "label_id": "Dibatalkan",
      "label_en": "Cancelled",
      "semantic": "neutral",
      "variant": "outline"
    },
    "denied": {
      "label_id": "Ditolak",
      "label_en": "Denied",
      "semantic": "critical",
      "variant": "soft"
    }
  },
  "invoice": {
    "draft": {
      "label_id": "Draft",
      "label_en": "Draft",
      "semantic": "neutral",
      "variant": "outline"
    },
    "issued": {
      "label_id": "Belum Dibayar",
      "label_en": "Unpaid",
      "semantic": "warning",
      "variant": "soft"
    },
    "partially_paid": {
      "label_id": "Dibayar Sebagian",
      "label_en": "Partially Paid",
      "semantic": "warning",
      "variant": "solid"
    },
    "paid": {
      "label_id": "Lunas",
      "label_en": "Paid",
      "semantic": "success",
      "variant": "solid"
    },
    "overdue": {
      "label_id": "Jatuh Tempo",
      "label_en": "Overdue",
      "semantic": "critical",
      "variant": "solid"
    },
    "cancelled": {
      "label_id": "Dibatalkan",
      "label_en": "Cancelled",
      "semantic": "neutral",
      "variant": "outline"
    }
  },
  "payment": {
    "initiated": {
      "label_id": "Menunggu Pembayaran",
      "label_en": "Pending",
      "semantic": "warning",
      "variant": "soft"
    },
    "pending": {
      "label_id": "Diproses",
      "label_en": "Processing",
      "semantic": "info",
      "variant": "soft"
    },
    "paid": {
      "label_id": "Berhasil",
      "label_en": "Paid",
      "semantic": "success",
      "variant": "solid"
    },
    "failed": {
      "label_id": "Gagal",
      "label_en": "Failed",
      "semantic": "critical",
      "variant": "soft"
    },
    "expired": {
      "label_id": "Kedaluwarsa",
      "label_en": "Expired",
      "semantic": "neutral",
      "variant": "outline"
    },
    "cancelled": {
      "label_id": "Dibatalkan",
      "label_en": "Cancelled",
      "semantic": "neutral",
      "variant": "outline"
    },
    "refunded": {
      "label_id": "Dikembalikan",
      "label_en": "Refunded",
      "semantic": "neutral",
      "variant": "soft"
    }
  },
  "maintenance_schedule": {
    "scheduled": {
      "label_id": "Terjadwal",
      "label_en": "Scheduled",
      "semantic": "info",
      "variant": "soft"
    },
    "due": {
      "label_id": "Jatuh Tempo",
      "label_en": "Due",
      "semantic": "warning",
      "variant": "soft"
    },
    "in_progress": {
      "label_id": "Sedang Dikerjakan",
      "label_en": "In Progress",
      "semantic": "info",
      "variant": "solid"
    },
    "completed": {
      "label_id": "Selesai",
      "label_en": "Completed",
      "semantic": "success",
      "variant": "soft"
    },
    "overdue": {
      "label_id": "Overdue",
      "label_en": "Overdue",
      "semantic": "critical",
      "variant": "solid"
    },
    "skipped": {
      "label_id": "Dilewati",
      "label_en": "Skipped",
      "semantic": "neutral",
      "variant": "outline"
    },
    "cancelled": {
      "label_id": "Dibatalkan",
      "label_en": "Cancelled",
      "semantic": "neutral",
      "variant": "outline"
    }
  },
  "incident": {
    "new": {
      "label_id": "Baru",
      "label_en": "New",
      "semantic": "neutral",
      "variant": "soft"
    },
    "assigned": {
      "label_id": "Ditugaskan",
      "label_en": "Assigned",
      "semantic": "info",
      "variant": "soft"
    },
    "in_progress": {
      "label_id": "Sedang Ditangani",
      "label_en": "In Progress",
      "semantic": "info",
      "variant": "solid"
    },
    "resolved": {
      "label_id": "Terselesaikan",
      "label_en": "Resolved",
      "semantic": "success",
      "variant": "soft"
    },
    "closed": {
      "label_id": "Ditutup",
      "label_en": "Closed",
      "semantic": "success",
      "variant": "solid"
    },
    "cancelled": {
      "label_id": "Dibatalkan",
      "label_en": "Cancelled",
      "semantic": "neutral",
      "variant": "outline"
    }
  },
  "finding": {
    "open": {
      "label_id": "Terbuka",
      "label_en": "Open",
      "semantic": "warning",
      "variant": "soft"
    },
    "in_progress": {
      "label_id": "Sedang Ditangani",
      "label_en": "In Progress",
      "semantic": "info",
      "variant": "soft"
    },
    "resolved": {
      "label_id": "Terselesaikan",
      "label_en": "Resolved",
      "semantic": "success",
      "variant": "soft"
    },
    "closed": {
      "label_id": "Ditutup",
      "label_en": "Closed",
      "semantic": "success",
      "variant": "solid"
    }
  },
  "inspection": {
    "new": {
      "label_id": "Baru",
      "label_en": "New",
      "semantic": "neutral",
      "variant": "soft"
    },
    "scheduled": {
      "label_id": "Terjadwal",
      "label_en": "Scheduled",
      "semantic": "info",
      "variant": "soft"
    },
    "assigned": {
      "label_id": "Ditugaskan",
      "label_en": "Assigned",
      "semantic": "info",
      "variant": "soft"
    },
    "in_progress": {
      "label_id": "Sedang Dikerjakan",
      "label_en": "In Progress",
      "semantic": "info",
      "variant": "solid"
    },
    "completed": {
      "label_id": "Selesai",
      "label_en": "Completed",
      "semantic": "success",
      "variant": "soft"
    },
    "closed": {
      "label_id": "Ditutup",
      "label_en": "Closed",
      "semantic": "success",
      "variant": "solid"
    },
    "cancelled": {
      "label_id": "Dibatalkan",
      "label_en": "Cancelled",
      "semantic": "neutral",
      "variant": "outline"
    }
  },
  "checkpoint": {
    "pending": {
      "label_id": "Menunggu",
      "label_en": "Pending",
      "semantic": "neutral",
      "variant": "soft"
    },
    "scanned": {
      "label_id": "Terverifikasi",
      "label_en": "Scanned",
      "semantic": "success",
      "variant": "soft"
    },
    "missed": {
      "label_id": "Terlewat",
      "label_en": "Missed",
      "semantic": "critical",
      "variant": "solid"
    }
  },
  "asset": {
    "active": {
      "label_id": "Aktif",
      "label_en": "Active",
      "semantic": "success",
      "variant": "soft"
    },
    "inactive": {
      "label_id": "Tidak Aktif",
      "label_en": "Inactive",
      "semantic": "neutral",
      "variant": "soft"
    },
    "under_maintenance": {
      "label_id": "Dalam Perawatan",
      "label_en": "Under Maintenance",
      "semantic": "warning",
      "variant": "soft"
    },
    "decommissioned": {
      "label_id": "Dinonaktifkan",
      "label_en": "Decommissioned",
      "semantic": "neutral",
      "variant": "outline"
    }
  },
  "authoring": {
    "draft": {
      "label_id": "Draft",
      "label_en": "Draft",
      "semantic": "neutral",
      "variant": "soft"
    },
    "published": {
      "label_id": "Dipublikasikan",
      "label_en": "Published",
      "semantic": "success",
      "variant": "soft"
    },
    "archived": {
      "label_id": "Diarsipkan",
      "label_en": "Archived",
      "semantic": "neutral",
      "variant": "outline"
    }
  },
  "flags": {
    "overdue": {
      "label_id": "Overdue",
      "label_en": "Overdue",
      "semantic": "critical",
      "variant": "solid",
      "icon": "alarm-clock"
    },
    "sla_risk": {
      "label_id": "SLA Risk",
      "label_en": "SLA Risk",
      "semantic": "warning",
      "variant": "soft",
      "icon": "timer"
    },
    "sla_breach": {
      "label_id": "SLA Breach",
      "label_en": "SLA Breach",
      "semantic": "critical",
      "variant": "solid",
      "icon": "timer-off"
    },
    "evidence_incomplete": {
      "label_id": "Evidence belum lengkap",
      "label_en": "Evidence incomplete",
      "semantic": "warning",
      "variant": "soft",
      "icon": "image-off"
    },
    "reopened": {
      "label_id": "Dibuka Kembali",
      "label_en": "Reopened",
      "semantic": "warning",
      "variant": "soft",
      "icon": "rotate-ccw"
    },
    "critical": {
      "label_id": "Kritis",
      "label_en": "Critical",
      "semantic": "critical",
      "variant": "solid",
      "icon": "alert-triangle"
    },
    "escalated": {
      "label_id": "Dieskalasi",
      "label_en": "Escalated",
      "semantic": "warning",
      "variant": "solid",
      "icon": "arrow-up"
    }
  },
  "sla_status": {
    "on_track": {
      "label_id": "Sesuai SLA",
      "label_en": "On Track",
      "semantic": "success",
      "variant": "soft",
      "icon": "timer"
    },
    "at_risk": {
      "label_id": "Berisiko",
      "label_en": "At Risk",
      "semantic": "warning",
      "variant": "soft",
      "icon": "timer"
    },
    "breached": {
      "label_id": "Melewati SLA",
      "label_en": "Breached",
      "semantic": "critical",
      "variant": "solid",
      "icon": "timer-off"
    },
    "completed": {
      "label_id": "Selesai",
      "label_en": "Completed",
      "semantic": "neutral",
      "variant": "soft",
      "icon": "check"
    }
  },
  "facility": {
    "operational": {
      "label_id": "Beroperasi",
      "label_en": "Operational",
      "semantic": "success",
      "variant": "soft"
    },
    "under_maintenance": {
      "label_id": "Dalam Perawatan",
      "label_en": "Under Maintenance",
      "semantic": "warning",
      "variant": "soft"
    },
    "closed": {
      "label_id": "Ditutup",
      "label_en": "Closed",
      "semantic": "neutral",
      "variant": "soft"
    },
    "inactive": {
      "label_id": "Tidak Aktif",
      "label_en": "Inactive",
      "semantic": "neutral",
      "variant": "outline"
    }
  },
  "occupancy": {
    "vacant": {
      "label_id": "Kosong",
      "label_en": "Vacant",
      "semantic": "info",
      "variant": "soft"
    },
    "occupied": {
      "label_id": "Terisi",
      "label_en": "Occupied",
      "semantic": "success",
      "variant": "soft"
    },
    "reserved": {
      "label_id": "Dipesan",
      "label_en": "Reserved",
      "semantic": "warning",
      "variant": "soft"
    },
    "inactive": {
      "label_id": "Tidak Aktif",
      "label_en": "Inactive",
      "semantic": "neutral",
      "variant": "outline"
    }
  },
  "priority": {
    "low": {
      "label_id": "Rendah",
      "label_en": "Low",
      "semantic": "neutral",
      "variant": "soft",
      "icon": "arrow-down"
    },
    "medium": {
      "label_id": "Sedang",
      "label_en": "Medium",
      "semantic": "info",
      "variant": "soft",
      "icon": "minus"
    },
    "high": {
      "label_id": "Tinggi",
      "label_en": "High",
      "semantic": "warning",
      "variant": "soft",
      "icon": "arrow-up"
    },
    "critical": {
      "label_id": "Kritis",
      "label_en": "Critical",
      "semantic": "critical",
      "variant": "solid",
      "icon": "alert-triangle"
    }
  },
  "severity": {
    "low": {
      "label_id": "Rendah",
      "label_en": "Low",
      "semantic": "neutral",
      "variant": "soft",
      "icon": "arrow-down"
    },
    "medium": {
      "label_id": "Sedang",
      "label_en": "Medium",
      "semantic": "info",
      "variant": "soft",
      "icon": "minus"
    },
    "high": {
      "label_id": "Tinggi",
      "label_en": "High",
      "semantic": "warning",
      "variant": "soft",
      "icon": "arrow-up"
    },
    "critical": {
      "label_id": "Kritis",
      "label_en": "Critical",
      "semantic": "critical",
      "variant": "solid",
      "icon": "alert-triangle"
    }
  },
  "sync_state": {
    "pending": {
      "label_id": "Pending Sync",
      "label_en": "Pending Sync",
      "semantic": "warning",
      "variant": "soft",
      "icon": "cloud-upload"
    },
    "synced": {
      "label_id": "Synced",
      "label_en": "Synced",
      "semantic": "success",
      "variant": "soft",
      "icon": "cloud-check"
    },
    "failed": {
      "label_id": "Sync Failed",
      "label_en": "Sync Failed",
      "semantic": "critical",
      "variant": "soft",
      "icon": "cloud-off"
    },
    "conflict": {
      "label_id": "Sync Conflict",
      "label_en": "Sync Conflict",
      "semantic": "warning",
      "variant": "soft",
      "icon": "git-merge"
    }
  },
  "tenant_user": {
    "pending_validation": {
      "label_id": "Menunggu Validasi",
      "label_en": "Pending Validation",
      "semantic": "warning",
      "variant": "soft"
    },
    "active": {
      "label_id": "Aktif",
      "label_en": "Active",
      "semantic": "success",
      "variant": "soft"
    },
    "rejected": {
      "label_id": "Ditolak",
      "label_en": "Rejected",
      "semantic": "critical",
      "variant": "soft"
    },
    "suspended": {
      "label_id": "Ditangguhkan",
      "label_en": "Suspended",
      "semantic": "neutral",
      "variant": "outline"
    }
  },
  "hotel_reservation": {
    "new": {
      "label_id": "Baru",
      "label_en": "New",
      "semantic": "neutral",
      "variant": "soft"
    },
    "confirmed": {
      "label_id": "Dikonfirmasi",
      "label_en": "Confirmed",
      "semantic": "info",
      "variant": "soft"
    },
    "checked_in": {
      "label_id": "Check-in",
      "label_en": "Checked In",
      "semantic": "info",
      "variant": "solid"
    },
    "checked_out": {
      "label_id": "Check-out",
      "label_en": "Checked Out",
      "semantic": "success",
      "variant": "soft"
    },
    "cancelled": {
      "label_id": "Dibatalkan",
      "label_en": "Cancelled",
      "semantic": "neutral",
      "variant": "outline"
    },
    "no_show": {
      "label_id": "Tidak Hadir",
      "label_en": "No Show",
      "semantic": "critical",
      "variant": "soft"
    }
  },
  "hotel_room": {
    "available": {
      "label_id": "Tersedia",
      "label_en": "Available",
      "semantic": "success",
      "variant": "soft"
    },
    "occupied": {
      "label_id": "Terisi",
      "label_en": "Occupied",
      "semantic": "info",
      "variant": "solid"
    },
    "dirty": {
      "label_id": "Kotor",
      "label_en": "Dirty",
      "semantic": "warning",
      "variant": "soft"
    },
    "clean": {
      "label_id": "Bersih",
      "label_en": "Clean",
      "semantic": "info",
      "variant": "soft"
    },
    "inspected": {
      "label_id": "Terinspeksi",
      "label_en": "Inspected",
      "semantic": "success",
      "variant": "solid"
    },
    "out_of_order": {
      "label_id": "Out of Order",
      "label_en": "Out of Order",
      "semantic": "critical",
      "variant": "soft"
    },
    "out_of_service": {
      "label_id": "Out of Service",
      "label_en": "Out of Service",
      "semantic": "neutral",
      "variant": "outline"
    }
  },
  "unit_listing": {
    "draft": {
      "label_id": "Draft",
      "label_en": "Draft",
      "semantic": "neutral",
      "variant": "outline"
    },
    "published": {
      "label_id": "Tersedia",
      "label_en": "Available",
      "semantic": "success",
      "variant": "soft"
    },
    "reserved": {
      "label_id": "Direservasi",
      "label_en": "Reserved",
      "semantic": "warning",
      "variant": "soft"
    },
    "sold": {
      "label_id": "Terjual",
      "label_en": "Sold",
      "semantic": "info",
      "variant": "solid"
    },
    "archived": {
      "label_id": "Diarsipkan",
      "label_en": "Archived",
      "semantic": "neutral",
      "variant": "outline"
    }
  },
  "rental_listing": {
    "draft": {
      "label_id": "Draft",
      "label_en": "Draft",
      "semantic": "neutral",
      "variant": "outline"
    },
    "published": {
      "label_id": "Dipublikasikan",
      "label_en": "Published",
      "semantic": "success",
      "variant": "soft"
    },
    "archived": {
      "label_id": "Diarsipkan",
      "label_en": "Archived",
      "semantic": "neutral",
      "variant": "outline"
    }
  },
  "sales_lead": {
    "new": {
      "label_id": "Baru",
      "label_en": "New",
      "semantic": "neutral",
      "variant": "soft"
    },
    "contacted": {
      "label_id": "Dihubungi",
      "label_en": "Contacted",
      "semantic": "info",
      "variant": "soft"
    },
    "qualified": {
      "label_id": "Qualified",
      "label_en": "Qualified",
      "semantic": "info",
      "variant": "solid"
    },
    "reserved": {
      "label_id": "Direservasi",
      "label_en": "Reserved",
      "semantic": "warning",
      "variant": "soft"
    },
    "sold": {
      "label_id": "Terjual",
      "label_en": "Sold",
      "semantic": "success",
      "variant": "solid"
    },
    "lost": {
      "label_id": "Lost",
      "label_en": "Lost",
      "semantic": "critical",
      "variant": "soft"
    },
    "cancelled": {
      "label_id": "Dibatalkan",
      "label_en": "Cancelled",
      "semantic": "neutral",
      "variant": "outline"
    }
  },
  "sale_reservation": {
    "reserved": {
      "label_id": "Direservasi",
      "label_en": "Reserved",
      "semantic": "warning",
      "variant": "soft"
    },
    "contract_signed": {
      "label_id": "Kontrak Ditandatangani",
      "label_en": "Contract Signed",
      "semantic": "info",
      "variant": "soft"
    },
    "sold": {
      "label_id": "Terjual",
      "label_en": "Sold",
      "semantic": "info",
      "variant": "solid"
    },
    "handed_over": {
      "label_id": "Serah Terima",
      "label_en": "Handed Over",
      "semantic": "success",
      "variant": "solid"
    },
    "cancelled": {
      "label_id": "Dibatalkan",
      "label_en": "Cancelled",
      "semantic": "neutral",
      "variant": "outline"
    }
  },
  "rental_reservation": {
    "new": {
      "label_id": "Baru (inquiry)",
      "label_en": "New (inquiry)",
      "semantic": "neutral",
      "variant": "soft"
    },
    "reserved": {
      "label_id": "Direservasi",
      "label_en": "Reserved",
      "semantic": "warning",
      "variant": "soft"
    },
    "active": {
      "label_id": "Aktif",
      "label_en": "Active",
      "semantic": "info",
      "variant": "solid"
    },
    "completed": {
      "label_id": "Selesai",
      "label_en": "Completed",
      "semantic": "success",
      "variant": "soft"
    },
    "cancelled": {
      "label_id": "Dibatalkan",
      "label_en": "Cancelled",
      "semantic": "neutral",
      "variant": "outline"
    }
  },
  "organization": {
    "active": {
      "label_id": "Aktif",
      "label_en": "Active",
      "semantic": "success",
      "variant": "soft"
    },
    "inactive": {
      "label_id": "Nonaktif",
      "label_en": "Inactive",
      "semantic": "neutral",
      "variant": "outline"
    },
    "suspended": {
      "label_id": "Ditangguhkan",
      "label_en": "Suspended",
      "semantic": "critical",
      "variant": "soft"
    }
  },
  "location_status": {
    "draft": {
      "label_id": "Draft",
      "label_en": "Draft",
      "semantic": "neutral",
      "variant": "outline"
    },
    "active": {
      "label_id": "Aktif",
      "label_en": "Active",
      "semantic": "success",
      "variant": "soft"
    },
    "inactive": {
      "label_id": "Nonaktif",
      "label_en": "Inactive",
      "semantic": "neutral",
      "variant": "soft"
    }
  },
  "export": {
    "pending": {
      "label_id": "Menunggu",
      "label_en": "Pending",
      "semantic": "neutral",
      "variant": "soft"
    },
    "processing": {
      "label_id": "Diproses",
      "label_en": "Processing",
      "semantic": "info",
      "variant": "soft"
    },
    "ready": {
      "label_id": "Siap",
      "label_en": "Ready",
      "semantic": "success",
      "variant": "soft"
    },
    "failed": {
      "label_id": "Gagal",
      "label_en": "Failed",
      "semantic": "critical",
      "variant": "soft"
    },
    "expired": {
      "label_id": "Kedaluwarsa",
      "label_en": "Expired",
      "semantic": "neutral",
      "variant": "outline"
    }
  },
  "emergency_alert": {
    "raised": {
      "label_id": "Dilaporkan",
      "label_en": "Raised",
      "semantic": "critical",
      "variant": "solid"
    },
    "acknowledged": {
      "label_id": "Diterima",
      "label_en": "Acknowledged",
      "semantic": "warning",
      "variant": "solid"
    },
    "responding": {
      "label_id": "Ditangani",
      "label_en": "Responding",
      "semantic": "info",
      "variant": "solid"
    },
    "resolved": {
      "label_id": "Selesai",
      "label_en": "Resolved",
      "semantic": "success",
      "variant": "soft"
    },
    "cancelled": {
      "label_id": "Dibatalkan",
      "label_en": "Cancelled",
      "semantic": "neutral",
      "variant": "outline"
    }
  },
  "parking_violation": {
    "open": {
      "label_id": "Terbuka",
      "label_en": "Open",
      "semantic": "warning",
      "variant": "soft"
    },
    "escalated": {
      "label_id": "Jadi Incident",
      "label_en": "Escalated",
      "semantic": "info",
      "variant": "soft"
    },
    "resolved": {
      "label_id": "Selesai",
      "label_en": "Resolved",
      "semantic": "success",
      "variant": "soft"
    }
  },
  "vehicle": {
    "active": {
      "label_id": "Aktif",
      "label_en": "Active",
      "semantic": "success",
      "variant": "soft"
    },
    "inactive": {
      "label_id": "Nonaktif",
      "label_en": "Inactive",
      "semantic": "neutral",
      "variant": "outline"
    },
    "blacklisted": {
      "label_id": "Blacklist",
      "label_en": "Blacklisted",
      "semantic": "critical",
      "variant": "solid"
    }
  },
  "lost_found_item": {
    "stored": {
      "label_id": "Disimpan",
      "label_en": "Stored",
      "semantic": "info",
      "variant": "soft"
    },
    "returned": {
      "label_id": "Dikembalikan",
      "label_en": "Returned",
      "semantic": "success",
      "variant": "soft"
    },
    "disposed": {
      "label_id": "Disposal",
      "label_en": "Disposed",
      "semantic": "neutral",
      "variant": "outline"
    }
  },
  "lost_report": {
    "open": {
      "label_id": "Terbuka",
      "label_en": "Open",
      "semantic": "warning",
      "variant": "soft"
    },
    "matched": {
      "label_id": "Cocok",
      "label_en": "Matched",
      "semantic": "info",
      "variant": "soft"
    },
    "closed": {
      "label_id": "Selesai",
      "label_en": "Closed",
      "semantic": "success",
      "variant": "soft"
    },
    "cancelled": {
      "label_id": "Dibatalkan",
      "label_en": "Cancelled",
      "semantic": "neutral",
      "variant": "outline"
    }
  },
  "investigation": {
    "not_started": {
      "label_id": "Belum dimulai",
      "label_en": "Not started",
      "semantic": "neutral",
      "variant": "soft"
    },
    "in_progress": {
      "label_id": "Berjalan",
      "label_en": "In progress",
      "semantic": "info",
      "variant": "soft"
    },
    "completed": {
      "label_id": "Selesai",
      "label_en": "Completed",
      "semantic": "success",
      "variant": "soft"
    }
  },
  "roster_attendance": {
    "upcoming": {
      "label_id": "Akan datang",
      "label_en": "Upcoming",
      "semantic": "neutral",
      "variant": "soft"
    },
    "on_duty": {
      "label_id": "On duty",
      "label_en": "On duty",
      "semantic": "success",
      "variant": "solid"
    },
    "late": {
      "label_id": "Terlambat",
      "label_en": "Late",
      "semantic": "warning",
      "variant": "soft"
    },
    "completed": {
      "label_id": "Selesai",
      "label_en": "Completed",
      "semantic": "success",
      "variant": "soft"
    },
    "absent": {
      "label_id": "Tidak hadir",
      "label_en": "Absent",
      "semantic": "critical",
      "variant": "soft"
    }
  },
  "attendance": {
    "on_duty": {
      "label_id": "On duty",
      "label_en": "On duty",
      "semantic": "success",
      "variant": "solid"
    },
    "completed": {
      "label_id": "Selesai",
      "label_en": "Completed",
      "semantic": "neutral",
      "variant": "soft"
    },
    "auto_closed": {
      "label_id": "Ditutup otomatis",
      "label_en": "Auto closed",
      "semantic": "warning",
      "variant": "soft"
    }
  },
  "shift_handover": {
    "submitted": {
      "label_id": "Menunggu diterima",
      "label_en": "Submitted",
      "semantic": "warning",
      "variant": "soft"
    },
    "acknowledged": {
      "label_id": "Diterima",
      "label_en": "Acknowledged",
      "semantic": "success",
      "variant": "soft"
    }
  },
  "asset_health": {
    "healthy": {
      "label_id": "Sehat",
      "label_en": "Healthy",
      "semantic": "success",
      "variant": "soft"
    },
    "warning": {
      "label_id": "Waspada",
      "label_en": "Warning",
      "semantic": "warning",
      "variant": "soft"
    },
    "critical": {
      "label_id": "Kritis",
      "label_en": "Critical",
      "semantic": "critical",
      "variant": "solid"
    },
    "offline": {
      "label_id": "Offline",
      "label_en": "Offline",
      "semantic": "neutral",
      "variant": "outline"
    },
    "unknown": {
      "label_id": "Belum ada data",
      "label_en": "Unknown",
      "semantic": "neutral",
      "variant": "soft"
    }
  },
  "validity": {
    "valid": {
      "label_id": "Berlaku",
      "label_en": "Valid",
      "semantic": "success",
      "variant": "soft"
    },
    "expiring": {
      "label_id": "Segera kedaluwarsa",
      "label_en": "Expiring",
      "semantic": "warning",
      "variant": "soft"
    },
    "expired": {
      "label_id": "Kedaluwarsa",
      "label_en": "Expired",
      "semantic": "critical",
      "variant": "solid"
    },
    "no_expiry": {
      "label_id": "Tanpa masa berlaku",
      "label_en": "No expiry",
      "semantic": "neutral",
      "variant": "outline"
    }
  },
  "cleaning_route_run": {
    "scheduled": {
      "label_id": "Terjadwal",
      "label_en": "Scheduled",
      "semantic": "info",
      "variant": "soft"
    },
    "in_progress": {
      "label_id": "Sedang Dikerjakan",
      "label_en": "In Progress",
      "semantic": "info",
      "variant": "solid"
    },
    "completed": {
      "label_id": "Selesai",
      "label_en": "Completed",
      "semantic": "success",
      "variant": "soft"
    },
    "cancelled": {
      "label_id": "Dibatalkan",
      "label_en": "Cancelled",
      "semantic": "neutral",
      "variant": "outline"
    }
  },
  "announcement": {
    "draft": {
      "label_id": "Draft",
      "label_en": "Draft",
      "semantic": "neutral",
      "variant": "outline"
    },
    "scheduled": {
      "label_id": "Terjadwal",
      "label_en": "Scheduled",
      "semantic": "info",
      "variant": "soft"
    },
    "published": {
      "label_id": "Terbit",
      "label_en": "Published",
      "semantic": "success",
      "variant": "soft"
    },
    "archived": {
      "label_id": "Diarsipkan",
      "label_en": "Archived",
      "semantic": "neutral",
      "variant": "outline"
    }
  },
  "package": {
    "received": {
      "label_id": "Diterima",
      "label_en": "Received",
      "semantic": "info",
      "variant": "soft"
    },
    "notified": {
      "label_id": "Menunggu Diambil",
      "label_en": "Awaiting Pickup",
      "semantic": "warning",
      "variant": "soft"
    },
    "picked_up": {
      "label_id": "Sudah Diambil",
      "label_en": "Picked Up",
      "semantic": "success",
      "variant": "soft"
    },
    "returned": {
      "label_id": "Dikembalikan",
      "label_en": "Returned",
      "semantic": "neutral",
      "variant": "outline"
    }
  },
  "parking_permit": {
    "requested": {
      "label_id": "Diajukan",
      "label_en": "Requested",
      "semantic": "warning",
      "variant": "soft"
    },
    "approved": {
      "label_id": "Disetujui",
      "label_en": "Approved",
      "semantic": "success",
      "variant": "soft"
    },
    "rejected": {
      "label_id": "Ditolak",
      "label_en": "Rejected",
      "semantic": "critical",
      "variant": "soft"
    },
    "cancelled": {
      "label_id": "Dibatalkan",
      "label_en": "Cancelled",
      "semantic": "neutral",
      "variant": "outline"
    },
    "expired": {
      "label_id": "Berakhir",
      "label_en": "Expired",
      "semantic": "neutral",
      "variant": "soft"
    },
    "revoked": {
      "label_id": "Dicabut",
      "label_en": "Revoked",
      "semantic": "critical",
      "variant": "outline"
    }
  },
  "tenant_feedback": {
    "new": {
      "label_id": "Baru",
      "label_en": "New",
      "semantic": "info",
      "variant": "soft"
    },
    "in_review": {
      "label_id": "Ditinjau",
      "label_en": "In Review",
      "semantic": "warning",
      "variant": "soft"
    },
    "responded": {
      "label_id": "Ditanggapi",
      "label_en": "Responded",
      "semantic": "success",
      "variant": "soft"
    },
    "closed": {
      "label_id": "Ditutup",
      "label_en": "Closed",
      "semantic": "success",
      "variant": "solid"
    }
  },
  "recurring_issue": {
    "open": {
      "label_id": "Terbuka",
      "label_en": "Open",
      "semantic": "critical",
      "variant": "soft"
    },
    "acknowledged": {
      "label_id": "Ditangani",
      "label_en": "Acknowledged",
      "semantic": "warning",
      "variant": "soft"
    },
    "resolved": {
      "label_id": "Selesai",
      "label_en": "Resolved",
      "semantic": "success",
      "variant": "soft"
    }
  },
  "billing_run": {
    "preview": {
      "label_id": "Pratinjau",
      "label_en": "Preview",
      "semantic": "info",
      "variant": "outline"
    },
    "generated": {
      "label_id": "Draft Dibuat",
      "label_en": "Generated",
      "semantic": "info",
      "variant": "soft"
    },
    "issued": {
      "label_id": "Diterbitkan",
      "label_en": "Issued",
      "semantic": "success",
      "variant": "soft"
    },
    "cancelled": {
      "label_id": "Dibatalkan",
      "label_en": "Cancelled",
      "semantic": "neutral",
      "variant": "outline"
    }
  },
  "meter_reading": {
    "recorded": {
      "label_id": "Tercatat",
      "label_en": "Recorded",
      "semantic": "info",
      "variant": "soft"
    },
    "flagged": {
      "label_id": "Perlu Dicek",
      "label_en": "Flagged",
      "semantic": "warning",
      "variant": "solid"
    },
    "approved": {
      "label_id": "Disetujui",
      "label_en": "Approved",
      "semantic": "success",
      "variant": "soft"
    },
    "rejected": {
      "label_id": "Ditolak",
      "label_en": "Rejected",
      "semantic": "critical",
      "variant": "soft"
    }
  },
  "credit_note": {
    "pending": {
      "label_id": "Menunggu Persetujuan",
      "label_en": "Pending Approval",
      "semantic": "warning",
      "variant": "soft"
    },
    "approved": {
      "label_id": "Disetujui",
      "label_en": "Approved",
      "semantic": "success",
      "variant": "soft"
    },
    "rejected": {
      "label_id": "Ditolak",
      "label_en": "Rejected",
      "semantic": "critical",
      "variant": "soft"
    },
    "cancelled": {
      "label_id": "Dibatalkan",
      "label_en": "Cancelled",
      "semantic": "neutral",
      "variant": "outline"
    }
  },
  "invoice_penalty": {
    "accruing": {
      "label_id": "Berjalan",
      "label_en": "Accruing",
      "semantic": "warning",
      "variant": "soft"
    },
    "final": {
      "label_id": "Final",
      "label_en": "Final",
      "semantic": "info",
      "variant": "soft"
    },
    "billed": {
      "label_id": "Ditagihkan",
      "label_en": "Billed",
      "semantic": "success",
      "variant": "soft"
    },
    "waived": {
      "label_id": "Dihapuskan",
      "label_en": "Waived",
      "semantic": "neutral",
      "variant": "outline"
    }
  },
  "collection_promise": {
    "open": {
      "label_id": "Menunggu",
      "label_en": "Open",
      "semantic": "warning",
      "variant": "soft"
    },
    "kept": {
      "label_id": "Ditepati",
      "label_en": "Kept",
      "semantic": "success",
      "variant": "soft"
    },
    "broken": {
      "label_id": "Ingkar",
      "label_en": "Broken",
      "semantic": "critical",
      "variant": "soft"
    },
    "cancelled": {
      "label_id": "Dibatalkan",
      "label_en": "Cancelled",
      "semantic": "neutral",
      "variant": "outline"
    }
  },
  "bank_statement_line": {
    "unmatched": {
      "label_id": "Belum Cocok",
      "label_en": "Unmatched",
      "semantic": "warning",
      "variant": "soft"
    },
    "suggested": {
      "label_id": "Ada Saran",
      "label_en": "Suggested",
      "semantic": "info",
      "variant": "soft"
    },
    "matched": {
      "label_id": "Cocok",
      "label_en": "Matched",
      "semantic": "success",
      "variant": "soft"
    },
    "ignored": {
      "label_id": "Diabaikan",
      "label_en": "Ignored",
      "semantic": "neutral",
      "variant": "outline"
    }
  },
  "budget": {
    "draft": {
      "label_id": "Draft",
      "label_en": "Draft",
      "semantic": "neutral",
      "variant": "outline"
    },
    "approved": {
      "label_id": "Disetujui",
      "label_en": "Approved",
      "semantic": "success",
      "variant": "soft"
    },
    "superseded": {
      "label_id": "Digantikan",
      "label_en": "Superseded",
      "semantic": "neutral",
      "variant": "soft"
    }
  },
  "webhook_delivery": {
    "pending": {
      "label_id": "Menunggu",
      "label_en": "Pending",
      "semantic": "info",
      "variant": "soft"
    },
    "delivered": {
      "label_id": "Terkirim",
      "label_en": "Delivered",
      "semantic": "success",
      "variant": "soft"
    },
    "failed": {
      "label_id": "Gagal",
      "label_en": "Failed",
      "semantic": "critical",
      "variant": "soft"
    }
  }
} as const;
export function statusDef(objectType: ObjectType, status: string): StatusDef | undefined { return statusMap[objectType]?.[status]; }
