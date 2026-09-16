// Prerender seluruh route statis ke dist/<route>/index.html + sitemap.xml + robots.txt (Website PRD §39).
// Dijalankan setelah `vite build` (client) dan `vite build --ssr` (dist-ssr/entry-server.js).
import fs from "node:fs/promises";
import path from "node:path";
import { pathToFileURL } from "node:url";

const root = path.resolve(import.meta.dirname, "..");
const dist = path.join(root, "dist");
const template = await fs.readFile(path.join(dist, "index.html"), "utf8");
const { render, ALL_ROUTES: routes } = await import(pathToFileURL(path.join(root, "dist-ssr", "entry-server.js")).href);
const siteUrl = (process.env.VITE_SITE_URL || "https://buildingvision.id").replace(/\/$/, "");
const NOINDEX = new Set(["/start-free-trial", "/login", "/404"]);

let n = 0;
for (const route of routes) {
  const { html, head } = render(route);
  const page = template.replace("<!--app-head-->", head).replace("<!--app-html-->", html);
  const file = route === "/" ? path.join(dist, "index.html") : path.join(dist, route.slice(1), "index.html");
  await fs.mkdir(path.dirname(file), { recursive: true });
  await fs.writeFile(file, page);
  n++;
}
// 404 fallback untuk host statis (Caddy: try_files … /404/index.html)
await fs.copyFile(path.join(dist, "404", "index.html"), path.join(dist, "404.html"));
const today = new Date().toISOString().slice(0, 10);
const sitemap = `<?xml version="1.0" encoding="UTF-8"?>\n<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">\n${routes
  .filter((r) => !NOINDEX.has(r))
  .map((r) => `  <url><loc>${siteUrl}${r === "/" ? "/" : r}</loc><lastmod>${today}</lastmod><changefreq>${r === "/" ? "weekly" : "monthly"}</changefreq><priority>${r === "/" ? "1.0" : r.startsWith("/solutions") || r === "/pricing" || r === "/download" ? "0.9" : "0.7"}</priority></url>`)
  .join("\n")}\n</urlset>\n`;
await fs.writeFile(path.join(dist, "sitemap.xml"), sitemap);
await fs.writeFile(path.join(dist, "robots.txt"), `User-agent: *\nAllow: /\nDisallow: /login\nDisallow: /start-free-trial\nSitemap: ${siteUrl}/sitemap.xml\n`);
console.log(`prerendered ${n} routes → dist/`);
