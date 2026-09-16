// SEO head management tanpa dependensi (Website PRD §39): title, description, canonical, Open Graph, JSON-LD.
// SSR (prerender): <Seo> menulis ke HeadContext yang dikumpulkan entry-server; client: sinkron ke document.head.
import { createContext, useContext, useEffect } from "react";
import { SITE_NAME, SITE_URL } from "./config";

export interface SeoProps {
  title: string;
  description: string;
  path: string; // "/solutions/hotel"
  image?: string; // path absolut atau relatif /images/...
  type?: "website" | "article";
  noindex?: boolean;
  jsonLd?: Record<string, unknown> | Record<string, unknown>[];
}

export interface HeadCollector {
  current: SeoProps | null;
}

export const HeadContext = createContext<HeadCollector | null>(null);

const DEFAULT_IMAGE = "/images/hero-towers-1440.webp";

export function renderHead(p: SeoProps): string {
  const title = p.title.includes(SITE_NAME) ? p.title : `${p.title} | ${SITE_NAME}`;
  const url = SITE_URL + (p.path === "/" ? "/" : p.path.replace(/\/$/, ""));
  const img = p.image?.startsWith("http") ? p.image : SITE_URL + (p.image ?? DEFAULT_IMAGE);
  const esc = (s: string) => s.replace(/&/g, "&amp;").replace(/"/g, "&quot;").replace(/</g, "&lt;");
  const tags = [
    `<title>${esc(title)}</title>`,
    `<meta name="description" content="${esc(p.description)}" />`,
    `<link rel="canonical" href="${esc(url)}" />`,
    `<meta property="og:site_name" content="${SITE_NAME}" />`,
    `<meta property="og:type" content="${p.type ?? "website"}" />`,
    `<meta property="og:title" content="${esc(title)}" />`,
    `<meta property="og:description" content="${esc(p.description)}" />`,
    `<meta property="og:url" content="${esc(url)}" />`,
    `<meta property="og:image" content="${esc(img)}" />`,
    `<meta name="twitter:card" content="summary_large_image" />`,
    `<meta name="twitter:title" content="${esc(title)}" />`,
    `<meta name="twitter:description" content="${esc(p.description)}" />`,
    `<meta name="twitter:image" content="${esc(img)}" />`,
  ];
  if (p.noindex) tags.push(`<meta name="robots" content="noindex" />`);
  const ld = p.jsonLd ? (Array.isArray(p.jsonLd) ? p.jsonLd : [p.jsonLd]) : [];
  for (const item of ld) tags.push(`<script type="application/ld+json">${JSON.stringify(item).replace(/</g, "\\u003c")}</script>`);
  return tags.join("\n    ");
}

function syncClientHead(p: SeoProps) {
  const title = p.title.includes(SITE_NAME) ? p.title : `${p.title} | ${SITE_NAME}`;
  document.title = title;
  const url = SITE_URL + (p.path === "/" ? "/" : p.path.replace(/\/$/, ""));
  const img = p.image?.startsWith("http") ? p.image : SITE_URL + (p.image ?? DEFAULT_IMAGE);
  const set = (sel: string, attrs: Record<string, string>) => {
    let el = document.head.querySelector<HTMLElement>(sel);
    if (!el) {
      el = document.createElement(sel.startsWith("link") ? "link" : "meta");
      document.head.appendChild(el);
      const m = sel.match(/\[(\w+(?::\w+)?)="([^"]+)"\]/);
      if (m) el.setAttribute(m[1], m[2]);
    }
    for (const [k, v] of Object.entries(attrs)) el.setAttribute(k, v);
  };
  set('meta[name="description"]', { content: p.description });
  set('link[rel="canonical"]', { href: url });
  set('meta[property="og:title"]', { content: title });
  set('meta[property="og:description"]', { content: p.description });
  set('meta[property="og:url"]', { content: url });
  set('meta[property="og:image"]', { content: img });
  set('meta[property="og:type"]', { content: p.type ?? "website" });
  set('meta[name="twitter:title"]', { content: title });
  set('meta[name="twitter:description"]', { content: p.description });
  set('meta[name="twitter:image"]', { content: img });
  const robots = document.head.querySelector('meta[name="robots"]');
  if (p.noindex) set('meta[name="robots"]', { content: "noindex" });
  else robots?.remove();
}

export function Seo(p: SeoProps) {
  const ctx = useContext(HeadContext);
  if (ctx && typeof window === "undefined") ctx.current = p; // SSR: kumpulkan
  useEffect(() => {
    syncClientHead(p);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [p.title, p.description, p.path, p.image, p.noindex]);
  return null;
}
