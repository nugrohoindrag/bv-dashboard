// GENERATED — jangan edit manual. Sumber: contracts/status-map.yaml.

class StatusDef {
  final String labelId; final String labelEn; final String semantic; final String variant; final String? icon;
  const StatusDef(this.labelId, this.labelEn, this.semantic, this.variant, [this.icon]);
}

const Map<String, Map<String, StatusDef>> statusMap = {
  'task': {
    'new': StatusDef('Baru', 'New', 'neutral', 'soft'),
    'scheduled': StatusDef('Terjadwal', 'Scheduled', 'info', 'soft'),
    'assigned': StatusDef('Ditugaskan', 'Assigned', 'info', 'soft'),
    'in_progress': StatusDef('Sedang Dikerjakan', 'In Progress', 'info', 'solid'),
    'on_hold': StatusDef('Ditunda', 'On Hold', 'warning', 'soft'),
    'completed': StatusDef('Selesai', 'Completed', 'success', 'soft'),
    'closed': StatusDef('Ditutup', 'Closed', 'success', 'solid'),
    'cancelled': StatusDef('Dibatalkan', 'Cancelled', 'neutral', 'outline'),
  },
  'work_order': {
    'new': StatusDef('Baru', 'New', 'neutral', 'soft'),
    'scheduled': StatusDef('Terjadwal', 'Scheduled', 'info', 'soft'),
    'assigned': StatusDef('Ditugaskan', 'Assigned', 'info', 'soft'),
    'in_progress': StatusDef('Sedang Dikerjakan', 'In Progress', 'info', 'solid'),
    'on_hold': StatusDef('Ditunda', 'On Hold', 'warning', 'soft'),
    'completed': StatusDef('Selesai', 'Completed', 'success', 'soft'),
    'closed': StatusDef('Ditutup', 'Closed', 'success', 'solid'),
    'cancelled': StatusDef('Dibatalkan', 'Cancelled', 'neutral', 'outline'),
  },
  'service_request': {
    'new': StatusDef('Baru', 'New', 'neutral', 'soft'),
    'acknowledged': StatusDef('Diterima', 'Acknowledged', 'info', 'soft'),
    'assigned': StatusDef('Ditugaskan', 'Assigned', 'info', 'soft'),
    'in_progress': StatusDef('Sedang Dikerjakan', 'In Progress', 'info', 'solid'),
    'waiting_for_tenant': StatusDef('Menunggu Tenant', 'Waiting for Tenant', 'warning', 'soft'),
    'resolved': StatusDef('Terselesaikan', 'Resolved', 'success', 'soft'),
    'closed': StatusDef('Ditutup', 'Closed', 'success', 'solid'),
    'cancelled': StatusDef('Dibatalkan', 'Cancelled', 'neutral', 'outline'),
  },
  'maintenance_schedule': {
    'scheduled': StatusDef('Terjadwal', 'Scheduled', 'info', 'soft'),
    'due': StatusDef('Jatuh Tempo', 'Due', 'warning', 'soft'),
    'in_progress': StatusDef('Sedang Dikerjakan', 'In Progress', 'info', 'solid'),
    'completed': StatusDef('Selesai', 'Completed', 'success', 'soft'),
    'overdue': StatusDef('Overdue', 'Overdue', 'critical', 'solid'),
    'skipped': StatusDef('Dilewati', 'Skipped', 'neutral', 'outline'),
    'cancelled': StatusDef('Dibatalkan', 'Cancelled', 'neutral', 'outline'),
  },
  'incident': {
    'new': StatusDef('Baru', 'New', 'neutral', 'soft'),
    'assigned': StatusDef('Ditugaskan', 'Assigned', 'info', 'soft'),
    'in_progress': StatusDef('Sedang Ditangani', 'In Progress', 'info', 'solid'),
    'resolved': StatusDef('Terselesaikan', 'Resolved', 'success', 'soft'),
    'closed': StatusDef('Ditutup', 'Closed', 'success', 'solid'),
    'cancelled': StatusDef('Dibatalkan', 'Cancelled', 'neutral', 'outline'),
  },
  'finding': {
    'open': StatusDef('Terbuka', 'Open', 'warning', 'soft'),
    'in_progress': StatusDef('Sedang Ditangani', 'In Progress', 'info', 'soft'),
    'resolved': StatusDef('Terselesaikan', 'Resolved', 'success', 'soft'),
    'closed': StatusDef('Ditutup', 'Closed', 'success', 'solid'),
  },
  'inspection': {
    'new': StatusDef('Baru', 'New', 'neutral', 'soft'),
    'scheduled': StatusDef('Terjadwal', 'Scheduled', 'info', 'soft'),
    'assigned': StatusDef('Ditugaskan', 'Assigned', 'info', 'soft'),
    'in_progress': StatusDef('Sedang Dikerjakan', 'In Progress', 'info', 'solid'),
    'completed': StatusDef('Selesai', 'Completed', 'success', 'soft'),
    'closed': StatusDef('Ditutup', 'Closed', 'success', 'solid'),
    'cancelled': StatusDef('Dibatalkan', 'Cancelled', 'neutral', 'outline'),
  },
  'checkpoint': {
    'pending': StatusDef('Menunggu', 'Pending', 'neutral', 'soft'),
    'scanned': StatusDef('Terverifikasi', 'Scanned', 'success', 'soft'),
    'missed': StatusDef('Terlewat', 'Missed', 'critical', 'solid'),
  },
  'asset': {
    'active': StatusDef('Aktif', 'Active', 'success', 'soft'),
    'inactive': StatusDef('Tidak Aktif', 'Inactive', 'neutral', 'soft'),
    'under_maintenance': StatusDef('Dalam Perawatan', 'Under Maintenance', 'warning', 'soft'),
    'decommissioned': StatusDef('Dinonaktifkan', 'Decommissioned', 'neutral', 'outline'),
  },
  'authoring': {
    'draft': StatusDef('Draft', 'Draft', 'neutral', 'soft'),
    'published': StatusDef('Dipublikasikan', 'Published', 'success', 'soft'),
    'archived': StatusDef('Diarsipkan', 'Archived', 'neutral', 'outline'),
  },
  'flags': {
    'overdue': StatusDef('Overdue', 'Overdue', 'critical', 'solid', 'alarm-clock'),
    'sla_risk': StatusDef('SLA Risk', 'SLA Risk', 'warning', 'soft', 'timer'),
    'sla_breach': StatusDef('SLA Breach', 'SLA Breach', 'critical', 'solid', 'timer-off'),
    'evidence_incomplete': StatusDef('Evidence belum lengkap', 'Evidence incomplete', 'warning', 'soft', 'image-off'),
  },
  'priority': {
    'low': StatusDef('Rendah', 'Low', 'neutral', 'soft', 'arrow-down'),
    'medium': StatusDef('Sedang', 'Medium', 'info', 'soft', 'minus'),
    'high': StatusDef('Tinggi', 'High', 'warning', 'soft', 'arrow-up'),
    'critical': StatusDef('Kritis', 'Critical', 'critical', 'solid', 'alert-triangle'),
  },
  'severity': {
    'low': StatusDef('Rendah', 'Low', 'neutral', 'soft', 'arrow-down'),
    'medium': StatusDef('Sedang', 'Medium', 'info', 'soft', 'minus'),
    'high': StatusDef('Tinggi', 'High', 'warning', 'soft', 'arrow-up'),
    'critical': StatusDef('Kritis', 'Critical', 'critical', 'solid', 'alert-triangle'),
  },
  'sync_state': {
    'pending': StatusDef('Pending Sync', 'Pending Sync', 'warning', 'soft', 'cloud-upload'),
    'synced': StatusDef('Synced', 'Synced', 'success', 'soft', 'cloud-check'),
    'failed': StatusDef('Sync Failed', 'Sync Failed', 'critical', 'soft', 'cloud-off'),
    'conflict': StatusDef('Sync Conflict', 'Sync Conflict', 'warning', 'soft', 'git-merge'),
  },
};
