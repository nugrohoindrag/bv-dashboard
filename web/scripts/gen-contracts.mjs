// Generate tokens.css (web), status-map.ts (web), tokens.dart & status_map.dart (mobile, di contracts/build)
// dari design-tokens/tokens.json dan contracts/status-map.yaml (Design System §2, §7.2–7.3; TAD §10).
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import YAML from "yaml";

const here = path.dirname(fileURLToPath(import.meta.url));
const root = path.resolve(here, "../..");
const tokens = JSON.parse(fs.readFileSync(path.join(root, "design-tokens/tokens.json"), "utf8")).bv;
const statusMap = YAML.parse(fs.readFileSync(path.join(root, "contracts/status-map.yaml"), "utf8"));

const header = "/* GENERATED — jangan edit manual. Sumber: design-tokens/tokens.json. Jalankan `npm run gen`. */\n";

// ---------- tokens.css ----------
let css = header + ":root {\n";
for (const group of ["brand", "success", "warning", "critical", "info", "neutral"]) {
  for (const [k, v] of Object.entries(tokens[group])) css += `  --bv-${group}-${k}: ${v.$value};\n`;
}
for (const [k, v] of Object.entries(tokens.surface)) css += `  --bv-surface-${k}: ${v.$value};\n`;
for (const [k, v] of Object.entries(tokens.space)) css += `  --bv-space-${k}: ${v};\n`;
for (const [k, v] of Object.entries(tokens.radius)) css += `  --bv-radius-${k}: ${v};\n`;
for (const [k, v] of Object.entries(tokens.shadow)) css += `  --bv-shadow-${k}: ${v};\n`;
css += `  --bv-font-sans: ${tokens.font.sans};\n  --bv-font-mono: ${tokens.font.mono};\n`;
for (const [k, v] of Object.entries(tokens.type)) css += `  --bv-type-${k}-size: ${v.size}; --bv-type-${k}-line: ${v.line}; --bv-type-${k}-weight: ${v.weight};\n`;
css += "}\n";
fs.mkdirSync(path.join(here, "../src/styles"), { recursive: true });
fs.writeFileSync(path.join(here, "../src/styles/tokens.css"), css);

// ---------- status-map.ts ----------
let ts = "// GENERATED — jangan edit manual. Sumber: contracts/status-map.yaml. Jalankan `npm run gen`.\n";
ts += `export type Semantic = "success" | "warning" | "critical" | "info" | "neutral";\nexport type Variant = "solid" | "soft" | "outline";\n`;
ts += `export interface StatusDef { label_id: string; label_en: string; semantic: Semantic; variant: Variant; icon?: string }\n`;
const groups = Object.keys(statusMap).filter((k) => k !== "version");
ts += `export type ObjectType = ${groups.map((g) => JSON.stringify(g)).join(" | ")};\n`;
ts += "export const statusMap: Record<ObjectType, Record<string, StatusDef>> = " + JSON.stringify(Object.fromEntries(groups.map((g) => [g, statusMap[g]])), null, 2) + " as const;\n";
ts += `export function statusDef(objectType: ObjectType, status: string): StatusDef | undefined { return statusMap[objectType]?.[status]; }\n`;
fs.mkdirSync(path.join(here, "../src/lib"), { recursive: true });
fs.writeFileSync(path.join(here, "../src/lib/status-map.ts"), ts);

// ---------- Dart (untuk repo mobile) ----------
const buildDir = path.join(root, "contracts/build");
fs.mkdirSync(buildDir, { recursive: true });
let dart = "// GENERATED — jangan edit manual. Sumber: design-tokens/tokens.json.\nimport 'package:flutter/material.dart';\n\nclass BvTokens {\n";
const hex = (h) => "Color(0xFF" + h.replace("#", "").toUpperCase() + ")";
for (const group of ["brand", "success", "warning", "critical", "info", "neutral"]) {
  for (const [k, v] of Object.entries(tokens[group])) dart += `  static const Color ${group}${k} = ${hex(v.$value)};\n`;
}
for (const [k, v] of Object.entries(tokens.surface)) dart += `  static const Color surface${k.replace(/-([a-z])/g, (_, c) => c.toUpperCase()).replace(/^./, (c) => c.toUpperCase())} = ${hex(v.$value)};\n`;
for (const [k, v] of Object.entries(tokens.radius)) dart += `  static const double radius${k[0].toUpperCase() + k.slice(1)} = ${parseFloat(v)};\n`;
for (const [k, v] of Object.entries(tokens.space)) dart += `  static const double space${k} = ${parseFloat(v)};\n`;
dart += "}\n";
fs.writeFileSync(path.join(buildDir, "tokens.dart"), dart);
let sm = "// GENERATED — jangan edit manual. Sumber: contracts/status-map.yaml.\n\nclass StatusDef {\n  final String labelId; final String labelEn; final String semantic; final String variant; final String? icon;\n  const StatusDef(this.labelId, this.labelEn, this.semantic, this.variant, [this.icon]);\n}\n\nconst Map<String, Map<String, StatusDef>> statusMap = {\n";
for (const g of groups) {
  sm += `  '${g}': {\n`;
  for (const [k, v] of Object.entries(statusMap[g])) sm += `    '${k}': StatusDef(${JSON.stringify(v.label_id).replace(/"/g, "'")}, ${JSON.stringify(v.label_en).replace(/"/g, "'")}, '${v.semantic}', '${v.variant}'${v.icon ? `, '${v.icon}'` : ""}),\n`;
  sm += "  },\n";
}
sm += "};\n";
fs.writeFileSync(path.join(buildDir, "status_map.dart"), sm);

// ---------- i18n status labels (id/en) ----------
const i18n = { id: {}, en: {} };
for (const g of groups) for (const [k, v] of Object.entries(statusMap[g])) {
  i18n.id[`status.${g}.${k}`] = v.label_id;
  i18n.en[`status.${g}.${k}`] = v.label_en;
}
fs.mkdirSync(path.join(here, "../src/lib/i18n"), { recursive: true });
fs.writeFileSync(path.join(here, "../src/lib/i18n/status.generated.json"), JSON.stringify(i18n, null, 2));
console.log("generated: tokens.css, status-map.ts, status.generated.json, contracts/build/{tokens,status_map}.dart");
