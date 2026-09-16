import React from 'react';
import { motion, type HTMLMotionProps } from 'motion/react';
import { type Tone, toneColor } from './tones.js';
import { useItemVariants } from './PageMotion.js';

/**
 * BuildingVision, the house card surface ( Surfaces & Containers).
 *
 * Every card-shaped surface the design system ships, MetricCard,
 * PlantMetricCard, HorizontalBarChart, ThroughputChartCard, DefectParetoCard,
 * is built from one recipe: a filled `surface` panel, a hairline `border`, a
 * 22px radius and elevation 1. This component is that recipe, named.
 *
 * Product pages should reach for this rather than `Card variant="filled"`,
 * whose `surface-container-highest` fill reads as a tinted box next to the
 * cards around it.
 *
 * `railTone` adds a status rail on the leading edge, the andon signal for
 * boards where a line's state has to be readable across the room. The fill
 * stays quiet either way; only the rail carries the colour.
 */
export interface SurfaceCardProps extends Omit<HTMLMotionProps<'div'>, 'children'> {
  children?: React.ReactNode;
  padding?: 'none' | 'sm' | 'md' | 'lg';
  railTone?: Tone;
  /** Lifts on hover. Use for cards that navigate or open something. */
  interactive?: boolean;
}

const PADDING: Record<NonNullable<SurfaceCardProps['padding']>, string> = {
  none: '0',
  sm: '12px',
  md: '16px',
  lg: '24px',
};

export const SurfaceCard: React.FC<SurfaceCardProps> = ({
  children,
  padding = 'md',
  railTone,
  interactive = false,
  className = '',
  style,
  ...props
}) => {
  const itemVariants = useItemVariants();

  return (
    <motion.div
      whileHover={interactive ? { y: -2, transition: { duration: 0.18 } } : undefined}
      variants={itemVariants}
      style={{
        borderRadius: 'var(--radius-xl)',
        backgroundColor: 'var(--color-surface)',
        border: '1px solid var(--color-border)',
        borderLeft: railTone ? `6px solid ${toneColor[railTone]}` : undefined,
        boxShadow: 'var(--elevation-1)',
        padding: PADDING[padding],
        color: 'var(--color-on-surface)',
        cursor: interactive ? 'pointer' : 'default',
        position: 'relative',
        ...style,
      }}
      className={`bv-surface-card ${className}`}
      {...props}
    >
      {children}
    </motion.div>
  );
};
