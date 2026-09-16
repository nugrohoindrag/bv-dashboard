// Website PRD §38: em dash (U+2014) dilarang di seluruh copy yang dilihat pengguna. Memindai src/ (konten, komponen) dan dist/ (HTML hasil prerender).
import fs from "node:fs/promises";
import path from "node:path";

const root = path.resolve(import.meta.dirname, "..");
const targets = [path.join(root, "src"), path.join(root, "dist")];
let bad = 0;
async function walk(dir) {
  let entries;
  try {
    entries = await fs.readdir(dir, { withFileTypes: true });
  } catch {
    return;
  }
  for (const e of entries) {
    const p = path.join(dir, e.name);
    if (e.isDirectory()) {
      if (e.name === "assets" || e.name === "images") continue;
      await walk(p);
    } else if (/\.(tsx?|json|html|mjs)$/.test(e.name)) {
      const text = await fs.readFile(p, "utf8");
      const lines = text.split("\n");
      lines.forEach((l, i) => {
        if (l.includes("—")) {
          bad++;
          console.log(`${path.relative(root, p)}:${i + 1}: em dash found`);
        }
      });
    }
  }
}
for (const t of targets) await walk(t);
if (bad) {
  console.error(`check-copy: ${bad} line(s) contain an em dash (Website PRD §38)`);
  process.exit(1);
}
console.log("check-copy: no em dash in website copy");
