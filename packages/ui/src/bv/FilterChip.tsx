import React from 'react';
import { Chip, type ChipProps } from '../components/selection/Chips.js';

/**
 * A `Chip variant="filter"` whose selected state fills solid `--color-
 * primary` instead of the mirror's `--md-sys-color-secondary-container`.
 *
 * BuildingVision's rule is one fill for every selected control, the active
 * entity tab, the hero, a CTA button, and a selected filter chip all read
 * "this is the current choice" the same way: solid `--color-primary` /
 * `--color-on-primary`. `Chip` ties its own selected state to the
 * *secondary* accent, which is a second, dimmer colour for the same idea;
 * this override keeps the selected fill on the one accent the rest of the
 * product uses. Unselected chips stay on the neutral surface ladder.
 * Everything else about `Chip`, shape, motion, the check mark, is
 * unchanged.
 */
export const FilterChip: React.FC<ChipProps> = ({ selected, style, ...props }) => (
  <Chip
    variant="filter"
    selected={selected}
    style={{
      backgroundColor: selected ? 'var(--color-primary)' : 'var(--color-surface-container-low)',
      color: selected ? 'var(--color-on-primary)' : 'var(--color-on-surface-variant)',
      border: selected ? 'none' : '1px solid var(--color-border)',
      ...style,
    }}
    {...props}
  />
);
