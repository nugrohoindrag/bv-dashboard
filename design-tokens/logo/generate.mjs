// Generator aset logo BuildingVision (mark "vision": 4 lensa bersarang + ekor) → SVG kanonik + PNG via sharp
// untuk dashboard (favicon), website (favicon + og), Tenant PWA, BVRooms app, Staff App (launcher Android/iOS).
import fs from "node:fs";
import path from "node:path";
import { createRequire } from "node:module";
import { fileURLToPath } from "node:url";
// sharp tidak dipasang di monorepo (berat); pakai: `npm i sharp --no-save` di folder ini atau NODE_PATH ke instalasi lain.
const require = createRequire(import.meta.url);
let sharp;
try { sharp = require("sharp"); } catch { console.error("sharp tidak ditemukan: jalankan `npm i sharp --no-save` di design-tokens/logo lalu ulangi"); process.exit(1); }

// Jalankan: node design-tokens/logo/generate.mjs — menulis aset ke monorepo ini dan repo saudara (../buildingvision-mobile-tenant, ../customer-booking-app, ../buildingvision-mobile-staff).
const ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../../..");
const BLUE = "#0442B9", INK = "#1B1B1B", WHITE = "#FFFFFF";
const PATH = [
  "M6 130H62", "M302 130H356",
  "M62 130C110 -35 254 -35 302 130", "M62 130C110 6 254 6 302 130", "M62 130C110 46 254 46 302 130", "M62 130C110 86 254 86 302 130",
  "M62 130C110 295 254 295 302 130", "M62 130C110 254 254 254 302 130", "M62 130C110 214 254 214 302 130", "M62 130C110 174 254 174 302 130",
].join(" ");

const markPath = (color, sw = 12) => `<path d="${PATH}" fill="none" stroke="${color}" stroke-width="${sw}" stroke-linecap="round" stroke-linejoin="round"/>`;

/** Mark saja (biru di transparan). */
const markSvg = (color = BLUE) => `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 362 260">${markPath(color)}</svg>`;

/** Ikon aplikasi: kotak biru radius ~22% dengan mark putih; `inset` untuk maskable/adaptive. */
function tileSvg({ size = 512, radius = 0.22, inset = 0.14, bg = BLUE, fg = WHITE, square = false } = {}) {
  const r = square ? 0 : Math.round(size * radius);
  const w = size * (1 - inset * 2), h = (w * 260) / 362;
  const x = (size - w) / 2, y = (size - h) / 2, sc = w / 362;
  return `<svg xmlns="http://www.w3.org/2000/svg" width="${size}" height="${size}" viewBox="0 0 ${size} ${size}"><rect width="${size}" height="${size}" rx="${r}" fill="${bg}"/><g transform="translate(${x} ${y}) scale(${sc})">${markPath(fg)}</g></svg>`;
}
/** Foreground adaptive (transparan) / background solid. */
const fgSvg = (size = 1024, inset = 0.3) => tileSvg({ size, inset, bg: "none", fg: WHITE, square: true }).replace(`<rect width="${size}" height="${size}" rx="0" fill="none"/>`, "");
const bgSvg = (size = 1024) => `<svg xmlns="http://www.w3.org/2000/svg" width="${size}" height="${size}"><rect width="${size}" height="${size}" fill="${BLUE}"/></svg>`;
/** Splash: latar putih, mark biru di tengah (28% lebar). */
function splashSvg(size = 2732, bg = WHITE, fg = BLUE) {
  const w = size * 0.28, h = (w * 260) / 362, x = (size - w) / 2, y = (size - h) / 2, sc = w / 362;
  return `<svg xmlns="http://www.w3.org/2000/svg" width="${size}" height="${size}"><rect width="${size}" height="${size}" fill="${bg}"/><g transform="translate(${x} ${y}) scale(${sc})">${markPath(fg)}</g></svg>`;
}
/** Lockup mark + wordmark (font sistem heavy sans; dipakai untuk SVG kanonik & og-image). */
function lockupSvg({ dark = false } = {}) {
  const word = dark ? WHITE : INK, accent = dark ? "#8FB3FF" : BLUE, mark = dark ? "#8FB3FF" : BLUE;
  return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1400 260" width="1400" height="260">
  <g>${markPath(mark)}</g>
  <text x="400" y="176" font-family="Montserrat, 'Segoe UI', Inter, Arial, sans-serif" font-weight="800" font-size="112" letter-spacing="3" fill="${word}">BUILDING <tspan fill="${accent}">VISION</tspan></text>
</svg>`;
}

const png = (svg, size, out, opts = {}) => sharp(Buffer.from(svg), { density: 300 }).resize(size, opts.height ?? size).png().toFile(out).then(() => console.log("png", path.relative(ROOT, out)));
const write = (file, s) => { fs.mkdirSync(path.dirname(file), { recursive: true }); fs.writeFileSync(file, s); console.log("svg", path.relative(ROOT, file)); };

// 1. kanonik di monorepo
const DT = `${ROOT}/buildingvision/design-tokens/logo`;
write(`${DT}/mark.svg`, markSvg());
write(`${DT}/mark-white.svg`, markSvg(WHITE));
write(`${DT}/logo.svg`, lockupSvg());
write(`${DT}/logo-dark.svg`, lockupSvg({ dark: true }));
write(`${DT}/app-icon.svg`, tileSvg());
await png(lockupSvg(), 1400, `${DT}/logo.png`, { height: 260 });

// 2. dashboard & website favicon (+ og image website)
const fav = tileSvg({ size: 64, radius: 0.22, inset: 0.12 });
write(`${ROOT}/buildingvision/web/public/favicon.svg`, fav);
write(`${ROOT}/buildingvision/website/public/favicon.svg`, fav);
await png(tileSvg({ size: 512, inset: 0.14 }), 180, `${ROOT}/buildingvision/web/public/apple-touch-icon.png`);
await png(tileSvg({ size: 512, inset: 0.14 }), 180, `${ROOT}/buildingvision/website/public/apple-touch-icon.png`);
{
  // og-image 1200x630: lockup di tengah latar putih
  const og = `<svg xmlns="http://www.w3.org/2000/svg" width="1200" height="630"><rect width="1200" height="630" fill="${WHITE}"/><g transform="translate(120 195) scale(0.685)">${lockupSvg().replace(/^<svg[^>]*>|<\/svg>$/g, "")}</g></svg>`;
  await png(og, 1200, `${ROOT}/buildingvision/website/public/images/og-image.png`, { height: 630 });
}

// 3. Tenant PWA + BVRooms app: ikon PWA + assets Capacitor
for (const app of [`${ROOT}/buildingvision-mobile-tenant`, `${ROOT}/customer-booking-app`]) {
  const icons = `${app}/public/icons`, assets = `${app}/assets`;
  write(`${icons}/icon.svg`, tileSvg({ size: 512 }));
  await png(tileSvg({ size: 512 }), 192, `${icons}/icon-192.png`);
  await png(tileSvg({ size: 512 }), 512, `${icons}/icon-512.png`);
  await png(tileSvg({ size: 512, inset: 0.24, square: true }), 512, `${icons}/icon-maskable-512.png`);
  fs.mkdirSync(assets, { recursive: true });
  await png(tileSvg({ size: 1024, square: true, inset: 0.16 }), 1024, `${assets}/icon-only.png`);
  await png(fgSvg(1024, 0.3), 1024, `${assets}/icon-foreground.png`);
  await png(bgSvg(1024), 1024, `${assets}/icon-background.png`);
  await png(splashSvg(2732), 2732, `${assets}/splash.png`);
  await png(splashSvg(2732), 2732, `${assets}/splash-dark.png`);
}

// 4. Staff App (Flutter): launcher Android mipmap + iOS AppIcon + logo di assets bv_ui
const staff = `${ROOT}/buildingvision-mobile-staff/app`;
const mip = { "mipmap-mdpi": 48, "mipmap-hdpi": 72, "mipmap-xhdpi": 96, "mipmap-xxhdpi": 144, "mipmap-xxxhdpi": 192 };
for (const [dir, size] of Object.entries(mip)) {
  const d = `${staff}/android/app/src/main/res/${dir}`;
  if (fs.existsSync(d)) await png(tileSvg({ size: 512, radius: 0.18 }), size, `${d}/ic_launcher.png`);
}
const ios = `${staff}/ios/Runner/Assets.xcassets/AppIcon.appiconset`;
if (fs.existsSync(`${ios}/Contents.json`)) {
  const c = JSON.parse(fs.readFileSync(`${ios}/Contents.json`, "utf8"));
  for (const img of c.images) {
    if (!img.filename) continue;
    const px = Math.round(parseFloat(img.size) * parseInt(img.scale));
    await png(tileSvg({ size: 1024, square: true, inset: 0.14 }), px, `${ios}/${img.filename}`);
  }
}
write(`${ROOT}/buildingvision-mobile-staff/packages/bv_ui/assets/images/logo_mark.svg`, markSvg());
await png(markSvg(), 724, `${ROOT}/buildingvision-mobile-staff/packages/bv_ui/assets/images/logo_mark.png`, { height: 520 });
console.log("DONE");
