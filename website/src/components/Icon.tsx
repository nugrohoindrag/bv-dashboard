// Material Symbols Rounded (subset font dari packages/ui; nama ikon di website ikut dipindai scripts/vendor-fonts.py).
export function Icon({ name, size = 24, className = "", fill }: { name: string; size?: number; className?: string; fill?: boolean }) {
  return (
    <span aria-hidden="true" className={`icon ${fill ? "icon-fill" : ""} ${className}`} style={{ fontSize: size, width: size, height: size }}>
      {name}
    </span>
  );
}
