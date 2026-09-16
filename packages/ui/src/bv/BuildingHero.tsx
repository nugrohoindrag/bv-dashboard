import React from 'react';
import { motion } from 'motion/react';
import { Icon } from '../components/communication/Icon.js';
import { M3_TRANSITIONS, useReducedMotionSafe } from '../motion/index.js';

/**
 * The dashboard's flagship banner: the property's name, the operational
 * headline for today, and the primary actions. Fills solid `--color-primary`
 * (never the pale container, never a gradient) so it reads as the boldest
 * surface on the page, following the "one fill for identity" rule. The skyline
 * behind the copy strokes itself in `--color-on-primary` for the same reason:
 * on a solid primary card anything stroked in primary disappears.
 */
export interface BuildingHeroStat {
  label: string;
  value: string;
  tone?: 'default' | 'success' | 'warning' | 'error';
}

export interface BuildingHeroAction {
  label: string;
  icon?: string;
  onClick?: () => void;
}

export interface BuildingHeroProps {
  /** Property / organization name, the eyebrow line. */
  propertyName: string;
  /** Context under the name, e.g. "Rabu, 16 September 2026 · Shift Pagi". */
  context?: string;
  /** Live indicator label (e.g. "Live") shown next to the name; omit to hide. */
  liveLabel?: string;
  headline: string;
  headlineSuffix?: string;
  /** Up to four compact stats rendered beside the headline. */
  stats?: BuildingHeroStat[];
  /** First action renders filled on-primary; the rest as outlined. */
  actions?: BuildingHeroAction[];
  /** Top-right pill, e.g. "SLA 96%". */
  badge?: { icon?: string; label: string };
  className?: string;
}

const Skyline: React.FC<{ reduced: boolean }> = ({ reduced }) => (
  <svg
    aria-hidden
    viewBox="0 0 520 160"
    preserveAspectRatio="xMaxYMax meet"
    style={{ position: 'absolute', right: 0, bottom: 0, height: '100%', width: 'auto', maxWidth: '55%', color: 'var(--color-on-primary)', opacity: 0.28, pointerEvents: 'none' }}
  >
    <g fill="none" stroke="currentColor" strokeWidth="2" strokeLinejoin="round" strokeLinecap="round">
      <path d="M10 150 H510" />
      <path d="M40 150 V92 H88 V150" />
      <path d="M52 92 V70 H76 V92" />
      <path d="M110 150 V60 H160 V150" />
      <path d="M124 60 V44 H146 V60" />
      <path d="M182 150 V104 H222 V150" />
      <path d="M244 150 V30 H300 V150" />
      <path d="M262 30 V14 H282 V30" />
      <path d="M322 150 V84 H366 V150" />
      <path d="M388 150 V52 H430 V150" />
      <path d="M452 150 V112 H496 V150" />
      {/* windows */}
      {[0, 1, 2, 3, 4, 5].map((r) => (
        <g key={r}>
          <path d={`M254 ${44 + r * 16} H266 M278 ${44 + r * 16} H290`} strokeWidth="3" />
          <path d={`M120 ${74 + r * 12} H130 M140 ${74 + r * 12} H150`} strokeWidth="3" />
          <path d={`M398 ${64 + r * 13} H408 M418 ${64 + r * 13} H426`} strokeWidth="3" />
        </g>
      ))}
    </g>
    {!reduced && (
      <motion.circle
        cx="470"
        cy="36"
        r="10"
        fill="none"
        stroke="currentColor"
        strokeWidth="2"
        initial={{ opacity: 0.4, scale: 0.9 }}
        animate={{ opacity: [0.4, 1, 0.4], scale: [0.9, 1.05, 0.9] }}
        transition={{ duration: 4, repeat: Infinity, ease: 'easeInOut' }}
        style={{ transformOrigin: '470px 36px' }}
      />
    )}
  </svg>
);

const statTone: Record<NonNullable<BuildingHeroStat['tone']>, string> = {
  default: 'var(--color-on-primary)',
  success: 'var(--color-success-container)',
  warning: 'var(--color-warning-container)',
  error: 'var(--color-error-container)',
};

export const BuildingHero: React.FC<BuildingHeroProps> = ({ propertyName, context, liveLabel, headline, headlineSuffix, stats = [], actions = [], badge, className = '' }) => {
  const reduced = useReducedMotionSafe();
  const [primary, ...secondary] = actions;
  return (
    <motion.section
      initial={{ opacity: 0, y: reduced ? 0 : 12 }}
      animate={{ opacity: 1, y: 0 }}
      transition={M3_TRANSITIONS.enter}
      className={`bv-hero ${className}`}
      style={{
        position: 'relative',
        overflow: 'hidden',
        borderRadius: 'var(--radius-hero)',
        padding: '20px 24px',
        backgroundColor: 'var(--color-primary)',
        color: 'var(--color-on-primary)',
        boxShadow: 'var(--elevation-1)',
      }}
    >
      <Skyline reduced={reduced} />
      <div style={{ position: 'relative', zIndex: 1, display: 'flex', flexDirection: 'column', gap: 'var(--space-4)' }}>
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', gap: 'var(--space-4)' }}>
          <div>
            <div style={{ display: 'flex', alignItems: 'center', gap: 8, fontWeight: 700, fontSize: 13, letterSpacing: '0.04em', textTransform: 'uppercase' }}>
              {liveLabel && (
                <span style={{ display: 'inline-flex', alignItems: 'center', gap: 6, fontSize: 11, padding: '2px 8px', borderRadius: 'var(--radius-pill)', backgroundColor: 'var(--color-success-container)', color: 'var(--color-on-success-container)', textTransform: 'none', letterSpacing: 0 }}>
                  <span style={{ width: 6, height: 6, borderRadius: 'var(--radius-pill)', backgroundColor: 'var(--color-success)' }} />
                  {liveLabel}
                </span>
              )}
              {propertyName}
            </div>
            {context && <div style={{ marginTop: 4, fontSize: 12.5, opacity: 0.85 }}>{context}</div>}
          </div>
          {badge && (
            <span style={{ display: 'inline-flex', alignItems: 'center', gap: 6, padding: '6px 12px', borderRadius: 'var(--radius-pill)', backgroundColor: 'var(--color-on-primary)', color: 'var(--color-primary)', fontWeight: 700, fontSize: 13, whiteSpace: 'nowrap' }}>
              {badge.icon && <Icon name={badge.icon} size={16} />}
              {badge.label}
            </span>
          )}
        </div>

        <div style={{ display: 'flex', alignItems: 'flex-end', gap: 'var(--space-6)', flexWrap: 'wrap' }}>
          <div>
            <div style={{ fontSize: 30, fontWeight: 800, lineHeight: 1.1, letterSpacing: '-0.01em' }}>
              {headline}
              {headlineSuffix && <span style={{ fontSize: 16, fontWeight: 600, opacity: 0.85, marginLeft: 10 }}>{headlineSuffix}</span>}
            </div>
          </div>
          {stats.length > 0 && (
            <div style={{ display: 'flex', gap: 'var(--space-5)', flexWrap: 'wrap' }}>
              {stats.map((s) => (
                <div key={s.label} style={{ minWidth: 72 }}>
                  <div style={{ fontSize: 11, opacity: 0.8, textTransform: 'uppercase', letterSpacing: '0.04em' }}>{s.label}</div>
                  <div style={{ fontSize: 20, fontWeight: 800, color: statTone[s.tone ?? 'default'], fontVariantNumeric: 'tabular-nums' }}>{s.value}</div>
                </div>
              ))}
            </div>
          )}
        </div>

        {actions.length > 0 && (
          <div style={{ display: 'flex', gap: 'var(--space-3)', flexWrap: 'wrap' }}>
            {primary && (
              <button type="button" onClick={primary.onClick} className="bv-hero__action bv-hero__action--primary">
                {primary.icon && <Icon name={primary.icon} size={18} />}
                {primary.label}
              </button>
            )}
            {secondary.map((a) => (
              <button key={a.label} type="button" onClick={a.onClick} className="bv-hero__action">
                {a.icon && <Icon name={a.icon} size={18} />}
                {a.label}
              </button>
            ))}
          </div>
        )}
      </div>
      <style>{`
        .bv-hero__action{display:inline-flex;align-items:center;gap:8px;height:40px;padding:0 16px;border-radius:var(--radius-pill);font:inherit;font-weight:700;font-size:13.5px;cursor:pointer;border:1px solid var(--color-on-primary);background:transparent;color:var(--color-on-primary);transition:background-color var(--motion-duration-short4, 200ms) ease}
        .bv-hero__action:hover{background-color:var(--color-primary-container);color:var(--color-on-primary-container);border-color:var(--color-primary-container)}
        .bv-hero__action--primary{background-color:var(--color-on-primary);color:var(--color-primary)}
        .bv-hero__action--primary:hover{background-color:var(--color-primary-container);color:var(--color-on-primary-container)}
        .bv-hero__action:focus-visible{outline:2px solid var(--color-on-primary);outline-offset:2px}
      `}</style>
    </motion.section>
  );
};
