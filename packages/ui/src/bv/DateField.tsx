import React, { useState } from 'react';

/**
 * BuildingVision, a date input whose label does not collide with the browser's
 * own placeholder.
 *
 * `FilledTextField` floats its label only when the field is focused or holds a
 * value, which is right for text. A `type="date"` input is different: the
 * browser always paints `dd/mm/yyyy` inside it, so an empty one showed the
 * resting label written over that placeholder — two overlapping strings in
 * every date filter on the planning screens.
 *
 * The label here is therefore always floated, and the box follows `Select`:
 * 56px, `surface-container`, a hairline all the way round at `radius-md` that
 * turns `primary` on focus. A filter row is where these two meet — a status
 * `Select` beside a date range — and the M3 filled recipe (a grey slab open on
 * three sides) read as a different, half-disabled control next to it. One
 * field shape, so a row of filters reads as one row. `bv/mirror-fixes.css`
 * gives `FilledTextField` the same box for the same reason.
 *
 * The native picker is left to inherit `color-scheme` from the theme rather
 * than declaring `light dark`: that keyword resolves against the *operating
 * system's* preference, so a console in dark mode on a light desktop opened a
 * white calendar and painted `dd/mm/yyyy` in near-black on a dark field.
 *
 * It lives in `bv/` rather than being fixed in place because
 * `packages/ui/src/components` is a byte-for-byte mirror of the upstream design
 * system and must not be edited.
 */
export interface DateFieldProps
  extends Omit<React.InputHTMLAttributes<HTMLInputElement>, 'type'> {
  label: string;
  error?: string;
  supportingText?: string;
  /** `date` by default; `month` and `datetime-local` share the problem. */
  type?: 'date' | 'month' | 'time' | 'datetime-local';
}

export const DateField: React.FC<DateFieldProps> = ({
  label,
  error,
  supportingText,
  type = 'date',
  disabled,
  className = '',
  style,
  onFocus,
  onBlur,
  ...props
}) => {
  const [isFocused, setIsFocused] = useState(false);
  const isError = Boolean(error);
  const accent = isError
    ? 'var(--color-error)'
    : isFocused
      ? 'var(--color-primary)'
      : 'var(--color-on-surface-variant)';
  // The box states are `Select`'s: a resting hairline, the accent on focus.
  const border = isError
    ? 'var(--color-error)'
    : isFocused
      ? 'var(--color-primary)'
      : 'var(--color-border)';

  return (
    <div
      className={`bv-date-field ${className}`}
      style={{ display: 'flex', flexDirection: 'column', gap: '4px', width: '100%', ...style }}
    >
      <div
        style={{
          position: 'relative',
          height: '56px',
          borderRadius: 'var(--radius-md)',
          backgroundColor: 'var(--color-surface-container)',
          border: `1px solid ${border}`,
          display: 'flex',
          alignItems: 'flex-end',
          opacity: disabled ? 0.38 : 1,
        }}
      >
        <label
          style={{
            position: 'absolute',
            left: '16px',
            top: '8px',
            fontSize: '12px',
            lineHeight: 1,
            color: accent,
            pointerEvents: 'none',
            fontFamily: 'var(--font-family)',
          }}
        >
          {label}
        </label>
        <input
          {...props}
          type={type}
          disabled={disabled}
          onFocus={(e) => {
            setIsFocused(true);
            onFocus?.(e);
          }}
          onBlur={(e) => {
            setIsFocused(false);
            onBlur?.(e);
          }}
          style={{
            width: '100%',
            border: 'none',
            outline: 'none',
            background: 'transparent',
            padding: '0 16px 8px',
            fontSize: '14px',
            fontFamily: 'var(--font-family)',
            color: 'var(--color-on-surface)',
          }}
        />
      </div>
      {(error || supportingText) && (
        <span
          style={{
            fontSize: '12px',
            paddingLeft: '16px',
            color: isError ? 'var(--color-error)' : 'var(--color-on-surface-variant)',
            fontFamily: 'var(--font-family)',
          }}
        >
          {error || supportingText}
        </span>
      )}
    </div>
  );
};
