// Settings › App Downloads (Website PRD §16–§21, §44–§48): tautan Google Drive aplikasi Staff/Tenant untuk website publik.
// Hanya role admin_internal (menu disembunyikan untuk yang lain; backend tetap memeriksa server-side).
import { useMemo, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Icon } from "@buildingvision/ui";
import type { ColumnDef } from "@tanstack/react-table";
import { Alert, Badge, Button, Dialog, DialogContent, DialogFooter, Field, Input, NativeSelect, Textarea } from "@/components/ui/primitives";
import { DataGrid } from "@/components/bv/datagrid";
import { RelativeTime, useToast } from "@/components/bv/common";
import { api, ApiError } from "@/lib/api";
import { useAuth } from "@/lib/auth";

interface AppDownload {
  id: string;
  name: string;
  app_type: "staff" | "tenant" | "customer";
  platform: "android" | "ios" | "other";
  download_url: string;
  status: "active" | "inactive";
  notes: string | null;
  updated_at: string;
  updated_by_name: string;
  version: number;
}

const APP_TYPE_LABEL = { staff: "Staff App", tenant: "Tenant App", customer: "Customer App (BVRooms)" } as const;
const PLATFORM_LABEL = { android: "Android", ios: "iOS", other: "Lainnya" } as const;
const DRIVE_HOSTS = ["drive.google.com", "docs.google.com", "drive.usercontent.google.com"];

function isDriveURL(v: string): boolean {
  try {
    const u = new URL(v.trim());
    return u.protocol === "https:" && DRIVE_HOSTS.includes(u.hostname.toLowerCase());
  } catch {
    return false;
  }
}

export default function AppDownloadsSection() {
  const { principal } = useAuth();
  const toast = useToast();
  const qc = useQueryClient();
  const list = useQuery({ queryKey: ["admin-app-downloads"], queryFn: () => api<{ data: AppDownload[] }>("admin/app-downloads").then((r) => r.data), enabled: !!principal?.is_internal_admin });
  const [edit, setEdit] = useState<AppDownload | null | "new">(null);
  const refresh = () => qc.invalidateQueries({ queryKey: ["admin-app-downloads"] });

  const setStatus = async (r: AppDownload, status: "active" | "inactive") => {
    try {
      await api(`admin/app-downloads/${r.id}/${status === "active" ? "activate" : "deactivate"}`, { method: "POST", body: {} });
      toast.success(`${r.name} ${status === "active" ? "diaktifkan" : "dinonaktifkan"}. Website publik terbarui otomatis.`);
      refresh();
    } catch (e) {
      toast.error(e);
    }
  };

  const columns = useMemo<ColumnDef<AppDownload, unknown>[]>(
    () => [
      { id: "name", header: "App Name", cell: ({ row }) => <div><div className="font-medium">{row.original.name}</div><div className="text-xs text-on-surface-variant">{APP_TYPE_LABEL[row.original.app_type]}</div></div> },
      { id: "platform", header: "Platform", cell: ({ row }) => PLATFORM_LABEL[row.original.platform], size: 100 },
      { id: "url", header: "Download URL", cell: ({ row }) => <a href={row.original.download_url} target="_blank" rel="noreferrer" className="block max-w-[360px] truncate text-primary underline" onClick={(e) => e.stopPropagation()}>{row.original.download_url}</a> },
      { id: "status", header: "Status", cell: ({ row }) => <Badge tone={row.original.status === "active" ? "success" : "neutral"}>{row.original.status === "active" ? "Active" : "Inactive"}</Badge>, size: 100 },
      { id: "updated", header: "Updated", cell: ({ row }) => <div className="text-xs"><RelativeTime value={row.original.updated_at} /><div className="text-on-surface-variant">{row.original.updated_by_name || "-"}</div></div>, size: 150 },
    ],
    [],
  );

  if (!principal?.is_internal_admin) {
    return <Alert variant="warning" title="Akses terbatas">Pengaturan App Downloads hanya untuk Admin Internal BuildingVision.</Alert>;
  }
  return (
    <div className="space-y-3">
      <Alert variant="info" title="Distribusi via Google Drive">Website publik membaca tautan aktif dari <code>GET /api/v1/public/app-downloads</code>. Mengubah tautan di sini tidak memerlukan deploy website. URL harus berada di domain Google Drive (drive.google.com / docs.google.com). Setiap perubahan tercatat di Audit Log.</Alert>
      <div className="flex justify-end"><Button onClick={() => setEdit("new")}><Icon name="add" size={16} /> Add App</Button></div>
      <DataGrid
        columns={columns}
        rows={list.data ?? []}
        rowId={(r) => r.id}
        loading={list.isLoading}
        onRowClick={(r) => setEdit(r)}
        empty={{ message: "No apps have been configured yet.", cta: <Button size="sm" onClick={() => setEdit("new")}>Add App</Button> }}
        rowActions={(r) => [
          { label: "Edit", icon: "edit", onSelect: () => setEdit(r) },
          r.status === "active" ? { label: "Deactivate", icon: "pause_circle", onSelect: () => setStatus(r, "inactive"), destructive: true } : { label: "Activate", icon: "play_circle", onSelect: () => setStatus(r, "active") },
        ]}
      />
      {edit && <AppDialog item={edit === "new" ? null : edit} onClose={() => setEdit(null)} onSaved={() => { setEdit(null); refresh(); }} />}
    </div>
  );
}

function AppDialog({ item, onClose, onSaved }: { item: AppDownload | null; onClose: () => void; onSaved: () => void }) {
  const toast = useToast();
  const [form, setForm] = useState({
    name: item?.name ?? "",
    app_type: item?.app_type ?? "staff",
    platform: item?.platform ?? "android",
    download_url: item?.download_url ?? "",
    status: item?.status ?? "inactive",
    notes: item?.notes ?? "",
  });
  const [busy, setBusy] = useState(false);
  const [urlError, setUrlError] = useState<string | null>(null);
  const suggestName = (t: "staff" | "tenant" | "customer") => (t === "staff" ? "BuildingVision Staff App" : t === "tenant" ? "BuildingVision Tenant App" : "BVRooms Customer App");

  const save = async () => {
    if (!form.name.trim()) return toast.error(new Error("App Name wajib diisi"));
    if (!isDriveURL(form.download_url)) {
      setUrlError("URL harus https dan berada di domain Google Drive (drive.google.com / docs.google.com)");
      return;
    }
    setUrlError(null);
    setBusy(true);
    try {
      const body = { name: form.name.trim(), app_type: form.app_type, platform: form.platform, download_url: form.download_url.trim(), status: form.status, notes: form.notes || null };
      if (item) await api(`admin/app-downloads/${item.id}`, { method: "PATCH", body, ifMatch: item.version });
      else await api("admin/app-downloads", { body });
      toast.success(item ? "App download disimpan" : "App download ditambahkan");
      onSaved();
    } catch (e) {
      if (e instanceof ApiError && e.problem.errors?.some((x) => x.field === "download_url")) setUrlError(e.message);
      else toast.error(e);
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent side="right" title={item ? `App Download · ${item.name}` : "Add App"} description="Tautan Google Drive yang ditampilkan di halaman Download Apps website.">
        <div className="space-y-4">
          <Field label="App Type" required>
            <NativeSelect value={form.app_type} onChange={(e) => { const t = e.target.value as "staff" | "tenant" | "customer"; setForm((f) => ({ ...f, app_type: t, name: f.name === "" || f.name === suggestName(f.app_type) ? suggestName(t) : f.name })); }}>
              <option value="staff">Staff App (staf & tim operasional)</option>
              <option value="tenant">Tenant App (tenant, penghuni, tamu)</option>
              <option value="customer">Customer App — BVRooms (booking kamar & unit)</option>
            </NativeSelect>
          </Field>
          <Field label="App Name" required><Input value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} placeholder={suggestName(form.app_type)} /></Field>
          <Field label="Platform" required>
            <NativeSelect value={form.platform} onChange={(e) => setForm({ ...form, platform: e.target.value as AppDownload["platform"] })}>
              <option value="android">Android</option>
              <option value="ios">iOS</option>
              <option value="other">Lainnya</option>
            </NativeSelect>
          </Field>
          <Field label="Google Drive URL" required error={urlError ?? undefined} help="Contoh: https://drive.google.com/file/d/…/view. Sistem hanya menyimpan tujuan tautan, tidak mengunduh file.">
            <Input value={form.download_url} onChange={(e) => { setForm({ ...form, download_url: e.target.value }); setUrlError(null); }} placeholder="https://drive.google.com/file/d/…/view" />
          </Field>
          <Field label="Status">
            <NativeSelect value={form.status} onChange={(e) => setForm({ ...form, status: e.target.value as AppDownload["status"] })}>
              <option value="inactive">Inactive (tidak tampil di website)</option>
              <option value="active">Active (tampil di website)</option>
            </NativeSelect>
          </Field>
          <Field label="Catatan internal"><Textarea rows={2} value={form.notes} onChange={(e) => setForm({ ...form, notes: e.target.value })} placeholder="mis. build 1.4.2, tanggal rilis" /></Field>
        </div>
        <DialogFooter>
          <Button variant="ghost" onClick={onClose}>Batal</Button>
          <Button onClick={save} loading={busy}>Simpan</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
