// GENERATED — jangan edit manual. Sumber: contracts/status-map.yaml. Jalankan `npm run gen`.
export type Semantic = "success" | "warning" | "critical" | "info" | "neutral";
export type Variant = "solid" | "soft" | "outline";
export interface StatusDef { label_id: string; label_en: string; semantic: Semantic; variant: Variant; icon?: string }
export type ObjectType = "task" | "work_order" | "service_request" | "service_request_tenant" | "booking" | "bvrooms_booking_customer" | "bvrooms_payment" | "visitor" | "invoice" | "payment" | "maintenance_schedule" | "incident" | "finding" | "inspection" | "checkpoint" | "asset" | "authoring" | "flags" | "priority" | "severity" | "sync_state";
export const statusMap: Record<ObjectType, Record<string, StatusDef>> = {
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
      "label_id": "Ditunda",
      "label_en": "On Hold",
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
      "label_id": "Ditunda",
      "label_en": "On Hold",
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
      "label_id": "Diterima",
      "label_en": "Acknowledged",
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
  }
} as const;
export function statusDef(objectType: ObjectType, status: string): StatusDef | undefined { return statusMap[objectType]?.[status]; }
