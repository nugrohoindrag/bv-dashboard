// Primitif kecil untuk website: tombol, container, section, eyebrow. Warna hanya lewat alias token (site.css).
import type { AnchorHTMLAttributes, ButtonHTMLAttributes, ReactNode } from "react";
import { Link } from "react-router-dom";
import { Icon } from "./Icon";

type Variant = "primary" | "secondary" | "ghost" | "inverse";
const base = "inline-flex items-center justify-center gap-2 rounded-[var(--radius-pill)] font-semibold transition-colors focus-visible:outline-2 disabled:opacity-60 disabled:pointer-events-none";
const sizes = { md: "h-11 px-5 text-sm", lg: "h-12 px-6 text-base", sm: "h-9 px-4 text-sm" } as const;
const variants: Record<Variant, string> = {
  primary: "bg-primary text-on-primary hover:bg-primary-strong",
  secondary: "border border-border bg-surface text-on-surface hover:bg-surface-container-low",
  ghost: "text-primary hover:bg-primary-soft",
  inverse: "bg-surface text-primary hover:bg-surface-container-low",
};

export function cn(...parts: (string | false | null | undefined)[]) {
  return parts.filter(Boolean).join(" ");
}

interface BtnLinkProps extends AnchorHTMLAttributes<HTMLAnchorElement> {
  to: string;
  variant?: Variant;
  size?: keyof typeof sizes;
  icon?: string;
  trailing?: boolean;
  children: ReactNode;
}

/** Tautan bergaya tombol. `to` eksternal (http…) memakai <a>, internal memakai <Link>. */
export function ButtonLink({ to, variant = "primary", size = "md", icon, trailing = true, className, children, ...rest }: BtnLinkProps) {
  const cls = cn(base, sizes[size], variants[variant], className);
  const inner = (
    <>
      {icon && !trailing && <Icon name={icon} size={20} />}
      {children}
      {icon && trailing && <Icon name={icon} size={20} />}
    </>
  );
  if (/^https?:\/\//.test(to) || to.startsWith("mailto:")) {
    return <a href={to} className={cls} {...rest}>{inner}</a>;
  }
  return <Link to={to} className={cls} {...(rest as object)}>{inner}</Link>;
}

export function Button({ variant = "primary", size = "md", className, children, ...rest }: ButtonHTMLAttributes<HTMLButtonElement> & { variant?: Variant; size?: keyof typeof sizes }) {
  return <button className={cn(base, sizes[size], variants[variant], className)} {...rest}>{children}</button>;
}

export function Container({ className, children }: { className?: string; children: ReactNode }) {
  return <div className={cn("mx-auto w-full max-w-[1200px] px-5 sm:px-8", className)}>{children}</div>;
}

export function Section({ id, className, children, tone = "default" }: { id?: string; className?: string; children: ReactNode; tone?: "default" | "muted" | "band" }) {
  return (
    <section id={id} className={cn("py-16 sm:py-24", tone === "muted" && "bg-surface-container-low", tone === "band" && "bv-band", className)}>
      <Container>{children}</Container>
    </section>
  );
}

export function Eyebrow({ children, inverse }: { children: ReactNode; inverse?: boolean }) {
  return <div className={cn("mb-3 text-xs font-bold uppercase tracking-[0.14em]", inverse ? "text-on-primary/80" : "text-primary")}>{children}</div>;
}

export function Heading({ children, as: As = "h2", className, inverse }: { children: ReactNode; as?: "h1" | "h2" | "h3"; className?: string; inverse?: boolean }) {
  const size = As === "h1" ? "text-[40px] leading-[1.05] sm:text-[56px]" : As === "h2" ? "text-[30px] leading-tight sm:text-[40px]" : "text-[20px] leading-snug sm:text-[24px]";
  return <As className={cn("font-extrabold tracking-tight", size, inverse ? "text-on-primary" : "text-on-surface", className)}>{children}</As>;
}

export function Lead({ children, className, inverse }: { children: ReactNode; className?: string; inverse?: boolean }) {
  return <p className={cn("mt-4 max-w-2xl text-base leading-relaxed sm:text-lg", inverse ? "text-on-primary/85" : "text-on-surface-variant", className)}>{children}</p>;
}

export function Card({ className, children }: { className?: string; children: ReactNode }) {
  return <div className={cn("rounded-[var(--radius-xl)] border border-border bg-surface p-6", className)}>{children}</div>;
}

export function Check({ children }: { children: ReactNode }) {
  return (
    <li className="flex items-start gap-2.5 text-sm text-on-surface">
      <Icon name="check_circle" size={20} className="mt-0.5 shrink-0 text-primary" />
      <span>{children}</span>
    </li>
  );
}
