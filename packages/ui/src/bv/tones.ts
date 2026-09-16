/**
 * BuildingVision, Semantic Tone Map
 *
 * The design system forbids literal colour values in product code (,
 * tokens/contract.css: "NEVER put a raw value in a component"). Product
 * screens still need to tell KPI tiles apart, so they pick a *semantic tone*
 * and the tone resolves to a theme token. A tone therefore follows the active
 * theme and accent automatically; a hex code never does.
 *
 * Bad <MetricCard accentColor="#3B82F6" />
 * Preferred <MetricCard tone="info" />
 */

export type Tone =
  | 'primary'
  | 'success'
  | 'warning'
  | 'error'
  | 'info'
  | 'neutral'
  | 'chart-1'
  | 'chart-2'
  | 'chart-3'
  | 'chart-4';

/** Foreground / stroke colour for a tone. */
export const toneColor: Record<Tone, string> = {
  primary: 'var(--color-primary)',
  success: 'var(--color-success)',
  warning: 'var(--color-warning)',
  error: 'var(--color-error)',
  info: 'var(--color-info)',
  neutral: 'var(--color-on-surface-variant)',
  'chart-1': 'var(--color-chart-primary)',
  'chart-2': 'var(--color-chart-secondary)',
  'chart-3': 'var(--color-chart-tertiary)',
  'chart-4': 'var(--color-chart-quaternary)',
};

/**
 * Solid fill for a tone. This is the default way to colour a chip, badge,
 * pill or status block: an opaque container token, paired with the
 * `toneOnContainer` text colour below. Solid fills keep their contrast on any
 * surface, which a translucent wash cannot promise.
 */
export const toneContainer: Record<Tone, string> = {
  primary: 'var(--color-primary-container)',
  success: 'var(--color-success-container)',
  warning: 'var(--color-warning-container)',
  error: 'var(--color-error-container)',
  info: 'var(--color-info-container)',
  neutral: 'var(--color-surface-container-high)',
  'chart-1': 'var(--color-surface-container-high)',
  'chart-2': 'var(--color-surface-container-high)',
  'chart-3': 'var(--color-surface-container-high)',
  'chart-4': 'var(--color-surface-container-high)',
};

/** Text/icon colour to place on top of `toneContainer`. */
export const toneOnContainer: Record<Tone, string> = {
  primary: 'var(--color-on-primary-container)',
  success: 'var(--color-on-success-container)',
  warning: 'var(--color-on-warning-container)',
  error: 'var(--color-on-error-container)',
  info: 'var(--color-on-info-container)',
  neutral: 'var(--color-on-surface)',
  'chart-1': 'var(--color-on-surface)',
  'chart-2': 'var(--color-on-surface)',
  'chart-3': 'var(--color-on-surface)',
  'chart-4': 'var(--color-on-surface)',
};

/**
 * Foreground to place directly on `toneColor`, the saturated tone itself
 * used as a solid fill (an icon box, a selected state), as opposed to
 * `toneOnContainer`, which pairs with the pale `toneContainer` wash. Reach
 * for this whenever a tone is the *background* not a container tint.
 */
export const toneOnColor: Record<Tone, string> = {
  primary: 'var(--color-on-primary)',
  success: 'var(--color-on-success)',
  warning: 'var(--color-on-warning)',
  error: 'var(--color-on-error)',
  info: 'var(--color-on-info)',
  neutral: 'var(--color-on-surface)',
  'chart-1': 'var(--color-on-primary)',
  'chart-2': 'var(--color-on-primary)',
  'chart-3': 'var(--color-on-primary)',
  'chart-4': 'var(--color-on-primary)',
};

/**
 * A translucent wash of a tone, mixed against the surface underneath.
 *
 * Reach for this only where a fill genuinely has to let the surface through,
 * a continuous intensity ramp, a selection overlay stacked on live content.
 * For anything with a fixed set of states (chips, badges, pills, status
 * blocks) use the solid `toneContainer` / `toneOnContainer` pair instead: a
 * wash lands on whatever happens to be behind it, so its contrast is a
 * coincidence rather than a guarantee.
 *
 * `color-mix` keeps the value derived from the token instead of a baked rgba,
 * so the wash re-derives itself when the theme or accent changes.
 */
export const toneWash = (tone: Tone, percent = 12): string =>
  `color-mix(in srgb, ${toneColor[tone]} ${percent}%, transparent)`;
