// Gambar responsif (Website PRD §8.4): srcset WebP 480/960/1440 + JPEG fallback, lazy loading, LQIP sebagai latar.
import registry from "@/content/images.generated.json";

export type ImageKey = keyof typeof registry;

interface Props {
  image: ImageKey;
  className?: string;
  sizes?: string;
  priority?: boolean; // hero: eager + fetchpriority high
  aspect?: string; // mis. "16/10"
  rounded?: string;
}

export function Picture({ image, className = "", sizes = "(min-width: 1024px) 50vw, 100vw", priority, aspect = "16/10", rounded = "rounded-[var(--radius-xl)]" }: Props) {
  const meta = registry[image];
  const base = `/images/${image}`;
  return (
    <div className={`relative overflow-hidden ${rounded} bg-surface-container ${className}`} style={{ aspectRatio: aspect, backgroundImage: `url(${meta.lqip})`, backgroundSize: "cover" }}>
      <picture>
        <source type="image/webp" srcSet={`${base}-480.webp 480w, ${base}-960.webp 960w, ${base}-1440.webp 1440w`} sizes={sizes} />
        <img
          src={`${base}-960.jpg`}
          alt={meta.alt}
          width={meta.width}
          height={meta.height}
          loading={priority ? "eager" : "lazy"}
          decoding="async"
          fetchPriority={priority ? "high" : "auto"}
          className="h-full w-full object-cover"
        />
      </picture>
    </div>
  );
}

export function imageAlt(image: ImageKey): string {
  return registry[image].alt;
}
