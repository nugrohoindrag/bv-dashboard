// Query hooks per resource (TanStack Query, TAD §9.1). Semua list cursor-based (TAD §6.3).
import { useInfiniteQuery, useMutation, useQuery, useQueryClient, type QueryKey } from "@tanstack/react-query";
import { api, uuid, type ListResponse } from "@/lib/api";
import type * as T from "./types";

export type Query = Record<string, string | number | boolean | undefined | null>;

export function useList<Item>(resource: string, query: Query = {}, opts: { enabled?: boolean; limit?: number } = {}) {
  const key: QueryKey = ["list", resource, query];
  return useInfiniteQuery({
    queryKey: key,
    enabled: opts.enabled,
    initialPageParam: null as string | null,
    queryFn: ({ pageParam, signal }) => api<ListResponse<Item>>(resource, { query: { ...query, cursor: pageParam ?? undefined, limit: opts.limit ?? 50 }, signal }),
    getNextPageParam: (last) => last.next_cursor ?? undefined,
    staleTime: 15_000,
  });
}

export function useAll<Item>(resource: string, query: Query = {}, opts: { enabled?: boolean } = {}) {
  return useQuery({ queryKey: ["all", resource, query], enabled: opts.enabled, queryFn: ({ signal }) => api<ListResponse<Item>>(resource, { query: { ...query, limit: 200 }, signal }).then((r) => r.data), staleTime: 30_000 });
}

export function useOne<Item>(resource: string, id?: string | null, opts: { enabled?: boolean } = {}) {
  return useQuery({ queryKey: ["one", resource, id], enabled: !!id && opts.enabled !== false, queryFn: ({ signal }) => api<Item>(`${resource}/${id}`, { signal }), staleTime: 10_000 });
}

// ---------- Work items ----------
export const resourceOf = (ot: "task" | "work_order") => (ot === "task" ? "tasks" : "work-orders");

export function useWorkItem(ot: "task" | "work_order", id?: string) {
  return useOne<T.WorkItem>(resourceOf(ot), id);
}
export function useActivities(ot: string, id?: string) {
  return useQuery({ queryKey: ["activities", ot, id], enabled: !!id, queryFn: () => api<ListResponse<T.Activity>>("activities", { query: { object_type: ot, object_id: id } }).then((r) => r.data) });
}
export function useComments(resource: string, id?: string) {
  return useQuery({ queryKey: ["comments", resource, id], enabled: !!id, queryFn: () => api<ListResponse<T.Comment>>(`${resource}/${id}/comments`).then((r) => r.data) });
}
export function useAttachments(ot: string, id?: string) {
  return useQuery({ queryKey: ["attachments", ot, id], enabled: !!id, queryFn: () => api<ListResponse<T.Attachment>>("attachments", { query: { object_type: ot, object_id: id } }).then((r) => r.data) });
}
export function useChecklistRuns(resource: string, id?: string) {
  return useQuery({ queryKey: ["checklist-runs", resource, id], enabled: !!id, queryFn: () => api<ListResponse<T.ChecklistRun>>(`${resource}/${id}/checklist-runs`).then((r) => r.data) });
}

export function useInvalidate() {
  const qc = useQueryClient();
  return (...prefixes: string[]) => {
    for (const p of prefixes) qc.invalidateQueries({ predicate: (q) => (q.queryKey as unknown[]).some((k) => typeof k === "string" && k.includes(p)) });
  };
}

// Aksi generik (transisi / assign / create) dengan Idempotency-Key.
export function useAction<TIn = unknown, TOut = unknown>(path: (input: TIn) => string, opts: { invalidate?: string[]; method?: string; body?: (input: TIn) => unknown } = {}) {
  const invalidate = useInvalidate();
  return useMutation({
    mutationFn: (input: TIn) => api<TOut>(path(input), { method: opts.method ?? "POST", body: opts.body ? opts.body(input) : (input as unknown as Record<string, unknown>), idempotencyKey: uuid() }),
    onSuccess: () => invalidate(...(opts.invalidate ?? ["list", "one", "activities", "overview", "comments", "checklist-runs", "attachments", "notifications"])),
  });
}

export function useTransition(resource: string) {
  return useAction<{ id: string; action: string; body?: Record<string, unknown> }, T.WorkItem>((i) => `${resource}/${i.id}/${i.action}`, { body: (i) => i.body ?? {} });
}
export function useAssign(resource: string) {
  return useAction<{ id: string; assignee_user_id?: string | null; assignee_team_id?: string | null; note?: string }, unknown>((i) => `${resource}/${i.id}/assign`, { body: (i) => ({ assignee_user_id: i.assignee_user_id ?? null, assignee_team_id: i.assignee_team_id ?? null, note: i.note }) });
}
export function useCreate<TIn, TOut = unknown>(resource: string) {
  return useAction<TIn, TOut>(() => resource);
}
export function useUpdate<TIn extends { id: string; version?: number }, TOut = unknown>(resource: string) {
  const invalidate = useInvalidate();
  return useMutation({
    mutationFn: ({ id, version, ...body }: TIn) => api<TOut>(`${resource}/${id}`, { method: "PATCH", body, ifMatch: version }),
    onSuccess: () => invalidate("list", "one", "activities", "overview"),
  });
}
export function useAddComment(resource: string) {
  return useAction<{ id: string; body: string }, T.Comment>((i) => `${resource}/${i.id}/comments`, { body: (i) => ({ body: i.body }) });
}
export function useAnswerItem() {
  return useAction<{ itemId: string; answer: Record<string, unknown> }, T.ChecklistRun>((i) => `checklist-run-items/${i.itemId}/answer`, { body: (i) => i.answer });
}

// ---------- Master data ----------
export function useUsers(query: Query = {}) {
  return useAll<T.User>("users", query);
}
export function useTeams(propertyId?: string | null, domain?: string) {
  return useAll<T.Team>("teams", { property_id: propertyId ?? undefined, domain });
}
export function useLocationTree(propertyId?: string | null) {
  return useQuery({ queryKey: ["tree", propertyId], enabled: !!propertyId, queryFn: () => api<T.TreeNode>("locations/tree", { query: { property_id: propertyId } }), staleTime: 60_000 });
}
export function useAssets(query: Query = {}) {
  return useAll<T.Asset>("assets", query);
}
export function useTemplates(query: Query = {}) {
  return useAll<T.ChecklistTemplate>("checklist-templates", query);
}

// ---------- Notifications ----------
export function useNotifications(unread = false) {
  return useQuery({ queryKey: ["notifications", unread], queryFn: () => api<{ data: T.Notification[]; unread_count: number; next_cursor: string | null }>("notifications", { query: { unread: unread || undefined, limit: 30 } }), refetchInterval: 60_000 });
}

// ---------- Overview ----------
export function useOverview<TOut>(panel: string, query: Query = {}) {
  return useQuery({ queryKey: ["overview", panel, query], queryFn: ({ signal }) => api<TOut>(`overview/${panel}`, { query, signal }), refetchInterval: 60_000, staleTime: 30_000 });
}

// ---------- Search ----------
export function useSearch(q: string, propertyId?: string | null) {
  return useQuery({ queryKey: ["search", q, propertyId], enabled: q.trim().length >= 2, queryFn: ({ signal }) => api<ListResponse<T.SearchResult>>("search", { query: { q, property_id: propertyId ?? undefined }, signal }).then((r) => r.data), staleTime: 5_000 });
}

// ---------- Attachments (presign → PUT → confirm; kompresi ≤1600px di client, TAD §5.13) ----------
export async function uploadAttachment(file: File, objectType: string, objectId: string, attachmentType: string, onProgress?: (p: number) => void): Promise<T.Attachment> {
  const blob = file.type.startsWith("image/") ? await compressImage(file) : file;
  const presign = await api<{ attachment_id: string; upload_url: string }>("attachments/presign", {
    body: { object_type: objectType, object_id: objectId, attachment_type: attachmentType, content_type: blob.type || file.type, size_bytes: blob.size, original_filename: file.name, client_attachment_id: uuid() },
  });
  onProgress?.(30);
  const put = await fetch(presign.upload_url, { method: "PUT", body: blob, headers: { "Content-Type": blob.type || file.type } });
  if (!put.ok) throw new Error("Upload ke storage gagal (" + put.status + ")");
  onProgress?.(80);
  const att = await api<T.Attachment>(`attachments/${presign.attachment_id}/confirm`, { body: { captured_at: new Date().toISOString(), gps_status: "unavailable" } });
  onProgress?.(100);
  return att;
}

async function compressImage(file: File, max = 1600, quality = 0.8): Promise<Blob> {
  try {
    const bmp = await createImageBitmap(file);
    const scale = Math.min(1, max / Math.max(bmp.width, bmp.height));
    if (scale === 1 && file.size < 400_000) return file;
    const canvas = document.createElement("canvas");
    canvas.width = Math.round(bmp.width * scale);
    canvas.height = Math.round(bmp.height * scale);
    canvas.getContext("2d")!.drawImage(bmp, 0, 0, canvas.width, canvas.height);
    return await new Promise<Blob>((res, rej) => canvas.toBlob((b) => (b ? res(b) : rej(new Error("compress"))), "image/jpeg", quality));
  } catch {
    return file;
  }
}
