"""
Fails when the product names an icon the vendored icon font cannot draw.

Material Symbols is a ligature font: `<Icon name="calendar_view_week" />`
renders the glyph only if the shipped subset carries that ligature, and
otherwise paints the literal text "calendar_view_week" — large, in the icon's
colour, overflowing whatever held it. The subset is generated from the source
by `pnpm --filter @factory-vision/ui fonts:vendor`, so every icon added after
the last generation degrades exactly that way. Nothing else notices: the
build passes, the type check passes, and the console ships a sidebar full of
words. This check is what notices.

Usage: python scripts/check-icon-font.py        (part of `pnpm ds:check`)
"""

from __future__ import annotations

import re
import sys
from pathlib import Path

try:
    from fontTools.ttLib import TTFont
except ImportError:
    raise SystemExit("fontTools is required: pip install fonttools brotli")

HERE = Path(__file__).resolve().parent
REPO = HERE.parent.parent.parent
FONT = REPO / "packages" / "ui" / "src" / "bv" / "fonts" / "material-symbols-rounded.woff2"

# Only the forms where the string can be nothing but an icon name. The
# vendoring script scans more generously (ternaries, lookup tables) and
# tolerates the non-icons that sweep picks up; a gate cannot.
ICON_PATTERNS = [
    re.compile(r"""<Icon\s+name=['"]([a-z0-9_]+)['"]"""),
    re.compile(r"""\bicon:\s*['"]([a-z0-9_]+)['"]"""),
    re.compile(r"""\bicon=['"]([a-z0-9_]+)['"]"""),
]


def referenced() -> dict[str, list[str]]:
    """icon name -> the files that render it."""
    where: dict[str, list[str]] = {}
    for root in (REPO / "web" / "src", REPO / "packages" / "ui" / "src"):
        for path in root.rglob("*"):
            if path.suffix not in {".ts", ".tsx"} or "node_modules" in path.parts or "dist" in path.parts:
                continue
            source = path.read_text(encoding="utf-8", errors="ignore")
            for pattern in ICON_PATTERNS:
                for name in pattern.findall(source):
                    where.setdefault(name, []).append(str(path.relative_to(REPO)))
    return where


def shipped() -> set[str]:
    """Every ligature the vendored font can type."""
    font = TTFont(FONT)
    reverse = {glyph: chr(code) for code, glyph in font.getBestCmap().items()}
    names: set[str] = set()
    for lookup in font["GSUB"].table.LookupList.Lookup:
        for sub_table in lookup.SubTable:
            inner = getattr(sub_table, "ExtSubTable", sub_table)
            if not hasattr(inner, "ligatures"):
                continue
            for first, ligatures in inner.ligatures.items():
                for lig in ligatures:
                    names.add(reverse.get(first, "?") + "".join(reverse.get(c, "?") for c in lig.Component))
    return names


def main() -> int:
    used = referenced()
    available = shipped()
    missing = sorted(name for name in used if name not in available)
    print(f"icon font: {len(available)} ligatures shipped, {len(used)} icons referenced")
    if not missing:
        print("OK - every referenced icon has a glyph.")
        return 0
    print(f"\n{len(missing)} icon(s) would render as text:")
    for name in missing:
        files = sorted(set(used[name]))
        print(f"  {name:28} {files[0]}{'' if len(files) == 1 else f'  (+{len(files) - 1} more)'}")
    print(
        "\nRegenerate the subset with `pnpm --filter @factory-vision/ui fonts:vendor`. "
        "A name still listed after that does not exist in Material Symbols Rounded; "
        "pick one that does (https://fonts.google.com/icons)."
    )
    return 1


if __name__ == "__main__":
    sys.exit(main())
