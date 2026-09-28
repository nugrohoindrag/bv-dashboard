/**
 * @license MIT
 * Tabs Component — Material Design 3 Navigation
 * 
 * Features:
 * - Primary & Pill tabs: Soft Frame pill track with an ink (secondary) active pill
 *   that slides between tabs (layoutId); Secondary: underline tabs

 * - M3 Spring Physics & Emphasized Easing
 * - Icon, Badge, and Disabled states
 * 
 * Copyright (c) 2026 Morphic Design System Contributors
 */

import React from 'react';
import { motion } from 'motion/react';
import { Icon } from '../communication/Icon.js';
import { M3_SPRING } from '../../motion/index.js';

export interface TabItem {
  id: string;
  label: string;
  icon?: React.ReactNode | string;
  badge?: string | number;
  disabled?: boolean;
}

export interface TabsProps {
  tabs: TabItem[];
  activeTab: string;
  onChange: (id: string) => void;
  variant?: 'primary' | 'secondary' | 'pills';
  fullWidth?: boolean;
  className?: string;
}

export const Tabs: React.FC<TabsProps> = ({
  tabs,
  activeTab,
  onChange,
  variant = 'primary',
  fullWidth = false,
  className = '',
}) => {
  const renderIcon = (icon?: React.ReactNode | string) => {
    if (!icon) return null;
    if (typeof icon === 'string') return <Icon name={icon} size={18} />;
    return icon;
  };

  // Soft Frame (29 Sep 2026): `primary` = pill track (white + hairline) holding the tabs; `pills` = the same
  // pills without the track; `secondary` keeps the underline style for dense sub-navigation.
  const pill = variant !== 'secondary';
  const track = variant === 'primary';
  return (
    <div
      role="tablist"
      className={`m3-tabs m3-tabs-${variant} ${className}`}
      style={{
        display: fullWidth ? 'flex' : 'inline-flex',
        alignItems: 'center',
        gap: pill ? '2px' : '0',
        borderBottom: pill ? 'none' : '1px solid var(--md-sys-color-border)',
        padding: track ? '4px' : 0,
        borderRadius: track ? 'var(--radius-pill)' : undefined,
        backgroundColor: track ? 'var(--md-sys-color-surface)' : undefined,
        border: track ? '1px solid var(--md-sys-color-border)' : undefined,
        boxShadow: track ? 'var(--md-sys-elevation-level1)' : undefined,
        position: 'relative',
        width: fullWidth ? '100%' : 'auto',
        maxWidth: '100%',
        overflowX: 'auto',
        scrollbarWidth: 'none',
      }}
    >
      {tabs.map((tab) => {
        const isActive = tab.id === activeTab;

        if (pill) {
          return (
            <button
              key={tab.id}
              role="tab"
              aria-selected={isActive}
              disabled={tab.disabled}
              onClick={() => !tab.disabled && onChange(tab.id)}
              style={{
                position: 'relative',
                display: 'inline-flex',
                alignItems: 'center',
                justifyContent: 'center',
                gap: '8px',
                height: '34px',
                padding: '0 14px',
                borderRadius: 'var(--radius-pill)',
                border: 'none',
                backgroundColor: 'transparent',
                color: isActive
                  ? 'var(--md-sys-color-on-secondary)'
                  : 'var(--md-sys-color-on-surface-variant)',
                fontSize: '13px',
                fontWeight: isActive ? 600 : 500,
                whiteSpace: 'nowrap',
                cursor: tab.disabled ? 'not-allowed' : 'pointer',
                opacity: tab.disabled ? 0.38 : 1,
                userSelect: 'none',
                flex: fullWidth ? 1 : 'none',
                transition: 'color 0.15s ease',
              }}
            >
              {isActive && (
                <motion.div
                  layoutId={track ? 'm3ActiveTrackTab' : 'm3ActivePillTab'}
                  transition={M3_SPRING.responsive}
                  style={{
                    position: 'absolute',
                    inset: 0,
                    borderRadius: 'var(--radius-pill)',
                    backgroundColor: 'var(--md-sys-color-secondary)',
                    zIndex: 0,
                  }}
                />
              )}

              <span style={{ position: 'relative', zIndex: 1, display: 'flex', alignItems: 'center', gap: '8px' }}>
                {renderIcon(tab.icon)}
                <span>{tab.label}</span>
                {tab.badge !== undefined && (
                  <span
                    style={{
                      fontSize: '11px',
                      fontWeight: 700,
                      padding: '1px 6px',
                      borderRadius: 'var(--radius-pill)',
                      backgroundColor: isActive ? 'var(--md-sys-color-tertiary)' : 'var(--md-sys-color-surface-container-high)',
                      color: isActive ? 'var(--md-sys-color-on-tertiary)' : 'var(--md-sys-color-on-surface-variant)',
                    }}
                  >
                    {tab.badge}
                  </span>
                )}
              </span>
            </button>
          );
        }

        // Primary / Secondary Underline Tabs
        return (
          <button
            key={tab.id}
            role="tab"
            aria-selected={isActive}
            disabled={tab.disabled}
            onClick={() => !tab.disabled && onChange(tab.id)}
            style={{
              position: 'relative',
              display: 'inline-flex',
              alignItems: 'center',
              justifyContent: 'center',
              gap: '8px',
              padding: '14px 20px',
              border: 'none',
              backgroundColor: 'transparent',
              color: isActive
                ? 'var(--md-sys-color-primary)'
                : 'var(--md-sys-color-on-surface-variant)',
              fontSize: '14px',
              fontWeight: isActive ? 700 : 500,
              cursor: tab.disabled ? 'not-allowed' : 'pointer',
              opacity: tab.disabled ? 0.38 : 1,
              userSelect: 'none',
              flex: fullWidth ? 1 : 'none',
              transition: 'color 0.18s ease',
            }}
          >
            {renderIcon(tab.icon)}
            <span>{tab.label}</span>
            {tab.badge !== undefined && (
              <span
                style={{
                  fontSize: '11px',
                  fontWeight: 700,
                  padding: '1px 6px',
                  borderRadius: 'var(--radius-pill)',
                  backgroundColor: isActive ? 'var(--md-sys-color-primary-container)' : 'var(--md-sys-color-surface-container-high)',
                  color: isActive ? 'var(--md-sys-color-on-primary-container)' : 'var(--md-sys-color-on-surface-variant)',
                }}
              >
                {tab.badge}
              </span>
            )}

            {isActive && (
              <motion.div
                layoutId="m3ActiveUnderlineTab"
                transition={M3_SPRING.responsive}
                style={{
                  position: 'absolute',
                  bottom: '-1px',
                  left: 0,
                  right: 0,
                  height: '3px',
                  borderRadius: '3px 3px 0 0',
                  backgroundColor: 'var(--md-sys-color-primary)',
                  zIndex: 1,
                }}
              />
            )}
          </button>
        );
      })}
    </div>
  );
};
