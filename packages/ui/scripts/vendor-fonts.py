"""
Vendors the self-hosted UI fonts.

Run this when an icon is added, or when a font needs refreshing:

    npm run fonts:vendor (di packages/ui)

Requires `pip install fonttools brotli`. This is a build-time tool, never a
runtime dependency: the shipped application must reach no CDN at all, because
an on-premise plant has no outbound internet and a missing icon font leaves
raw ligature names ("menu_open", "edit", "delete") in place of every icon.

Why not simply ask Google Fonts for a subset:

  - `icon_names=` returns a font addressed by codepoint with the ligature
    table stripped, and the design system's Icon component renders the icon
    *name* as text. Ligatures are what turn that name into a glyph, so the
    icons never form.
  - Subsetting by `--text` alone does not shrink anything either. Layout
    closure follows every ligature reachable from those letters, which is all
    4,275 icons.

So the ligature table itself is pruned to the icons this product renders
before subsetting. That is the difference between 5.1 MB and ~125 KB.
"""

from __future__ import annotations

import json
import os
import re
import sys
import urllib.request
from pathlib import Path

try:
    from fontTools import subset
    from fontTools.ttLib import TTFont
except ImportError:
    sys.exit("fonttools is required: pip install fonttools brotli")

HERE = Path(__file__).resolve().parent
REPO = HERE.parents[2]
FONT_DIR = HERE.parent / "src" / "bv" / "fonts"
CSS_PATH = HERE.parent / "src" / "bv" / "fonts.css"

# Google Fonts serves TTF unless the request looks like a browser.
UA = (
    "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 "
    "(KHTML, like Gecko) Chrome/120.0 Safari/537.36"
)

ICON_CSS = (
    "https://fonts.googleapis.com/css2?family=Material+Symbols+Rounded"
    ":opsz,wght,FILL,GRAD@20..48,100..700,0..1,-50..200"
)
TEXT_CSS = (
    "https://fonts.googleapis.com/css2"
    "?family=Roboto+Flex:wght@100..900&family=Inter:wght@100..900&display=swap"
)

# Indonesian plus the English domain terms. The other ranges Google ships
# (Cyrillic, Greek, Vietnamese) would triple the payload for nothing.
TEXT_SUBSETS = {"latin", "latin-ext"}

# Direct string forms.
ICON_PATTERNS = [
    re.compile(r"""<Icon\s+name=['"]([a-z0-9_]+)['"]"""),
    re.compile(r"""\bicon:\s*['"]([a-z0-9_]+)['"]"""),
    re.compile(r"""\bicon=['"]([a-z0-9_]+)['"]"""),
    re.compile(r"""\bname:\s*['"]([a-z0-9_]+)['"]"""),
]

# Expression forms such as `name={up ? 'trending_up' : 'trending_flat'}`, where
# one icon element can name several icons depending on state. Every quoted
# identifier inside the braces is a candidate; names that turn out not to be
# real icons are reported and skipped, so being generous here costs nothing.
ICON_EXPRESSIONS = [
    re.compile(r"(?:name|icon)=\{([^{}]*(?:\{[^{}]*\}[^{}]*)*)\}"),
    re.compile(r"(?:name|icon):\s*([^,\n]*\?[^,\n]*)"),
    # Lookup tables such as `const ICONS: Record<KpiMetric, string> = {...}`,
    # where the icon name is the value and the key says nothing about icons.
    re.compile(r"\bconst\s+\w*ICONS?\w*[^=]*=\s*\{([^}]*)\}", re.IGNORECASE),
]
QUOTED = re.compile(r"""['"]([a-z][a-z0-9_]{2,})['"]""")

# Icons the design system's own components render internally, which the
# patterns above cannot always see.
BASELINE_ICONS = [
    "add", "arrow_back", "arrow_downward", "arrow_drop_down", "arrow_forward", "arrow_upward",
    "calendar_today", "cancel", "check", "check_circle", "chevron_left", "chevron_right",
    "close", "dark_mode", "delete", "density_medium", "density_small", "done", "download",
    "drag_indicator", "edit", "error", "expand_less", "expand_more", "filter_list", "first_page",
    "info", "keyboard_arrow_down", "keyboard_arrow_left", "keyboard_arrow_right",
    "keyboard_arrow_up", "last_page", "light_mode", "logout", "menu", "menu_open", "more_horiz",
    "more_vert", "person", "refresh", "remove", "schedule", "search", "settings", "sort",
    "star", "unfold_more", "upload", "view_list", "visibility", "visibility_off", "warning",
]


def fetch(url: str) -> bytes:
    request = urllib.request.Request(url, headers={"User-Agent": UA})
    with urllib.request.urlopen(request, timeout=180) as response:
        return response.read()


def collect_icon_names() -> list[str]:
    """Every icon rendered anywhere in the product, including the design system."""
    found = set(BASELINE_ICONS)
    roots = [REPO / "web" / "src", REPO / "packages" / "ui" / "src", REPO / "website" / "src"]  # website publik ikut dipindai
    for root in roots:
        for path in root.rglob("*"):
            if path.suffix not in {".ts", ".tsx"} or "node_modules" in path.parts or "dist" in path.parts:
                continue
            source = path.read_text(encoding="utf-8", errors="ignore")
            for pattern in ICON_PATTERNS:
                found.update(pattern.findall(source))
            for pattern in ICON_EXPRESSIONS:
                for expression in pattern.findall(source):
                    found.update(QUOTED.findall(expression))
    return sorted(n for n in found if re.fullmatch(r"[a-z][a-z0-9_]{2,}", n))


def ligature_name(font: TTFont, first: str, lig) -> str:
    """The text that types this ligature, e.g. `location_on`."""
    rev = _reverse_cmap(font)
    return rev.get(first, "?") + "".join(rev.get(component, "?") for component in lig.Component)


def ligature_names(font: TTFont) -> dict[str, str]:
    """Every ligature the font knows, as typed name -> glyph it produces."""
    names: dict[str, str] = {}
    for lookup in font["GSUB"].table.LookupList.Lookup:
        for sub_table in lookup.SubTable:
            inner = getattr(sub_table, "ExtSubTable", sub_table)
            if not hasattr(inner, "ligatures"):
                continue
            for first, ligatures in inner.ligatures.items():
                for lig in ligatures:
                    names[ligature_name(font, first, lig)] = lig.LigGlyph
    return names


_REVERSE_CMAP: dict[int, dict[str, str]] = {}


def _reverse_cmap(font: TTFont) -> dict[str, str]:
    key = id(font)
    if key not in _REVERSE_CMAP:
        _REVERSE_CMAP[key] = {glyph: chr(code) for code, glyph in font.getBestCmap().items()}
    return _REVERSE_CMAP[key]


def build_icon_font(icon_names: list[str]) -> None:
    css = fetch(ICON_CSS).decode("utf-8")
    url = re.search(r"url\((https://fonts\.gstatic\.com/[^)]+)\)", css)
    if not url:
        raise SystemExit("No icon font URL in the Google Fonts response")

    full_path = FONT_DIR / ".material-symbols-full.woff2"
    full_path.write_bytes(fetch(url.group(1)))
    print(f"  downloaded full font        {full_path.stat().st_size / 1024 / 1024:.2f} MB")

    font = TTFont(full_path)

    # An icon is addressed by the *ligature* it types, and the ligature table
    # is the only place that name is recorded. Reading glyph names instead
    # loses every alias: `location_on` types the glyph named `place`,
    # `warning_amber` and `report_problem` both type `warning`, and each of
    # them was being reported as "not a real icon" and dropped — which is why
    # those icons rendered as their own names in the console.
    ligature_glyph = ligature_names(font)
    keep = sorted(n for n in set(icon_names) if n in ligature_glyph)
    unknown = sorted(set(icon_names) - set(keep))
    if unknown:
        print(f"  not real icon names, ignored: {', '.join(unknown)}")
    keep_glyphs = sorted({ligature_glyph[n] for n in keep})

    # Prune the ligature table before subsetting, or layout closure re-adds
    # every icon reachable from the same letters. Prune by ligature name, not
    # glyph: `warning` the glyph must survive for `warning_amber` the alias.
    keep_set = set(keep)
    kept = dropped = 0
    for lookup in font["GSUB"].table.LookupList.Lookup:
        for sub_table in lookup.SubTable:
            inner = getattr(sub_table, "ExtSubTable", sub_table)
            if not hasattr(inner, "ligatures"):
                continue
            for first, ligatures in list(inner.ligatures.items()):
                survivors = [lig for lig in ligatures if ligature_name(font, first, lig) in keep_set]
                dropped += len(ligatures) - len(survivors)
                kept += len(survivors)
                if survivors:
                    inner.ligatures[first] = survivors
                else:
                    del inner.ligatures[first]
    print(f"  ligatures kept {kept}, dropped {dropped}")

    options = subset.Options()
    options.flavor = "woff2"
    options.layout_features = ["rlig", "rclt", "liga"]
    options.notdef_outline = True
    options.name_IDs = ["*"]
    options.drop_tables = []

    subsetter = subset.Subsetter(options=options)
    subsetter.populate(text="".join(sorted(set("".join(keep)))) + " ", glyphs=keep_glyphs)
    subsetter.subset(font)

    out = FONT_DIR / "material-symbols-rounded.woff2"
    subset.save_font(font, out, options)
    full_path.unlink()
    print(f"  material-symbols-rounded.woff2  {out.stat().st_size / 1024:.1f} KB ({len(keep)} icons)")


def build_text_fonts() -> list[dict]:
    css = fetch(TEXT_CSS).decode("utf-8")
    faces = []
    pattern = re.compile(r"/\*\s*([a-z-]+)\s*\*/\s*@font-face\s*\{(.*?)\}", re.S)
    for subset_name, body in pattern.findall(css):
        if subset_name not in TEXT_SUBSETS:
            continue
        family = re.search(r"font-family:\s*'([^']+)'", body).group(1)
        url = re.search(r"url\((https://[^)]+)\)", body).group(1)
        unicode_range = re.search(r"unicode-range:\s*([^;]+);", body).group(1).strip()
        filename = f"{family.lower().replace(' ', '-')}-{subset_name}.woff2"
        path = FONT_DIR / filename
        path.write_bytes(fetch(url))
        print(f"  {filename:<32}{path.stat().st_size / 1024:.1f} KB")
        faces.append({"family": family, "file": filename, "range": unicode_range})
    return faces


def write_css(faces: list[dict]) -> None:
    blocks = "\n\n".join(
        f"""@font-face {{
  font-family: '{face["family"]}';
  font-style: normal;
  font-weight: 100 900;
  font-display: swap;
  src: url('./fonts/{face["file"]}') format('woff2');
  unicode-range: {face["range"]};
}}"""
        for face in faces
    )

    CSS_PATH.write_text(
        f"""/**
 * Factory Vision, self-hosted UI fonts. GENERATED, do not edit by hand.
 *
 * Regenerate with: npm run fonts:vendor (di packages/ui)
 *
 * Every font the product needs is bundled with the deployment, so an
 * on-premise install renders correctly with no outbound internet. Material
 * Symbols is a ligature font: without it the console shows the raw icon names
 * instead of icons, which is not a graceful degradation.
 *
 * The icon font carries only the ligatures this product renders. The text
 * faces carry Latin and Latin Extended, which is what Indonesian and the
 * English domain terms need.
 */

{blocks}

@font-face {{
  font-family: 'Material Symbols Rounded';
  font-style: normal;
  font-weight: 100 700;
  /* Block, not swap: a moment of nothing beats a moment of "menu_open". */
  font-display: block;
  src: url('./fonts/material-symbols-rounded.woff2') format('woff2');
}}

/*
 * Matches what Google Fonts ships beside the font. The design system's Icon
 * component renders this class, and without the 'liga' feature the ligatures
 * never form into glyphs.
 */
.material-symbols-rounded {{
  font-family: 'Material Symbols Rounded';
  font-weight: normal;
  font-style: normal;
  font-size: 24px;
  line-height: 1;
  letter-spacing: normal;
  text-transform: none;
  display: inline-block;
  white-space: nowrap;
  word-wrap: normal;
  direction: ltr;
  -webkit-font-feature-settings: 'liga';
  -webkit-font-smoothing: antialiased;
  font-variation-settings:
    'FILL' 0,
    'wght' 400,
    'GRAD' 0,
    'opsz' 24;
}}
""",
        encoding="utf-8",
        newline="\n",
    )
    print(f"\nWrote {CSS_PATH.relative_to(REPO)}")


def main() -> None:
    FONT_DIR.mkdir(parents=True, exist_ok=True)
    icons = collect_icon_names()
    print(f"Icons referenced in the product: {len(icons)}")
    build_icon_font(icons)
    faces = build_text_fonts()
    write_css(faces)


if __name__ == "__main__":
    main()
