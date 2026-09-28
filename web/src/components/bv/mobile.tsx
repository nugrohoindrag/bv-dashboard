// Mobile action component (PRD P0 §20.3, §22): interaksi utama di mobile berorientasi aksi.
// MobileActionBar = satu aksi utama (mis. "Mulai" / Start Task) menempel di bawah + aksi sekunder di overflow (BottomSheet DS).
// Fab = aksi "Buat" pada halaman daftar. Keduanya hanya tampil < 768px (md) dan sadar safe-area.
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { BottomSheet, FAB, Icon } from "@buildingvision/ui";
import { Button } from "@/components/ui/primitives";
import { cn } from "@/lib/utils";

export interface MobileAction {
  label: string;
  onSelect: () => void;
  icon?: string;
  loading?: boolean;
  disabled?: boolean;
  destructive?: boolean;
}

export function MobileActionBar({ primary, secondary = [], preview, className }: { primary?: MobileAction | null; secondary?: MobileAction[]; /** Katalog DS: tampil statis di semua lebar. */ preview?: boolean; className?: string }) {
  const { t } = useTranslation();
  const [more, setMore] = useState(false);
  if (!primary && secondary.length === 0) return null;
  const bar = (
    <div
      data-testid="mobile-action-bar"
      className={cn(
        "flex items-center gap-2 border-t border-border bg-surface px-4 pt-3 pb-safe",
        preview ? "rounded-b-[var(--radius-lg)]" : "fixed inset-x-0 bottom-0 z-[900] md:hidden",
        className,
      )}
      style={{ boxShadow: preview ? undefined : "var(--elevation-2)" }}
    >
      {secondary.length > 0 && (
        <Button variant="secondary" size="icon" aria-label={t("action.more_actions")} onClick={() => setMore(true)}>
          <Icon name="more_horiz" size={20} />
        </Button>
      )}
      {primary && (
        <Button size="lg" className="flex-1 justify-center" icon={primary.icon} loading={primary.loading} disabled={primary.disabled} variant={primary.destructive ? "destructive" : "primary"} onClick={primary.onSelect}>
          {primary.label}
        </Button>
      )}
      <BottomSheet isOpen={more} onClose={() => setMore(false)} title={t("action.more_actions")}>
        <div className="flex flex-col gap-1" role="menu">
          {secondary.map((a) => (
            <button
              key={a.label}
              type="button"
              role="menuitem"
              disabled={a.disabled}
              onClick={() => { setMore(false); a.onSelect(); }}
              className={cn("flex min-h-12 items-center gap-3 rounded-[var(--radius-md)] px-3 text-left text-body hover:bg-surface-container disabled:opacity-50", a.destructive ? "text-error" : "text-on-surface")}
            >
              {a.icon && <Icon name={a.icon} size={20} />}
              {a.label}
            </button>
          ))}
        </div>
      </BottomSheet>
    </div>
  );
  if (preview) return bar;
  return (
    <>
      {/* spacer agar konten terakhir tidak tertutup bar */}
      <div className="h-24 md:hidden" aria-hidden />
      {bar}
    </>
  );
}

export function Fab({ label, icon = "add", onClick, preview, className, "aria-label": ariaLabel }: { label?: string; icon?: string; onClick: () => void; preview?: boolean; className?: string; "aria-label"?: string }) {
  return (
    <>
    {!preview && <div className="h-20 md:hidden" aria-hidden />}
    <div className={cn(preview ? "inline-flex" : "fixed bottom-[max(16px,env(safe-area-inset-bottom))] right-4 z-[900] md:hidden", className)} data-testid="fab">
      <FAB
        icon={<Icon name={icon} size={24} />}
        label={label}
        aria-label={ariaLabel ?? label}
        onClick={onClick}
        style={{ backgroundColor: "var(--color-primary)", color: "var(--color-on-primary)" }}
      />
    </div>
    </>
  );
}
