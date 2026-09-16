import React, { useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { AnimatePresence, motion } from 'motion/react';
import { Icon } from '../components/communication/Icon.js';
import { IconButton } from '../components/actions/IconButton.js';

/**
 * BuildingVision, row action menu (⋮).
 *
 * A table row rarely has one action, and a row of icon buttons spends the
 * table's scarcest resource — horizontal space — on affordances instead of on
 * the data the operator came to read. This collapses a row's actions into a
 * single overflow button, so the action column costs one icon and the width it
 * gives back goes to the columns that carry meaning.
 *
 * The panel is portalled to `document.body` rather than positioned inside the
 * row. `AdvancedDataTable` clips itself twice — `overflow: hidden` on the card
 * and `overflow-x: auto` on the scroller, whose overflow-y computes to `auto`
 * and so clips vertically too — and an absolutely positioned popover inside a
 * cell is cut off by both. Portalling escapes the clip; fixed coordinates
 * measured from the trigger keep the menu attached to the button it belongs
 * to, and are recomputed while any ancestor scrolls or the window resizes.
 *
 * The panel flips above the trigger when it would cross the bottom edge and is
 * clamped inside the viewport on both axes, so a row on the last visible line
 * of the table still opens a complete menu. It closes on click-outside, on
 * Escape, and after any item runs.
 */
export interface RowActionItem {
  id: string;
  label: string;
  /** Material Symbols Rounded ligature name. */
  icon?: string;
  onClick?: () => void;
  disabled?: boolean;
  /** Destructive actions take the error tone and sit last, below a divider. */
  danger?: boolean;
}

export interface RowActionMenuProps {
  items: RowActionItem[];
  /** Accessible name for the trigger; the visible label is the glyph. */
  label?: string;
  className?: string;
}

const GAP = 6;
const EDGE = 8;

export const RowActionMenu: React.FC<RowActionMenuProps> = ({
  items,
  label = 'Aksi lainnya',
  className = '',
}) => {
  const [open, setOpen] = useState(false);
  const [pos, setPos] = useState<{ top: number; left: number } | null>(null);
  // The mirror's IconButton is a plain function component and forwards no
  // ref, so the anchor is the wrapper; it boxes the button exactly.
  const triggerRef = useRef<HTMLSpanElement>(null);
  const menuRef = useRef<HTMLDivElement>(null);

  const focusTrigger = () => triggerRef.current?.querySelector('button')?.focus();

  const place = useCallback(() => {
    const trigger = triggerRef.current;
    const menu = menuRef.current;
    if (!trigger || !menu) return;

    const anchor = trigger.getBoundingClientRect();
    const panel = menu.getBoundingClientRect();
    const vw = window.innerWidth;
    const vh = window.innerHeight;

    // Below the trigger by default; above it when that would leave the
    // viewport, and pinned to the bottom edge when neither side fits.
    let top = anchor.bottom + GAP;
    if (top + panel.height > vh - EDGE) {
      const above = anchor.top - GAP - panel.height;
      top = above >= EDGE ? above : Math.max(EDGE, vh - EDGE - panel.height);
    }

    // Right-aligned with the trigger, which itself sits at the right edge of
    // the table, then clamped so a narrow window cannot push it off-screen.
    const left = Math.min(
      Math.max(EDGE, anchor.right - panel.width),
      Math.max(EDGE, vw - EDGE - panel.width),
    );

    setPos({ top, left });
  }, []);

  // Runs before paint, so the first frame is already in the right place.
  useLayoutEffect(() => {
    if (open) place();
    else setPos(null);
  }, [open, place]);

  useEffect(() => {
    if (!open) return;

    const onPointerDown = (event: MouseEvent) => {
      const target = event.target as Node;
      if (menuRef.current?.contains(target) || triggerRef.current?.contains(target)) return;
      setOpen(false);
    };
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        setOpen(false);
        focusTrigger();
      }
    };
    // Capture: the table scrolls inside its own box, which never bubbles.
    const onReflow = () => place();

    document.addEventListener('mousedown', onPointerDown);
    document.addEventListener('keydown', onKeyDown);
    window.addEventListener('scroll', onReflow, true);
    window.addEventListener('resize', onReflow);
    return () => {
      document.removeEventListener('mousedown', onPointerDown);
      document.removeEventListener('keydown', onKeyDown);
      window.removeEventListener('scroll', onReflow, true);
      window.removeEventListener('resize', onReflow);
    };
  }, [open, place]);

  const run = (item: RowActionItem) => {
    if (item.disabled) return;
    setOpen(false);
    item.onClick?.();
  };

  // Arrow keys walk the menu; the list is short, so wrapping is enough.
  const onMenuKeyDown = (event: React.KeyboardEvent<HTMLDivElement>) => {
    if (event.key !== 'ArrowDown' && event.key !== 'ArrowUp') return;
    event.preventDefault();
    const buttons = Array.from(
      menuRef.current?.querySelectorAll<HTMLButtonElement>('button:not([disabled])') ?? [],
    );
    if (buttons.length === 0) return;
    const step = event.key === 'ArrowDown' ? 1 : -1;
    const current = buttons.indexOf(document.activeElement as HTMLButtonElement);
    if (current === -1) {
      buttons[step === 1 ? 0 : buttons.length - 1]?.focus();
      return;
    }
    buttons[(current + step + buttons.length) % buttons.length]?.focus();
  };

  const ordered = [...items.filter((i) => !i.danger), ...items.filter((i) => i.danger)];
  const firstDangerIndex = ordered.findIndex((i) => i.danger);

  return (
    <>
      <span ref={triggerRef} className={className} style={{ display: 'inline-flex' }}>
        <IconButton
          variant="standard"
          selected={open}
          aria-label={label}
          aria-haspopup="menu"
          aria-expanded={open}
          title={label}
          onClick={() => setOpen((prev) => !prev)}
          icon={<Icon name="more_vert" size={18} />}
          style={{
            width: '32px',
            height: '32px',
            borderRadius: 'var(--radius-md)',
            backgroundColor: open ? 'var(--color-surface-container-high)' : 'transparent',
          }}
        />
      </span>

      {createPortal(
        <AnimatePresence>
          {open && (
            <motion.div
              ref={menuRef}
              role="menu"
              aria-label={label}
              onKeyDown={onMenuKeyDown}
              initial={{ opacity: 0, scale: 0.96, y: -4 }}
              animate={{ opacity: 1, scale: 1, y: 0 }}
              exit={{ opacity: 0, scale: 0.96, y: -4 }}
              transition={{ duration: 0.14 }}
              style={{
                position: 'fixed',
                top: pos?.top ?? 0,
                left: pos?.left ?? 0,
                // Measured before it is seen: no position, no paint.
                visibility: pos ? 'visible' : 'hidden',
                zIndex: 1100,
                minWidth: '184px',
                padding: 'var(--space-1)',
                borderRadius: 'var(--radius-md)',
                backgroundColor: 'var(--color-surface-container)',
                border: '1px solid var(--color-border)',
                boxShadow: 'var(--elevation-3)',
                display: 'flex',
                flexDirection: 'column',
                gap: '2px',
              }}
            >
              {ordered.map((item, index) => (
                <React.Fragment key={item.id}>
                  {index === firstDangerIndex && index > 0 && (
                    <div
                      role="separator"
                      style={{
                        height: '1px',
                        margin: 'var(--space-1) 0',
                        backgroundColor: 'var(--color-border)',
                      }}
                    />
                  )}
                  <button
                    role="menuitem"
                    type="button"
                    disabled={item.disabled}
                    onClick={() => run(item)}
                    style={{
                      display: 'flex',
                      alignItems: 'center',
                      gap: 'var(--space-2)',
                      width: '100%',
                      padding: 'var(--space-2) var(--space-3)',
                      borderRadius: 'var(--radius-sm)',
                      border: 'none',
                      backgroundColor: 'transparent',
                      color: item.danger ? 'var(--color-error)' : 'var(--color-on-surface)',
                      fontSize: '13px',
                      fontWeight: 600,
                      textAlign: 'left',
                      whiteSpace: 'nowrap',
                      cursor: item.disabled ? 'not-allowed' : 'pointer',
                      opacity: item.disabled ? 0.38 : 1,
                      transition: 'background-color 0.12s ease',
                    }}
                    onMouseEnter={(e) => {
                      if (!item.disabled)
                        e.currentTarget.style.backgroundColor = 'var(--color-surface-container-high)';
                    }}
                    onMouseLeave={(e) => {
                      e.currentTarget.style.backgroundColor = 'transparent';
                    }}
                    onFocus={(e) => {
                      if (!item.disabled)
                        e.currentTarget.style.backgroundColor = 'var(--color-surface-container-high)';
                    }}
                    onBlur={(e) => {
                      e.currentTarget.style.backgroundColor = 'transparent';
                    }}
                  >
                    {item.icon && <Icon name={item.icon} size={16} />}
                    <span>{item.label}</span>
                  </button>
                </React.Fragment>
              ))}
            </motion.div>
          )}
        </AnimatePresence>,
        document.body,
      )}
    </>
  );
};
