import React from 'react';
import { motion, type HTMLMotionProps, type Variants } from 'motion/react';
import { M3_TRANSITIONS, M3_STAGGER, fade, useReducedMotionSafe } from '../motion/index.js';

/**
 * BuildingVision, page entrance orchestration ( Motion, Reveals).
 *
 * Most system components already animate themselves on mount. What they cannot
 * do is agree on an *order*: mounted together they all arrive in the same
 * frame, which reads as a flash rather than a reveal. These three wrappers
 * supply the missing layer.
 *
 * <Page> route content, drives the sequence
 * <Section>…</Section> a band of the page, arrives in turn
 * <Section stagger> …and cascades its own children
 * <Item>…</Item> a tile in a KPI row or grid
 * </Section>
 * </Page>
 *
 * Orchestration rides on Motion's variant propagation: `Page` sets the
 * `hidden` → `visible` labels once, and every descendant that declares
 * `variants` (and no `initial`/`animate` of its own) inherits them. So the
 * sequence is declared by nesting, not by hand-counted delays.
 *
 * Under `prefers-reduced-motion` every level falls back to opacity only,
 *'s stated fallback, and the stagger collapses to zero, so the page still
 * arrives as one piece instead of not arriving at all.
 */

const RISE = 10;

const sectionVariants = (reduced: boolean, stagger: boolean): Variants => {
  if (reduced) {
    return {
      hidden: { opacity: 0 },
      visible: {
        opacity: 1,
        transition: { ...M3_TRANSITIONS.enter, staggerChildren: 0 },
      },
    };
  }
  return {
    hidden: { opacity: 0, y: RISE },
    visible: {
      opacity: 1,
      y: 0,
      transition: { ...M3_TRANSITIONS.enter, staggerChildren: stagger ? M3_STAGGER.tight : 0 },
    },
  };
};

export interface PageProps extends Omit<HTMLMotionProps<'div'>, 'children'> {
  children?: React.ReactNode;
  /** Per-band delay. Defaults to the "normal" step. */
  stagger?: number;
}

export const Page: React.FC<PageProps> = ({ children, stagger, className = '', ...props }) => {
  const reduced = useReducedMotionSafe();
  const step = reduced ? 0 : (stagger ?? M3_STAGGER.normal);

  return (
    <motion.div
      initial="hidden"
      animate="visible"
      variants={{
        hidden: {},
        visible: { transition: { staggerChildren: step, delayChildren: 0.04 } },
      }}
      className={`bv-page ${className}`}
      {...props}
    >
      {children}
    </motion.div>
  );
};

export interface SectionProps extends Omit<HTMLMotionProps<'div'>, 'children'> {
  children?: React.ReactNode;
  /** Cascade this section's own children. Use on KPI rows and card grids. */
  stagger?: boolean;
}

export const Section: React.FC<SectionProps> = ({ children, stagger = false, className = '', ...props }) => {
  const reduced = useReducedMotionSafe();
  return (
    <motion.div variants={sectionVariants(reduced, stagger)} className={`bv-section ${className}`} {...props}>
      {children}
    </motion.div>
  );
};

/**
 * Variants for a single tile in a cascading section.
 *
 * A component that declares these, and sets no `initial`/`animate` of its own
 * inherits the labels from whatever `<Section stagger>` or `<Page>` is above
 * it, and joins the cascade. With no such parent the variants are inert, so a
 * component carrying them is still safe to render anywhere.
 *
 * `SurfaceCard` and `MetricCard` use this, which is why a KPI row cascades
 * without each tile being wrapped by hand.
 */
export const useItemVariants = (): Variants => {
  const reduced = useReducedMotionSafe();
  return reduced
    ? fade
    : {
        hidden: { opacity: 0, y: RISE },
        visible: { opacity: 1, y: 0, transition: M3_TRANSITIONS.enter },
      };
};

export interface ItemProps extends Omit<HTMLMotionProps<'div'>, 'children'> {
  children?: React.ReactNode;
}

/** Wrapper form of `useItemVariants`, for tiles you cannot give variants to. */
export const Item: React.FC<ItemProps> = ({ children, className = '', ...props }) => (
  <motion.div variants={useItemVariants()} className={`bv-item ${className}`} {...props}>
    {children}
  </motion.div>
);
