// Foundation layer (DS §1.2): primitive shadcn-style di atas Radix, di-styling meniru Metronic Demo 10 dengan token BuildingVision.
// Semua warna lewat token semantic; feature code tidak boleh memakai warna mentah (DS §7.5).
import * as React from "react";
import { Slot } from "@radix-ui/react-slot";
import * as DialogPrimitive from "@radix-ui/react-dialog";
import * as PopoverPrimitive from "@radix-ui/react-popover";
import * as DropdownPrimitive from "@radix-ui/react-dropdown-menu";
import * as TabsPrimitive from "@radix-ui/react-tabs";
import * as TooltipPrimitive from "@radix-ui/react-tooltip";
import * as CheckboxPrimitive from "@radix-ui/react-checkbox";
import * as AlertDialogPrimitive from "@radix-ui/react-alert-dialog";
import { cva, type VariantProps } from "class-variance-authority";
import { Check, Loader2, X } from "lucide-react";
import { cn } from "@/lib/utils";
import { initials } from "@/lib/format";

// ---------- Button ----------
export const buttonVariants = cva(
  "inline-flex items-center justify-center gap-2 whitespace-nowrap rounded-md text-body font-medium transition-colors disabled:pointer-events-none disabled:opacity-50 [&_svg]:size-4 [&_svg]:shrink-0",
  {
    variants: {
      variant: {
        primary: "bg-primary text-primary-foreground hover:bg-brand-700 shadow-card",
        secondary: "bg-card text-foreground border border-border hover:bg-muted",
        ghost: "hover:bg-muted text-foreground",
        destructive: "bg-destructive text-white hover:bg-critical-text",
        link: "text-brand-600 underline-offset-4 hover:underline h-auto px-0",
        success: "bg-success text-white hover:opacity-90",
      },
      size: { sm: "h-8 px-3 text-sm", md: "h-9 px-4", lg: "h-10 px-6", icon: "h-9 w-9", "icon-sm": "h-8 w-8" },
    },
    defaultVariants: { variant: "primary", size: "md" },
  },
);
export interface ButtonProps extends React.ButtonHTMLAttributes<HTMLButtonElement>, VariantProps<typeof buttonVariants> {
  asChild?: boolean;
  loading?: boolean;
}
export const Button = React.forwardRef<HTMLButtonElement, ButtonProps>(({ className, variant, size, asChild, loading, children, disabled, ...props }, ref) => {
  const Comp = asChild ? Slot : "button";
  return (
    <Comp ref={ref} className={cn(buttonVariants({ variant, size }), className)} disabled={disabled || loading} {...props}>
      {loading && <Loader2 className="animate-spin" aria-hidden />}
      {children}
    </Comp>
  );
});
Button.displayName = "Button";

// ---------- Badge (tinggi 22px, radius full, text-xs 500 — DS §4.1) ----------
export function Badge({ className, dot, children, ...props }: React.HTMLAttributes<HTMLSpanElement> & { dot?: boolean }) {
  return (
    <span className={cn("inline-flex h-[22px] items-center gap-1 whitespace-nowrap rounded-full px-2 text-xs font-medium leading-none", className)} {...props}>
      {dot && (
        <span aria-hidden className="text-[8px]">
          ●
        </span>
      )}
      {children}
    </span>
  );
}

// ---------- Card ----------
export function Card({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) {
  return <div className={cn("rounded-lg border border-border bg-card shadow-card", className)} {...props} />;
}
export function CardHeader({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) {
  return <div className={cn("flex items-start justify-between gap-3 px-5 pt-4 pb-2", className)} {...props} />;
}
export function CardTitle({ className, ...props }: React.HTMLAttributes<HTMLHeadingElement>) {
  return <h2 className={cn("text-h2 font-semibold text-foreground", className)} {...props} />;
}
export function CardSubtitle({ className, ...props }: React.HTMLAttributes<HTMLParagraphElement>) {
  return <p className={cn("text-sm text-muted-foreground", className)} {...props} />;
}
export function CardContent({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) {
  return <div className={cn("px-5 pb-5", className)} {...props} />;
}
export function CardFooter({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) {
  return <div className={cn("flex items-center justify-between border-t border-border px-5 py-3 text-sm text-muted-foreground", className)} {...props} />;
}

// ---------- Form controls ----------
const inputClass =
  "flex h-9 w-full rounded-md border border-border bg-card px-3 py-1 text-body text-foreground shadow-card transition-colors placeholder:text-muted-foreground focus-visible:border-brand-500 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/30 disabled:cursor-not-allowed disabled:opacity-50 aria-[invalid=true]:border-critical";
export const Input = React.forwardRef<HTMLInputElement, React.InputHTMLAttributes<HTMLInputElement>>(({ className, ...props }, ref) => <input ref={ref} className={cn(inputClass, className)} {...props} />);
Input.displayName = "Input";
export const Textarea = React.forwardRef<HTMLTextAreaElement, React.TextareaHTMLAttributes<HTMLTextAreaElement>>(({ className, ...props }, ref) => (
  <textarea ref={ref} className={cn(inputClass, "h-auto min-h-[80px] py-2", className)} {...props} />
));
Textarea.displayName = "Textarea";
export const NativeSelect = React.forwardRef<HTMLSelectElement, React.SelectHTMLAttributes<HTMLSelectElement>>(({ className, children, ...props }, ref) => (
  <select ref={ref} className={cn(inputClass, "pr-8", className)} {...props}>
    {children}
  </select>
));
NativeSelect.displayName = "NativeSelect";
export function Label({ className, required, children, ...props }: React.LabelHTMLAttributes<HTMLLabelElement> & { required?: boolean }) {
  return (
    <label className={cn("mb-1 block text-sm font-medium text-foreground", className)} {...props}>
      {children}
      {required && (
        <span className="ml-0.5 text-critical" aria-hidden>
          *
        </span>
      )}
    </label>
  );
}
export function FieldError({ children }: { children?: React.ReactNode }) {
  return children ? <p className="mt-1 text-xs text-critical-text">{children}</p> : null;
}
export function HelperText({ children }: { children?: React.ReactNode }) {
  return children ? <p className="mt-1 text-xs text-muted-foreground">{children}</p> : null;
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

export const Checkbox = React.forwardRef<React.ElementRef<typeof CheckboxPrimitive.Root>, React.ComponentPropsWithoutRef<typeof CheckboxPrimitive.Root>>(({ className, ...props }, ref) => (
  <CheckboxPrimitive.Root ref={ref} className={cn("peer h-4 w-4 shrink-0 rounded-sm border border-neutral-300 bg-card data-[state=checked]:border-primary data-[state=checked]:bg-primary data-[state=checked]:text-white", className)} {...props}>
    <CheckboxPrimitive.Indicator className="flex items-center justify-center">
      <Check className="h-3 w-3" />
    </CheckboxPrimitive.Indicator>
  </CheckboxPrimitive.Root>
));
Checkbox.displayName = "Checkbox";

// ---------- Skeleton / Separator / Avatar ----------
export function Skeleton({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) {
  return <div className={cn("animate-pulse rounded-md bg-muted", className)} {...props} />;
}
export function Separator({ className, vertical }: { className?: string; vertical?: boolean }) {
  return <div role="separator" className={cn(vertical ? "h-full w-px" : "h-px w-full", "bg-border", className)} />;
}
export function Avatar({ name, className, size = 28 }: { name?: string | null; className?: string; size?: number }) {
  return (
    <span aria-hidden className={cn("inline-flex shrink-0 items-center justify-center rounded-full bg-brand-100 text-[11px] font-semibold text-brand-900", className)} style={{ width: size, height: size }}>
      {initials(name)}
    </span>
  );
}

// ---------- Alert ----------
export function Alert({ variant = "info", title, children, className, action }: { variant?: "info" | "success" | "warning" | "critical"; title?: string; children?: React.ReactNode; className?: string; action?: React.ReactNode }) {
  const map = { info: "border-info/30 bg-info-soft text-info-text", success: "border-success/30 bg-success-soft text-success-text", warning: "border-warning/30 bg-warning-soft text-warning-text", critical: "border-critical/30 bg-critical-soft text-critical-text" };
  return (
    <div role="alert" className={cn("flex items-start justify-between gap-3 rounded-md border px-4 py-3 text-sm", map[variant], className)}>
      <div>
        {title && <div className="font-semibold">{title}</div>}
        {children && <div>{children}</div>}
      </div>
      {action}
    </div>
  );
}

// ---------- Dialog ----------
export const Dialog = DialogPrimitive.Root;
export const DialogTrigger = DialogPrimitive.Trigger;
export const DialogClose = DialogPrimitive.Close;
export function DialogContent({ className, children, title, description, side, ...props }: React.ComponentPropsWithoutRef<typeof DialogPrimitive.Content> & { title?: string; description?: string; side?: "center" | "right" }) {
  return (
    <DialogPrimitive.Portal>
      <DialogPrimitive.Overlay className="fixed inset-0 z-40 bg-neutral-900/40 data-[state=open]:animate-in data-[state=open]:fade-in-0" />
      <DialogPrimitive.Content
        className={cn(
          "fixed z-50 bg-card shadow-modal focus:outline-none",
          side === "right"
            ? "inset-y-0 right-0 flex w-[560px] max-w-full flex-col border-l border-border" /* Drawer kanan 560px (DS §5.3) */
            : "left-1/2 top-1/2 w-full max-w-lg -translate-x-1/2 -translate-y-1/2 rounded-lg border border-border",
          className,
        )}
        {...props}
      >
        {(title || description) && (
          <div className="flex items-start justify-between gap-3 border-b border-border px-6 py-4">
            <div>
              {title && <DialogPrimitive.Title className="text-h2 font-semibold">{title}</DialogPrimitive.Title>}
              {description && <DialogPrimitive.Description className="mt-0.5 text-sm text-muted-foreground">{description}</DialogPrimitive.Description>}
            </div>
            <DialogPrimitive.Close className="rounded-md p-1 text-muted-foreground hover:bg-muted" aria-label="Tutup">
              <X className="h-4 w-4" />
            </DialogPrimitive.Close>
          </div>
        )}
        <div className={cn("px-6 py-4", side === "right" && "flex-1 overflow-y-auto")}>{children}</div>
      </DialogPrimitive.Content>
    </DialogPrimitive.Portal>
  );
}
export function DialogFooter({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) {
  return <div className={cn("-mx-6 -mb-4 mt-4 flex justify-end gap-2 border-t border-border bg-muted/40 px-6 py-3", className)} {...props} />;
}

// ---------- AlertDialog (ConfirmDialog — DS §4) ----------
export function ConfirmDialog({ open, onOpenChange, title, description, confirmLabel = "Ya, lanjutkan", cancelLabel = "Batal", destructive, onConfirm, loading }: { open: boolean; onOpenChange: (o: boolean) => void; title: string; description?: string; confirmLabel?: string; cancelLabel?: string; destructive?: boolean; onConfirm: () => void; loading?: boolean }) {
  return (
    <AlertDialogPrimitive.Root open={open} onOpenChange={onOpenChange}>
      <AlertDialogPrimitive.Portal>
        <AlertDialogPrimitive.Overlay className="fixed inset-0 z-40 bg-neutral-900/40" />
        <AlertDialogPrimitive.Content className="fixed left-1/2 top-1/2 z-50 w-full max-w-md -translate-x-1/2 -translate-y-1/2 rounded-lg border border-border bg-card p-6 shadow-modal">
          <AlertDialogPrimitive.Title className="text-h2 font-semibold">{title}</AlertDialogPrimitive.Title>
          {description && <AlertDialogPrimitive.Description className="mt-2 text-sm text-muted-foreground">{description}</AlertDialogPrimitive.Description>}
          <div className="mt-6 flex justify-end gap-2">
            <AlertDialogPrimitive.Cancel asChild>
              <Button variant="ghost">{cancelLabel}</Button>
            </AlertDialogPrimitive.Cancel>
            <Button variant={destructive ? "destructive" : "primary"} onClick={onConfirm} loading={loading}>
              {confirmLabel}
            </Button>
          </div>
        </AlertDialogPrimitive.Content>
      </AlertDialogPrimitive.Portal>
    </AlertDialogPrimitive.Root>
  );
}

// ---------- Popover / Dropdown / Tooltip ----------
export const Popover = PopoverPrimitive.Root;
export const PopoverTrigger = PopoverPrimitive.Trigger;
export function PopoverContent({ className, align = "end", ...props }: React.ComponentPropsWithoutRef<typeof PopoverPrimitive.Content>) {
  return (
    <PopoverPrimitive.Portal>
      <PopoverPrimitive.Content align={align} sideOffset={6} className={cn("z-50 rounded-lg border border-border bg-card p-2 shadow-popover outline-none", className)} {...props} />
    </PopoverPrimitive.Portal>
  );
}
export const DropdownMenu = DropdownPrimitive.Root;
export const DropdownMenuTrigger = DropdownPrimitive.Trigger;
export function DropdownMenuContent({ className, align = "end", ...props }: React.ComponentPropsWithoutRef<typeof DropdownPrimitive.Content>) {
  return (
    <DropdownPrimitive.Portal>
      <DropdownPrimitive.Content align={align} sideOffset={6} className={cn("z-50 min-w-[180px] rounded-lg border border-border bg-card p-1 shadow-popover", className)} {...props} />
    </DropdownPrimitive.Portal>
  );
}
export function DropdownMenuItem({ className, destructive, ...props }: React.ComponentPropsWithoutRef<typeof DropdownPrimitive.Item> & { destructive?: boolean }) {
  return <DropdownPrimitive.Item className={cn("flex cursor-pointer select-none items-center gap-2 rounded-md px-2 py-1.5 text-body outline-none hover:bg-muted data-[disabled]:pointer-events-none data-[disabled]:opacity-50 [&_svg]:size-4", destructive && "text-critical-text", className)} {...props} />;
}
export const DropdownMenuSeparator = () => <DropdownPrimitive.Separator className="my-1 h-px bg-border" />;
export const DropdownMenuLabel = ({ children }: { children: React.ReactNode }) => <div className="px-2 py-1 text-xs uppercase tracking-wide text-muted-foreground">{children}</div>;

export const TooltipProvider = TooltipPrimitive.Provider;
export function Tooltip({ content, children }: { content: React.ReactNode; children: React.ReactNode }) {
  return (
    <TooltipPrimitive.Root delayDuration={300}>
      <TooltipPrimitive.Trigger asChild>{children}</TooltipPrimitive.Trigger>
      <TooltipPrimitive.Portal>
        <TooltipPrimitive.Content sideOffset={4} className="z-50 rounded-md bg-neutral-text px-2 py-1 text-xs text-white shadow-popover">
          {content}
        </TooltipPrimitive.Content>
      </TooltipPrimitive.Portal>
    </TooltipPrimitive.Root>
  );
}

// ---------- Tabs ----------
export const Tabs = TabsPrimitive.Root;
export function TabsList({ className, ...props }: React.ComponentPropsWithoutRef<typeof TabsPrimitive.List>) {
  return <TabsPrimitive.List className={cn("flex items-center gap-1 border-b border-border", className)} {...props} />;
}
export function TabsTrigger({ className, ...props }: React.ComponentPropsWithoutRef<typeof TabsPrimitive.Trigger>) {
  return <TabsPrimitive.Trigger className={cn("-mb-px border-b-2 border-transparent px-3 py-2 text-body text-muted-foreground hover:text-foreground data-[state=active]:border-primary data-[state=active]:font-medium data-[state=active]:text-foreground", className)} {...props} />;
}
export function TabsContent({ className, ...props }: React.ComponentPropsWithoutRef<typeof TabsPrimitive.Content>) {
  return <TabsPrimitive.Content className={cn("pt-4 focus:outline-none", className)} {...props} />;
}

// ---------- Table (basis DataGrid, acuan Metronic DataGrid) ----------
export function Table({ className, ...props }: React.TableHTMLAttributes<HTMLTableElement>) {
  return (
    <div className="w-full overflow-x-auto">
      <table className={cn("w-full caption-bottom text-body", className)} {...props} />
    </div>
  );
}
export const THead = (p: React.HTMLAttributes<HTMLTableSectionElement>) => <thead className="bg-muted text-xs uppercase tracking-[0.04em] text-muted-foreground" {...p} />;
export const TBody = (p: React.HTMLAttributes<HTMLTableSectionElement>) => <tbody className="[&_tr:last-child]:border-0" {...p} />;
export const TR = ({ className, ...p }: React.HTMLAttributes<HTMLTableRowElement>) => <tr className={cn("border-b border-border transition-colors hover:bg-brand-50/60 data-[state=selected]:bg-brand-100/60", className)} {...p} />;
export const TH = ({ className, ...p }: React.ThHTMLAttributes<HTMLTableCellElement>) => <th className={cn("h-10 px-3 text-left align-middle font-medium", className)} {...p} />;
export const TD = ({ className, ...p }: React.TdHTMLAttributes<HTMLTableCellElement>) => <td className={cn("px-3 py-2.5 align-middle", className)} {...p} />;

// ---------- Segmented control (ChecklistRunner) ----------
export function Segmented<T extends string>({ value, onChange, options, disabled }: { value?: T | null; onChange: (v: T) => void; options: { value: T; label: string; tone?: "success" | "critical" | "neutral" }[]; disabled?: boolean }) {
  return (
    <div className="inline-flex rounded-md border border-border bg-card p-0.5">
      {options.map((o) => {
        const active = value === o.value;
        const tone = o.tone === "success" ? "bg-success text-white" : o.tone === "critical" ? "bg-critical text-white" : "bg-neutral text-white";
        return (
          <button key={o.value} type="button" disabled={disabled} onClick={() => onChange(o.value)} className={cn("rounded px-3 py-1 text-sm transition-colors", active ? tone : "text-muted-foreground hover:bg-muted", disabled && "opacity-60")} aria-pressed={active}>
            {o.label}
          </button>
        );
      })}
    </div>
  );
}
