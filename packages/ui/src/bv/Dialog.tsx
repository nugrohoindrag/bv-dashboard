import React from 'react';
import { Modal, type ModalProps } from '../components/overlays/index.js';
import { Button } from '../components/actions/index.js';

/**
 * BuildingVision, Dialog ( Overlays).
 *
 * The spec lists a confirmation dialog alongside the general Modal frame; the
 * system ships the frame, so this composes the confirm/cancel affordance on
 * top of it rather than reimplementing the overlay, backdrop or motion.
 */
export interface DialogProps extends Omit<ModalProps, 'children'> {
  headline?: string;
  supportingText?: string;
  confirmLabel?: string;
  cancelLabel?: string;
  /** Destructive confirms get the error-toned action. */
  destructive?: boolean;
  onConfirm?: () => void;
  children?: React.ReactNode;
}

export const Dialog: React.FC<DialogProps> = ({
  headline,
  supportingText,
  confirmLabel = 'Konfirmasi',
  cancelLabel = 'Batal',
  destructive = false,
  onConfirm,
  title,
  children,
  onClose,
  ...modalProps
}) => {
  return (
    <Modal {...modalProps} onClose={onClose} title={title || headline}>
      {supportingText && (
        <p
          style={{
            margin: '0 0 16px',
            fontSize: 'var(--font-size-body)',
            color: 'var(--color-on-surface-variant)',
          }}
        >
          {supportingText}
        </p>
      )}

      {children}

      {onConfirm && (
        <div
          style={{
            display: 'flex',
            justifyContent: 'flex-end',
            gap: 'var(--space-2, 8px)',
            marginTop: 'var(--space-6, 24px)',
          }}
        >
          <Button variant="text" onClick={onClose}>
            {cancelLabel}
          </Button>
          <Button
            variant="filled"
            onClick={onConfirm}
            style={
              destructive
                ? { backgroundColor: 'var(--color-error)', color: 'var(--color-on-error)' }
                : undefined
            }
          >
            {confirmLabel}
          </Button>
        </div>
      )}
    </Modal>
  );
};
