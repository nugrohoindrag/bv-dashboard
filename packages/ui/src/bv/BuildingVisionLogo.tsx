import React from 'react';

/**
 * The BuildingVision brand (Sep 2026): the "vision" mark — four nested lens
 * curves meeting at pointed tips with a horizontal tail on each side — and the
 * uppercase wordmark BUILDING VISION ("VISION" in brand blue). Source asset:
 * design-tokens/logo/*.svg (same geometry; the Tenant PWA, BVRooms app and
 * Staff App render the identical mark).
 *
 * The mark is vector; the wordmark is set in the app face on purpose: the
 * dashboard already ships the two families it needs (Roboto Flex / Inter), so
 * a separate glyph set would only add weight.
 *
 * Colours are literal hex on purpose and this is one of the two files allowed
 * to name them (the other is `bv/palette.css`). A logo is a fixed brand asset,
 * not a themed surface: `brand` and `white` are pinned here. The theme-aware
 * case is `auto`, switched by the `--bv-logo-*` tokens palette.css sets per
 * `data-theme`.
 */
const BRAND_BLUE = '#0442B9';
const BRAND_INK = '#1B1B1B';
const WHITE = '#FFFFFF';

export type BuildingVisionLogoTone = 'auto' | 'brand' | 'white';

interface LogoColours {
  mark: string;
  word: string;
  accent: string;
  tagline: string;
}

function logoColours(tone: BuildingVisionLogoTone): LogoColours {
  switch (tone) {
    case 'white':
      return { mark: WHITE, word: WHITE, accent: WHITE, tagline: WHITE };
    case 'brand':
      return { mark: BRAND_BLUE, word: BRAND_INK, accent: BRAND_BLUE, tagline: '#4B5563' };
    default:
      return {
        mark: 'var(--bv-logo-mark)',
        word: 'var(--bv-logo-word)',
        accent: 'var(--bv-logo-mark)',
        tagline: 'var(--bv-logo-tagline)',
      };
  }
}

/**
 * Mark geometry in a 360×260 box (centre 182,130): tips at x=62 / x=302,
 * tails to x=6 / x=356, four lens curves with apexes 6 / 37 / 67 / 97 px
 * from the top (mirrored below). Stroke 12, round caps.
 */
export const BUILDING_VISION_MARK_VIEWBOX = '0 0 362 260';
export const BUILDING_VISION_MARK_PATH = [
  'M6 130H62',
  'M302 130H356',
  'M62 130C110 -35 254 -35 302 130',
  'M62 130C110 6 254 6 302 130',
  'M62 130C110 46 254 46 302 130',
  'M62 130C110 86 254 86 302 130',
  'M62 130C110 295 254 295 302 130',
  'M62 130C110 254 254 254 302 130',
  'M62 130C110 214 254 214 302 130',
  'M62 130C110 174 254 174 302 130',
].join(' ');

export interface BuildingVisionIconProps {
  /** Width in px; height follows the 362:260 ratio. */
  size?: number;
  tone?: BuildingVisionLogoTone;
  className?: string;
  title?: string;
}

/** The mark alone (no tile): nested lens curves. */
export const BuildingVisionIcon: React.FC<BuildingVisionIconProps> = ({ size = 32, tone = 'auto', className, title = 'BuildingVision' }) => {
  const c = logoColours(tone);
  return (
    <svg width={size} height={Math.round((size * 260) / 362)} viewBox={BUILDING_VISION_MARK_VIEWBOX} className={className} role="img" aria-label={title}>
      <path d={BUILDING_VISION_MARK_PATH} fill="none" stroke={c.mark} strokeWidth="12" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
};

export interface BuildingVisionLogoProps {
  /** Height of the lock-up in px; width follows. */
  height?: number;
  tone?: BuildingVisionLogoTone;
  tagline?: string;
  className?: string;
}

/** Mark + wordmark lock-up for the sidebar header and the login page. */
export const BuildingVisionLogo: React.FC<BuildingVisionLogoProps> = ({ height = 36, tone = 'auto', tagline, className }) => {
  const c = logoColours(tone);
  const markWidth = Math.round(height * 1.25);
  return (
    <span className={className} style={{ display: 'inline-flex', alignItems: 'center', gap: Math.round(height * 0.3), lineHeight: 1 }}>
      <BuildingVisionIcon size={markWidth} tone={tone} />
      <span style={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
        <span
          style={{
            fontFamily: 'var(--font-family)',
            fontWeight: 800,
            fontSize: Math.round(height * 0.46),
            letterSpacing: '0.01em',
            textTransform: 'uppercase',
            whiteSpace: 'nowrap',
            color: c.word,
          }}
        >
          Building <span style={{ color: c.accent }}>Vision</span>
        </span>
        {tagline && (
          <span style={{ fontFamily: 'var(--font-family)', fontSize: Math.max(10, Math.round(height * 0.28)), color: c.tagline }}>
            {tagline}
          </span>
        )}
      </span>
    </span>
  );
};
