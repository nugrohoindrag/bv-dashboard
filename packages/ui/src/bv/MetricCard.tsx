import React, { useId } from 'react';
import { motion } from 'motion/react';
import { Icon } from '../components/communication/index.js';
import { type Tone, toneColor, toneOnColor } from './tones.js';
import { useItemVariants } from './PageMotion.js';

export interface MetricCardProps {
  label: string;
  value: string;
  delta?: string;
  deltaType?: 'positive' | 'negative' | 'neutral';
  subValue?: string;
  statusBadge?: string;
  statusType?: 'success' | 'warning' | 'error' | 'neutral';
  tone?: Tone;
  icon?: React.ReactNode;
  sparklineData?: number[];
  className?: string;
  style?: React.CSSProperties;
}

export const MetricCard: React.FC<MetricCardProps> = ({
  label,
  value,
  delta,
  deltaType = 'positive',
  subValue,
  statusBadge,
  statusType,
  tone,
  icon,
  sparklineData = [20, 45, 30, 60, 50, 80, 70],
  className = '',
  style,
}) => {
  const itemVariants = useItemVariants();
  const gradId = useId().replace(/:/g, '');

  // Determine effective tone & accent color based on tone prop or statusType fallback
  const resolvedTone: Tone = tone
    ? tone
    : statusType === 'warning'
      ? 'warning'
      : statusType === 'error'
        ? 'error'
        : statusType === 'neutral'
          ? 'neutral'
          : statusType === 'success'
            ? 'success'
            : 'primary';

  const accent = toneColor[resolvedTone] || 'var(--color-primary)';

  const isPositive = deltaType === 'positive';
  const deltaColor =
    deltaType === 'negative'
      ? 'var(--color-error)'
      : deltaType === 'neutral'
        ? 'var(--color-on-surface-variant)'
        : accent;

  const max = Math.max(...sparklineData, 1);
  const min = Math.min(...sparklineData, 0);
  const points = sparklineData.map((val, idx) => {
    const x = (idx / (sparklineData.length - 1)) * 90;
    const y = 30 - ((val - min) / (max - min || 1)) * 24;
    return { x, y, str: `${x},${y}` };
  });

  const pointsString = points.map((p) => p.str).join(' ');
  const areaPoints = `0,34 ${pointsString} 90,34`;
  const lastPoint = points[points.length - 1];

  return (
    <motion.div
      whileHover={{ y: -3, scale: 1.01, transition: { duration: 0.2, ease: 'easeOut' } }}
      variants={itemVariants}
      style={{
        borderRadius: 'var(--radius-lg, 16px)',
        backgroundColor: 'var(--color-surface)',
        border: '1px solid var(--color-border)',
        padding: '12px 14px',
        color: 'var(--color-on-surface)',
        display: 'flex',
        flexDirection: 'column',
        justifyContent: 'space-between',
        height: '100%',
        minHeight: '116px',
        boxShadow: 'var(--elevation-1)',
        position: 'relative',
        overflow: 'hidden',
        ...style,
      }}
      className={`morphic-metric-card ${className}`}
    >
      {/* Concentric Radar Circular Vector Artwork in Background (Aligned with Color Palette) */}
      <svg
        style={{
          position: 'absolute',
          top: 0,
          right: 0,
          width: '110px',
          height: '100%',
          pointerEvents: 'none',
          opacity: 0.35,
          overflow: 'visible',
        }}
        viewBox="0 0 130 110"
        fill="none"
      >
        {/* Outer Circular Ring with subtle slow rotation */}
        <motion.circle
          cx="102"
          cy="30"
          r="46"
          stroke={accent}
          strokeWidth="1.2"
          strokeDasharray="4 4"
          animate={{ rotate: 360 }}
          transition={{ duration: 40, repeat: Infinity, ease: 'linear' }}
          style={{ transformOrigin: '102px 30px' }}
        />
        {/* Middle Circular Ring */}
        <circle cx="102" cy="30" r="28" stroke={accent} strokeWidth="1" strokeDasharray="3 3" opacity="0.8" />
        {/* Inner Circular Ring */}
        <circle cx="102" cy="30" r="16" stroke={accent} strokeWidth="0.8" strokeDasharray="2 3" opacity="0.6" />
        {/* Subtle Crosshair Axis Lines */}
        <path
          d="M 102 0 V 85 M 35 30 H 130"
          stroke={accent}
          strokeWidth="0.9"
          strokeDasharray="2 4"
          opacity="0.7"
        />
        {/* Diagonal Guideline */}
        <path d="M 68 64 L 130 0" stroke={accent} strokeWidth="0.8" strokeDasharray="3 4" opacity="0.4" />
      </svg>

      {/* Top Header */}
      <div
        style={{
          display: 'flex',
          justifyContent: 'space-between',
          alignItems: 'center',
          position: 'relative',
          zIndex: 1,
        }}
      >
        <span style={{ fontSize: '11px', color: 'var(--color-on-surface-variant)', fontWeight: 600 }}>
          {label}
        </span>
        {icon && (
          <motion.div
            whileHover={{ scale: 1.1, rotate: 6 }}
            style={{
              width: '24px',
              height: '24px',
              borderRadius: 'var(--radius-pill)',
              backgroundColor: accent,
              border: 'none',
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              color: toneOnColor[resolvedTone],
            }}
          >
            {icon}
          </motion.div>
        )}
      </div>

      {/* Bottom Metric & Live Waveform */}
      <div
        style={{
          display: 'flex',
          justifyContent: 'space-between',
          alignItems: 'flex-end',
          marginTop: '10px',
          position: 'relative',
          zIndex: 1,
        }}
      >
        <div>
          <div
            style={{
              fontSize: '17px',
              fontWeight: 800,
              letterSpacing: '-0.02em',
              lineHeight: 1.1,
              fontFeatureSettings: '"tnum" 1',
            }}
          >
            {value}
          </div>

          {/* Delta / Status / Subtitle text */}
          <div
            style={{ display: 'flex', alignItems: 'center', gap: '6px', marginTop: '3px', flexWrap: 'wrap' }}
          >
            {statusBadge && (
              <span
                style={{
                  fontSize: '11px',
                  fontWeight: 700,
                  color: accent,
                }}
              >
                {statusBadge}
              </span>
            )}
            {delta && (
              <div
                style={{
                  fontSize: '11px',
                  fontWeight: 600,
                  color: deltaColor,
                  display: 'flex',
                  alignItems: 'center',
                  gap: '2px',
                }}
              >
                <Icon
                  name={
                    isPositive ? 'trending_up' : deltaType === 'negative' ? 'trending_down' : 'trending_flat'
                  }
                  size={13}
                />
                <span>{delta}</span>
              </div>
            )}
            {subValue && (
              <span style={{ fontSize: '11px', color: 'var(--color-on-surface-variant)', fontWeight: 500 }}>
                {subValue}
              </span>
            )}
          </div>
        </div>

        {/* High-Tech Animated Vector Sparkline */}
        <div style={{ width: '76px', height: '28px', overflow: 'visible' }}>
          <svg width="90" height="34" viewBox="0 0 90 34" fill="none" style={{ overflow: 'visible' }}>
            <defs>
              <linearGradient id={`sparkGrad-${gradId}`} x1="0" y1="0" x2="0" y2="1">
                <stop offset="0%" stopColor={accent} stopOpacity="0.28" />
                <stop offset="100%" stopColor={accent} stopOpacity="0.0" />
              </linearGradient>
            </defs>

            {/* Gradient Area Fill */}
            <polygon points={areaPoints} fill={`url(#sparkGrad-${gradId})`} />

            {/* Drawing Vector Waveform Line */}
            <motion.polyline
              points={pointsString}
              fill="none"
              stroke={accent}
              strokeWidth="2.2"
              strokeLinecap="round"
              strokeLinejoin="round"
              initial={{ pathLength: 0 }}
              animate={{ pathLength: 1 }}
              transition={{ duration: 0.9, ease: 'easeOut' }}
            />

            {/* Pulsing Leading Data Node */}
            {lastPoint && (
              <motion.circle
                cx={lastPoint.x}
                cy={lastPoint.y}
                r={2.5}
                fill={accent}
                // `initial` is required, not decorative: animating `r` as a
                // keyframe array without a defined starting value makes Motion
                // write `r="undefined"` on the first frame, which the SVG
                // parser rejects, eight KPI cards, eight console errors on
                // every dashboard load.
                initial={{ r: 2.5, opacity: 0.7 }}
                animate={{ r: [2.5, 4.5, 2.5], opacity: [0.7, 1, 0.7] }}
                transition={{ duration: 1.8, repeat: Infinity, ease: 'easeInOut' }}
              />
            )}
          </svg>
        </div>
      </div>
    </motion.div>
  );
};

export const KPICard = MetricCard;
