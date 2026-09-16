import React from 'react';

/**
 * The BuildingVision brand: the double-roof "M" mark (the same emblem the
 * Staff App and the Tenant PWA render) and the BuildingVision wordmark.
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
const BRAND_TEAL = '#0E9187';
const BRAND_TEAL_LIGHT = '#5FD3C6';
const BRAND_INK = '#111827';
const WHITE = '#FFFFFF';

export type BuildingVisionLogoTone = 'auto' | 'brand' | 'white';

interface LogoColours {
  mark: string;
  mark2: string;
  word: string;
  tagline: string;
}

function logoColours(tone: BuildingVisionLogoTone): LogoColours {
  switch (tone) {
    case 'white':
      return { mark: WHITE, mark2: WHITE, word: WHITE, tagline: WHITE };
    case 'brand':
      return { mark: BRAND_TEAL, mark2: BRAND_TEAL_LIGHT, word: BRAND_INK, tagline: '#4B5563' };
    default:
      return {
        mark: 'var(--bv-logo-mark)',
        mark2: 'var(--bv-logo-mark-2)',
        word: 'var(--bv-logo-word)',
        tagline: 'var(--bv-logo-tagline)',
      };
  }
}

export interface BuildingVisionIconProps {
  size?: number;
  tone?: BuildingVisionLogoTone;
  className?: string;
  title?: string;
}

/** The mark alone: two roofs on a rounded tile. */
export const BuildingVisionIcon: React.FC<BuildingVisionIconProps> = ({ size = 32, tone = 'auto', className, title = 'BuildingVision' }) => {
  const c = logoColours(tone);
  const id = React.useId();
  return (
    <svg width={size} height={size} viewBox="0 0 64 64" className={className} role="img" aria-label={title}>
      <defs>
        <linearGradient id={id} x1="0" y1="0" x2="1" y2="1">
          <stop offset="0" stopColor={c.mark2} />
          <stop offset="1" stopColor={c.mark} />
        </linearGradient>
      </defs>
      <path
        d="M9 43 L22 25 L35 43 M29 43 L42 25 L55 43 M55 43 V31"
        fill="none"
        stroke={tone === 'white' ? WHITE : `url(#${id})`}
        strokeWidth="6.5"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
      <rect x="20" y="37" width="4" height="4" rx="1" fill={tone === 'white' ? WHITE : c.mark} />
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
  return (
    <span className={className} style={{ display: 'inline-flex', alignItems: 'center', gap: Math.round(height * 0.28), lineHeight: 1 }}>
      <span
        style={{
          display: 'inline-flex',
          alignItems: 'center',
          justifyContent: 'center',
          width: height,
          height,
          borderRadius: 'var(--radius-md)',
          backgroundColor: tone === 'white' ? 'rgba(255, 255, 255, 0.16)' : 'var(--color-primary-soft)',
          flexShrink: 0,
        }}
      >
        <BuildingVisionIcon size={Math.round(height * 0.8)} tone={tone} />
      </span>
      <span style={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
        <span style={{ fontFamily: 'var(--font-family)', fontWeight: 800, fontSize: Math.round(height * 0.5), letterSpacing: '-0.01em', color: c.word }}>
          Building<span style={{ color: tone === 'white' ? WHITE : c.mark }}>Vision</span>
        </span>
        {tagline && (
          <span style={{ fontFamily: 'var(--font-family)', fontSize: Math.max(10, Math.round(height * 0.3)), color: c.tagline }}>
            {tagline}
          </span>
        )}
      </span>
    </span>
  );
};
