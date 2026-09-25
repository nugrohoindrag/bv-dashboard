// Foundation layer: primitif produk di atas design system Morphic/Nexus (@buildingvision/ui) + lapisan bv.
// API (nama komponen/prop) dipertahankan dari versi Radix/shadcn agar kode fitur tidak berubah besar; implementasi
// mengompos komponen DS (Button, IconButton, Checkbox, Tabs, Modal, Tooltip, SegmentedButton) dan token kontrak
// `--color-*`/`--radius-*`/`--elevation-*`. Tidak ada warna literal (Design System Guideline §2.1).
import * as React from "react";
import { createPortal } from "react-dom";
import { AnimatePresence, motion } from "motion/react";
import {
  Button as DsButton,
  Checkbox as DsCheckbox,
  Icon,
  IconButton as DsIconButton,
  SegmentedButton,
  Tabs as DsTabs,
  Tooltip as DsTooltip,
  Modal,
  type ButtonProps as DsButtonProps,
  type TabItem,
} from "@buildingvision/ui";
import { Dialog as BvConfirm, SurfaceCard, toneContainer, toneOnContainer, type Tone } from "@buildingvision/ui/bv";
import { cn } from "@/lib/utils";
import { initials } from "@/lib/format";

export { Icon };

// ---------- Button ----------
type Variant = "primary" | "secondary" | "ghost" | "destructive" | "link" | "success" | "tonal";
type Size = "sm" | "md" | "lg" | "icon" | "icon-sm";
export interface ButtonProps extends Omit<DsButtonProps, "variant" | "size" | "icon"> {
  variant?: Variant | null;
  size?: Size | null;
  loading?: boolean;
  /** Ikon Material Symbols di depan label (mis. "add"). */
  icon?: string;
  /** Untuk size="icon": label aksesibilitas wajib. */
  "aria-label"?: string;
}
const dsVariant: Record<Variant, DsButtonProps["variant"]> = { primary: "filled", secondary: "outlined", ghost: "text", destructive: "filled", link: "text", success: "filled", tonal: "tonal" };

export const Button = React.forwardRef<HTMLButtonElement, ButtonProps>(function Button({ variant, size, loading, icon, children, disabled, style, className, ...props }, ref) {
  const v: Variant = variant ?? "primary";
  const toneStyle: React.CSSProperties =
    v === "destructive" ? { backgroundColor: "var(--color-error)", color: "var(--color-on-error)" } : v === "success" ? { backgroundColor: "var(--color-success)", color: "var(--color-on-success)" } : v === "secondary" ? { color: "var(--color-on-surface)", borderColor: "var(--color-border)", backgroundColor: "var(--color-surface)" } : v === "ghost" ? { color: "var(--color-on-surface-variant)" } : v === "link" ? { padding: 0, height: "auto", textDecoration: "underline", textUnderlineOffset: 3 } : {};
  const iconNode = loading ? <Icon name="progress_activity" size={18} className="animate-spin" /> : icon ? <Icon name={icon} size={18} /> : undefined;
  if (size === "icon" || size === "icon-sm") {
    return (
      <DsIconButton
        ref={ref as React.Ref<HTMLButtonElement>}
        variant={v === "primary" ? "filled" : v === "secondary" ? "outlined" : "standard"}
        icon={iconNode ?? children}
        disabled={disabled || loading}
        className={className}
        style={{ width: size === "icon-sm" ? 32 : 40, height: size === "icon-sm" ? 32 : 40, ...toneStyle, ...style }}
        {...(props as object)}
      />
    );
  }
  const s = size === "lg" ? "lg" : size === "sm" ? "sm" : "md";
  return (
    <DsButton ref={ref as React.Ref<HTMLButtonElement>} variant={dsVariant[v]} size={s} icon={iconNode} disabled={disabled || loading} className={cn("whitespace-nowrap", className)} style={{ borderRadius: "var(--radius-md)", ...(s === "sm" ? { padding: "0 12px", fontSize: 12.5, height: 30 } : {}), ...toneStyle, ...style }} {...props}>
      {/* DS membungkus children dalam <span> inline; jadikan flex agar ikon/teks/chevron sejajar dan justify-* pemanggil berlaku */}
      <span className="flex w-full items-center gap-2" style={{ justifyContent: "inherit" }}>{children}</span>
    </DsButton>
  );
});

// ---------- Badge (pasangan solid container + on-container, DS Guideline §2.3) ----------
export function Badge({ className, dot, tone, children, ...props }: React.HTMLAttributes<HTMLSpanElement> & { dot?: boolean; tone?: Tone }) {
  return (
    <span
      className={cn("inline-flex h-[22px] items-center gap-1 whitespace-nowrap rounded-full px-2 text-xs font-semibold leading-none", !tone && "bg-surface-container-high text-on-surface-variant", className)}
      style={tone ? { backgroundColor: toneContainer[tone], color: toneOnContainer[tone] } : undefined}
      {...props}
    >
      {dot && <span aria-hidden className="h-1.5 w-1.5 rounded-full bg-current" />}
      {children}
    </span>
  );
}

// ---------- Card = SurfaceCard (rumah kartu DS: surface + hairline border + radius-xl + elevation-1) ----------
export function Card({ className, children, railTone, interactive, style, ...props }: React.HTMLAttributes<HTMLDivElement> & { railTone?: Tone; interactive?: boolean }) {
  // padding: "" menghapus padding inline SurfaceCard sehingga kelas Tailwind p-* dari pemanggil (atau CardHeader/CardContent) yang menentukan.
  return (
    <SurfaceCard padding="none" railTone={railTone} interactive={interactive} className={cn("overflow-hidden", className)} style={{ padding: "", ...style }} {...(props as object)}>
      {children}
    </SurfaceCard>
  );
}
export function CardHeader({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) {
  return <div className={cn("flex items-start justify-between gap-3 px-5 pb-2 pt-4", className)} {...props} />;
}
export function CardTitle({ className, ...props }: React.HTMLAttributes<HTMLHeadingElement>) {
  return <h2 className={cn("text-h3 font-bold text-on-surface", className)} {...props} />;
}
export function CardSubtitle({ className, ...props }: React.HTMLAttributes<HTMLParagraphElement>) {
  return <p className={cn("text-sm text-on-surface-variant", className)} {...props} />;
}
export function CardContent({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) {
  return <div className={cn("px-5 pb-5", className)} {...props} />;
}
export function CardFooter({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) {
  return <div className={cn("flex items-center justify-between border-t border-border px-5 py-3 text-sm text-on-surface-variant", className)} {...props} />;
}

// ---------- Form controls (geometri outlined text field DS: 40px, radius-input, outline → primary saat fokus) ----------
export const inputClass =
  "flex h-10 w-full rounded-[var(--radius-input)] border border-outline-variant bg-surface px-3 py-1 text-body text-on-surface transition-colors placeholder:text-outline focus-visible:border-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/25 disabled:cursor-not-allowed disabled:opacity-50 aria-[invalid=true]:border-error";
export const Input = React.forwardRef<HTMLInputElement, React.InputHTMLAttributes<HTMLInputElement>>(({ className, ...props }, ref) => <input ref={ref} className={cn(inputClass, className)} {...props} />);
Input.displayName = "Input";
export const Textarea = React.forwardRef<HTMLTextAreaElement, React.TextareaHTMLAttributes<HTMLTextAreaElement>>(({ className, ...props }, ref) => (
  <textarea ref={ref} className={cn(inputClass, "h-auto min-h-[88px] py-2", className)} {...props} />
));
Textarea.displayName = "Textarea";
export const NativeSelect = React.forwardRef<HTMLSelectElement, React.SelectHTMLAttributes<HTMLSelectElement>>(({ className, children, ...props }, ref) => (
  <span className={cn("relative inline-flex w-full", className)}>
    <select ref={ref} className={cn(inputClass, "appearance-none pr-9")} {...props}>
      {children}
    </select>
    <Icon name="expand_more" size={18} className="pointer-events-none absolute right-2.5 top-1/2 -translate-y-1/2 text-on-surface-variant" />
  </span>
));
NativeSelect.displayName = "NativeSelect";
export function Label({ className, required, children, ...props }: React.LabelHTMLAttributes<HTMLLabelElement> & { required?: boolean }) {
  return (
    <label className={cn("mb-1 block text-xs font-semibold uppercase tracking-wide text-on-surface-variant", className)} {...props}>
      {children}
      {required && (
        <span className="ml-0.5 text-error" aria-hidden>
          *
        </span>
      )}
    </label>
  );
}
export function FieldError({ children }: { children?: React.ReactNode }) {
  return children ? <p className="mt-1 text-xs text-error">{children}</p> : null;
}
export function HelperText({ children }: { children?: React.ReactNode }) {
  return children ? <p className="mt-1 text-xs text-on-surface-variant">{children}</p> : null;
}
export function Field({ label, required, error, help, children, className }: { label: string; required?: boolean; error?: string; help?: string; children: React.ReactNode; className?: string }) {
  return (
    <div className={className}>
      <Label required={required}>{label}</Label>
      {children}
      <FieldError>{error}</FieldError>
      <HelperText>{help}</HelperText>
    </div>
  );
}

// ---------- Checkbox (DS) — kompatibel prop Radix `onCheckedChange` ----------
export function Checkbox({ checked, onCheckedChange, onChange, label, disabled, className, indeterminate, onClick }: { checked?: boolean | "indeterminate"; onCheckedChange?: (v: boolean) => void; onChange?: (v: boolean) => void; label?: string; disabled?: boolean; className?: string; indeterminate?: boolean; "aria-label"?: string; onClick?: (e: React.MouseEvent) => void }) {
  return (
    <span onClick={onClick} className={cn("inline-flex", className)}>
      <DsCheckbox checked={checked === true} indeterminate={indeterminate || checked === "indeterminate"} onChange={(v) => { onCheckedChange?.(v); onChange?.(v); }} label={label} disabled={disabled} />
    </span>
  );
}

// ---------- Skeleton / Separator / Avatar ----------
export function Skeleton({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) {
  return <div className={cn("animate-pulse rounded-md bg-surface-container-highest", className)} {...props} />;
}
export function Separator({ className, vertical }: { className?: string; vertical?: boolean }) {
  return <div role="separator" className={cn(vertical ? "h-full w-px" : "h-px w-full", "bg-border", className)} />;
}
export function Avatar({ name, className, size = 28 }: { name?: string | null; className?: string; size?: number }) {
  return (
    <span aria-hidden className={cn("inline-flex shrink-0 items-center justify-center rounded-full bg-primary-container text-[11px] font-bold text-on-primary-container", className)} style={{ width: size, height: size }}>
      {initials(name)}
    </span>
  );
}

// ---------- Alert (banner solid container pair) ----------
const alertTone: Record<"info" | "success" | "warning" | "critical", { tone: Tone; icon: string }> = { info: { tone: "info", icon: "info" }, success: { tone: "success", icon: "check_circle" }, warning: { tone: "warning", icon: "warning" }, critical: { tone: "error", icon: "error" } };
export function Alert({ variant = "info", title, children, className, action }: { variant?: "info" | "success" | "warning" | "critical"; title?: string; children?: React.ReactNode; className?: string; action?: React.ReactNode }) {
  const { tone, icon } = alertTone[variant];
  return (
    <div role="alert" className={cn("flex items-start justify-between gap-3 rounded-[var(--radius-lg)] px-4 py-3 text-sm", className)} style={{ backgroundColor: toneContainer[tone], color: toneOnContainer[tone] }}>
      <div className="flex items-start gap-2">
        <Icon name={icon} size={18} className="mt-0.5 shrink-0" />
        <div>
          {title && <div className="font-bold">{title}</div>}
          {children && <div>{children}</div>}
        </div>
      </div>
      {action}
    </div>
  );
}

// ---------- Dialog (Modal DS untuk center; Drawer kanan 560px untuk form panjang) ----------
interface DialogCtx { open: boolean; setOpen: (o: boolean) => void }
const DialogContext = React.createContext<DialogCtx | null>(null);
export function Dialog({ open, onOpenChange, defaultOpen, children }: { open?: boolean; onOpenChange?: (o: boolean) => void; defaultOpen?: boolean; children: React.ReactNode }) {
  const [inner, setInner] = React.useState(!!defaultOpen);
  const isOpen = open ?? inner;
  const setOpen = React.useCallback(
    (o: boolean) => {
      setInner(o);
      onOpenChange?.(o);
    },
    [onOpenChange],
  );
  return <DialogContext.Provider value={{ open: isOpen, setOpen }}>{children}</DialogContext.Provider>;
}
function useDialog() {
  const c = React.useContext(DialogContext);
  if (!c) throw new Error("DialogContent di luar <Dialog>");
  return c;
}
export function DialogTrigger({ children }: { children: React.ReactElement<{ onClick?: (e: React.MouseEvent) => void }> }) {
  const { setOpen } = useDialog();
  return React.cloneElement(children, { onClick: (e: React.MouseEvent) => { children.props.onClick?.(e); setOpen(true); } });
}
export function DialogClose({ children }: { children: React.ReactElement<{ onClick?: (e: React.MouseEvent) => void }> }) {
  const { setOpen } = useDialog();
  return React.cloneElement(children, { onClick: (e: React.MouseEvent) => { children.props.onClick?.(e); setOpen(false); } });
}
export function DialogContent({ className, children, title, description, side, maxWidth }: { className?: string; children: React.ReactNode; title?: string; description?: string; side?: "center" | "right"; maxWidth?: string }) {
  const { open, setOpen } = useDialog();
  if (side === "right") {
    return (
      <Drawer open={open} onClose={() => setOpen(false)} title={title} description={description} className={className}>
        {children}
      </Drawer>
    );
  }
  return (
    <Modal isOpen={open} onClose={() => setOpen(false)} title={title} maxWidth={maxWidth ?? "560px"} className={className}>
      {description && <p className="-mt-2 mb-4 text-sm text-on-surface-variant">{description}</p>}
      {children}
    </Modal>
  );
}
export function DialogFooter({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) {
  return <div className={cn("mt-6 flex justify-end gap-2 border-t border-border pt-4", className)} {...props} />;
}

/** Drawer kanan (form panjang, detail): surface + hairline kiri + elevation-3, lebar 560px. */
export function Drawer({ open, onClose, title, description, children, className, width = 560 }: { open: boolean; onClose: () => void; title?: string; description?: string; children: React.ReactNode; className?: string; width?: number }) {
  React.useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && onClose();
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [open, onClose]);
  if (typeof document === "undefined") return null;
  return createPortal(
    <AnimatePresence>
      {open && (
        <div className="fixed inset-0 z-[998] flex justify-end" role="dialog" aria-modal="true" aria-label={title}>
          <motion.button type="button" aria-label="Tutup" initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }} onClick={onClose} className="absolute inset-0" style={{ backgroundColor: "var(--color-scrim)" }} />
          <motion.aside
            initial={{ x: 40, opacity: 0 }}
            animate={{ x: 0, opacity: 1 }}
            exit={{ x: 40, opacity: 0 }}
            transition={{ duration: 0.22, ease: [0.2, 0, 0, 1] }}
            className={cn("relative flex h-full max-w-full flex-col border-l border-border bg-surface text-on-surface", className)}
            style={{ width, boxShadow: "var(--elevation-3)" }}
          >
            {(title || description) && (
              <div className="flex items-start justify-between gap-3 border-b border-border px-6 py-4">
                <div>
                  {title && <h2 className="text-h2 font-bold">{title}</h2>}
                  {description && <p className="mt-0.5 text-sm text-on-surface-variant">{description}</p>}
                </div>
                <DsIconButton icon={<Icon name="close" size={20} />} aria-label="Tutup" onClick={onClose} />
              </div>
            )}
            <div className="flex-1 overflow-y-auto px-6 py-4">{children}</div>
          </motion.aside>
        </div>
      )}
    </AnimatePresence>,
    document.body,
  );
}

// ---------- ConfirmDialog = bv Dialog (konfirmasi/batal di atas Modal DS) ----------
export function ConfirmDialog({ open, onOpenChange, title, description, confirmLabel = "Ya, lanjutkan", cancelLabel = "Batal", destructive, onConfirm }: { open: boolean; onOpenChange: (o: boolean) => void; title: string; description?: string; confirmLabel?: string; cancelLabel?: string; destructive?: boolean; onConfirm: () => void; loading?: boolean }) {
  return <BvConfirm isOpen={open} onClose={() => onOpenChange(false)} headline={title} supportingText={description} confirmLabel={confirmLabel} cancelLabel={cancelLabel} destructive={destructive} onConfirm={onConfirm} />;
}

// ---------- Popover terkontrol (portal, posisi dari trigger) ----------
export function Popover({ open, onOpenChange, trigger, children, align = "end", className, width, triggerClassName }: { open: boolean; onOpenChange: (o: boolean) => void; trigger: React.ReactNode; children: React.ReactNode; align?: "start" | "end"; className?: string; width?: number; triggerClassName?: string }) {
  const ref = React.useRef<HTMLSpanElement>(null);
  const [pos, setPos] = React.useState<{ top: number; left: number; right: number } | null>(null);
  React.useLayoutEffect(() => {
    if (!open || !ref.current) return;
    const r = ref.current.getBoundingClientRect();
    setPos({ top: r.bottom + 6, left: r.left, right: window.innerWidth - r.right });
  }, [open]);
  React.useEffect(() => {
    if (!open) return;
    const onDoc = (e: MouseEvent) => {
      const t = e.target as Node;
      if (ref.current?.contains(t)) return;
      if ((t as HTMLElement).closest?.("[data-bv-popover]")) return;
      onOpenChange(false);
    };
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && onOpenChange(false);
    document.addEventListener("mousedown", onDoc);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("mousedown", onDoc);
      document.removeEventListener("keydown", onKey);
    };
  }, [open, onOpenChange]);
  return (
    <>
      <span ref={ref} className={cn("inline-flex", triggerClassName)} onClick={() => onOpenChange(!open)}>
        {trigger}
      </span>
      {open &&
        pos &&
        createPortal(
          <motion.div
            data-bv-popover
            initial={{ opacity: 0, y: -4 }}
            animate={{ opacity: 1, y: 0 }}
            // di atas drawer/dialog (998–999) & snackbar (1000), sejajar RowActionMenu DS (1100)
            className={cn("fixed z-[1100] rounded-[var(--radius-lg)] border border-border bg-surface p-2 text-on-surface", className)}
            style={{ top: pos.top, ...(align === "end" ? { right: pos.right } : { left: pos.left }), width, boxShadow: "var(--elevation-3)" }}
          >
            {children}
          </motion.div>,
          document.body,
        )}
    </>
  );
}

/** Menu aksi sederhana (pengganti DropdownMenu Radix): trigger + daftar item. */
export interface MenuItem { label: React.ReactNode; onSelect?: () => void; destructive?: boolean; icon?: string; disabled?: boolean; to?: string }
export function Menu({ trigger, items, header, align = "end", width = 200 }: { trigger: React.ReactNode; items: (MenuItem | "separator")[]; header?: React.ReactNode; align?: "start" | "end"; width?: number }) {
  const [open, setOpen] = React.useState(false);
  return (
    <Popover open={open} onOpenChange={setOpen} trigger={trigger} align={align} width={width} className="p-1">
      {header && <div className="px-2 py-1.5 text-xs uppercase tracking-wide text-on-surface-variant">{header}</div>}
      {items.map((it, i) =>
        it === "separator" ? (
          <div key={i} className="my-1 h-px bg-border" />
        ) : (
          <button
            key={i}
            type="button"
            disabled={it.disabled}
            onClick={() => {
              setOpen(false);
              it.onSelect?.();
            }}
            className={cn("flex w-full cursor-pointer items-center gap-2 rounded-[var(--radius-sm)] px-2 py-1.5 text-left text-body hover:bg-surface-container disabled:pointer-events-none disabled:opacity-50", it.destructive ? "text-error" : "text-on-surface")}
          >
            {it.icon && <Icon name={it.icon} size={16} />}
            {it.label}
          </button>
        ),
      )}
    </Popover>
  );
}

// ---------- Tooltip (DS) ----------
export function Tooltip({ content, children }: { content: React.ReactNode; children: React.ReactNode }) {
  return <DsTooltip content={typeof content === "string" ? content : String(content ?? "")}>{children}</DsTooltip>;
}
export const TooltipProvider = ({ children }: { children: React.ReactNode }) => <>{children}</>;

// ---------- Tabs (kompat API Radix di atas Tabs DS) ----------
interface TabsCtx { value: string; setValue: (v: string) => void }
const TabsContext = React.createContext<TabsCtx | null>(null);
export function Tabs({ value, defaultValue, onValueChange, children, className }: { value?: string; defaultValue?: string; onValueChange?: (v: string) => void; children: React.ReactNode; className?: string }) {
  const [inner, setInner] = React.useState(defaultValue ?? "");
  const val = value ?? inner;
  const setValue = React.useCallback((v: string) => { setInner(v); onValueChange?.(v); }, [onValueChange]);
  return (
    <TabsContext.Provider value={{ value: val, setValue }}>
      <div className={className}>{children}</div>
    </TabsContext.Provider>
  );
}
function useTabs() {
  const c = React.useContext(TabsContext);
  if (!c) throw new Error("Tabs* di luar <Tabs>");
  return c;
}
/** Teks polos dari node React (label tab DS harus string). */
function nodeText(n: React.ReactNode): string | undefined {
  if (n === null || n === undefined || typeof n === "boolean") return undefined;
  if (typeof n === "string" || typeof n === "number") return String(n);
  if (Array.isArray(n)) return n.map(nodeText).filter(Boolean).join(" ").trim() || undefined;
  if (React.isValidElement<{ children?: React.ReactNode }>(n)) return nodeText(n.props.children);
  return undefined;
}
/** Mengumpulkan <TabsTrigger> anak menjadi daftar tab DS; urutan mengikuti urutan deklarasi. */
export function TabsList({ children, className, variant = "primary" }: { children: React.ReactNode; className?: string; variant?: "primary" | "secondary" | "pills" }) {
  const { value, setValue } = useTabs();
  const tabs: TabItem[] = [];
  React.Children.forEach(children, (ch) => {
    if (!React.isValidElement<TabsTriggerProps>(ch)) return;
    const p = ch.props;
    tabs.push({ id: p.value, label: p.label ?? nodeText(p.children) ?? p.value, badge: p.badge, disabled: p.disabled, icon: p.icon });
  });
  return (
    <div className={className}>
      <DsTabs tabs={tabs} activeTab={value || tabs[0]?.id || ""} onChange={setValue} variant={variant} />
    </div>
  );
}
export interface TabsTriggerProps { value: string; children?: React.ReactNode; label?: string; badge?: string | number; disabled?: boolean; icon?: string; className?: string }
/** Deklaratif saja; dirender oleh TabsList. */
export function TabsTrigger(_props: TabsTriggerProps) {
  return null;
}
export function TabsContent({ value, className, children }: { value: string; className?: string; children: React.ReactNode }) {
  const { value: active } = useTabs();
  if (active !== value) return null;
  return <div className={cn("pt-4 focus:outline-none", className)}>{children}</div>;
}

// ---------- Table (kelas .bv-table dari bv/table-header.css: header solid primary, hairline baris) ----------
export function Table({ className, ...props }: React.TableHTMLAttributes<HTMLTableElement>) {
  return (
    <div className="bv-table-scroll w-full">
      <table className={cn("bv-table", className)} {...props} />
    </div>
  );
}
export const THead = (p: React.HTMLAttributes<HTMLTableSectionElement>) => <thead {...p} />;
export const TBody = (p: React.HTMLAttributes<HTMLTableSectionElement>) => <tbody {...p} />;
export const TR = ({ className, ...p }: React.HTMLAttributes<HTMLTableRowElement>) => <tr className={cn("transition-colors hover:bg-surface-container-low data-[state=selected]:bg-primary-soft", className)} {...p} />;
export const TH = ({ className, ...p }: React.ThHTMLAttributes<HTMLTableCellElement>) => <th className={className} {...p} />;
export const TD = ({ className, ...p }: React.TdHTMLAttributes<HTMLTableCellElement>) => <td className={className} {...p} />;

// ---------- Segmented (DS SegmentedButton; tone hanya sebagai aksen ikon, bukan latar) ----------
export function Segmented<T extends string>({ value, onChange, options, disabled }: { value?: T | null; onChange: (v: T) => void; options: { value: T; label: string; tone?: "success" | "critical" | "neutral" }[]; disabled?: boolean }) {
  return (
    <span className={cn(disabled && "pointer-events-none opacity-60")}>
      <SegmentedButton
        value={value ?? ""}
        onChange={(v) => onChange(v as T)}
        items={options.map((o) => ({ value: o.value, label: o.label, icon: o.tone === "success" ? <Icon name="check_circle" size={16} style={{ color: "var(--color-success)" }} /> : o.tone === "critical" ? <Icon name="cancel" size={16} style={{ color: "var(--color-error)" }} /> : undefined }))}
      />
    </span>
  );
}
