// Website PRD v1.1 §8: unduh foto open-source (Unsplash License) dari manifest lalu hasilkan varian responsif WebP + JPEG.
// Jalankan: `npm run images` (butuh jaringan sekali; hasil di public/images/ di-commit agar build CI tidak perlu jaringan).
import fs from "node:fs/promises";
import path from "node:path";
import sharp from "sharp";

const root = path.resolve(import.meta.dirname, "..");
const manifest = JSON.parse(await fs.readFile(path.join(root, "src/content/images.json"), "utf8"));
const outDir = path.join(root, "public/images");
const cacheDir = path.join(root, ".image-cache");
await fs.mkdir(outDir, { recursive: true });
await fs.mkdir(cacheDir, { recursive: true });
const WIDTHS = [480, 960, 1440];

for (const img of manifest.images) {
  const id = img.source.split("/photos/")[1];
  const src = path.join(cacheDir, `${img.key}.jpg`);
  try {
    await fs.access(src);
  } catch {
    const url = `https://images.unsplash.com/photo-${id}?w=1600&q=82&fm=jpg&fit=max`;
    const res = await fetch(url);
    if (!res.ok) throw new Error(`${img.key}: ${res.status} ${url}`);
    await fs.writeFile(src, Buffer.from(await res.arrayBuffer()));
    console.log("downloaded", img.key);
  }
  const base = sharp(src).rotate();
  const meta = await base.metadata();
  for (const w of WIDTHS) {
    await base.clone().resize({ width: w, withoutEnlargement: true }).webp({ quality: 74 }).toFile(path.join(outDir, `${img.key}-${w}.webp`));
  }
  await base.clone().resize({ width: 960, withoutEnlargement: true }).jpeg({ quality: 76, mozjpeg: true }).toFile(path.join(outDir, `${img.key}-960.jpg`));
  // placeholder blur kecil (LQIP) untuk latar saat memuat
  const tiny = await base.clone().resize({ width: 24 }).webp({ quality: 40 }).toBuffer();
  img.lqip = `data:image/webp;base64,${tiny.toString("base64")}`;
  img.width = meta.width;
  img.height = meta.height;
  console.log("processed", img.key, meta.width, "x", meta.height);
}
// registry untuk komponen <Picture>: key → alt, dimensi, lqip
const registry = Object.fromEntries(manifest.images.map((i) => [i.key, { alt: i.alt, width: i.width, height: i.height, lqip: i.lqip }]));
await fs.writeFile(path.join(root, "src/content/images.generated.json"), JSON.stringify(registry, null, 1));
console.log("done", manifest.images.length, "images");
