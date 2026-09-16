#!/usr/bin/env node
/**
 * Keeps packages/ui/src an exact mirror of the upstream design system.
 *
 *   node scripts/sync-design-system.mjs check   — report drift, exit 1 if any
 *   node scripts/sync-design-system.mjs pull    — overwrite the mirror from upstream
 *
 * src/bv is the BuildingVision extension layer and is never touched by either
 * mode. Everything else in src/ belongs to upstream: edit it there, then pull.
 */
import { readdirSync, readFileSync, statSync, mkdirSync, copyFileSync, rmSync } from 'node:fs';
import { join, relative, dirname, sep } from 'node:path';
import { fileURLToPath } from 'node:url';

const HERE = dirname(fileURLToPath(import.meta.url));
const MIRROR = join(HERE, '..', 'src');
const UPSTREAM = process.env.DESIGN_SYSTEM_PATH
  ? join(process.env.DESIGN_SYSTEM_PATH, 'src')
  : join('D:', sep, 'Design System', 'src');

/** Paths under src/ that belong to Factory Vision, not upstream. */
const OWNED_BY_PRODUCT = ['bv'];

const mode = process.argv[2] ?? 'check';

function walk(root, base = root, out = []) {
  for (const entry of readdirSync(root)) {
    const abs = join(root, entry);
    const rel = relative(base, abs);
    if (OWNED_BY_PRODUCT.includes(rel.split(sep)[0])) continue;
    if (statSync(abs).isDirectory()) walk(abs, base, out);
    else out.push(rel);
  }
  return out;
}

let upstreamFiles;
try {
  upstreamFiles = walk(UPSTREAM);
} catch {
  console.error(`Upstream design system not found at ${UPSTREAM}.`);
  console.error('Set DESIGN_SYSTEM_PATH to its root if it lives elsewhere.');
  process.exit(2);
}
const mirrorFiles = walk(MIRROR);

const same = (a, b) => {
  try {
    return readFileSync(a).equals(readFileSync(b));
  } catch {
    return false;
  }
};

const modified = upstreamFiles.filter(
  (f) => mirrorFiles.includes(f) && !same(join(UPSTREAM, f), join(MIRROR, f))
);
const missing = upstreamFiles.filter((f) => !mirrorFiles.includes(f));
const extra = mirrorFiles.filter((f) => !upstreamFiles.includes(f));

if (mode === 'pull') {
  for (const f of [...modified, ...missing]) {
    mkdirSync(dirname(join(MIRROR, f)), { recursive: true });
    copyFileSync(join(UPSTREAM, f), join(MIRROR, f));
  }
  for (const f of extra) rmSync(join(MIRROR, f));
  console.log(
    `Mirror synced from ${UPSTREAM}: ${modified.length} updated, ${missing.length} added, ${extra.length} removed.`
  );
  process.exit(0);
}

const drift = modified.length + missing.length + extra.length;
if (drift === 0) {
  console.log(`Mirror is clean against ${UPSTREAM} (${upstreamFiles.length} files).`);
  process.exit(0);
}
for (const f of modified) console.error(`modified  src/${f}`);
for (const f of missing) console.error(`missing   src/${f}`);
for (const f of extra) console.error(`extra     src/${f}`);
console.error(
  `\n${drift} file(s) drifted. Move product-specific changes into src/fv, then run ` +
    `\`pnpm --filter @factory-vision/ui ds:pull\`.`
);
process.exit(1);
