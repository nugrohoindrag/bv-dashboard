// GENERATED — jangan edit manual. Sumber: contracts/status-map.yaml. Jalankan `npm run gen`.
export type Semantic = "success" | "warning" | "critical" | "info" | "neutral";
export type Variant = "solid" | "soft" | "outline";
export interface StatusDef { label_id: string; label_en: string; semantic: Semantic; variant: Variant; icon?: string }
export type ObjectType = "task" | "work_order" | "service_request" | "maintenance_schedule" | "incident" | "finding" | "inspection" | "checkpoint" | "asset" | "authoring" | "flags" | "priority" | "severity" | "sync_state";
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
