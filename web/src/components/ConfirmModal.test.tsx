import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { ConfirmModal } from './ConfirmModal';

afterEach(() => cleanup());

const noop = () => {};

describe('ConfirmModal', () => {
  it('renders nothing when closed', () => {
    render(<ConfirmModal open={false} title="Remove?" onConfirm={noop} onCancel={noop} />);
    expect(screen.queryByTestId('confirm-modal')).toBeNull();
  });

  it('renders title + message + default labels when open', () => {
    render(
      <ConfirmModal
        open
        title="Remove worker?"
        message="This cannot be undone."
        onConfirm={noop}
        onCancel={noop}
      />,
    );
    expect(screen.getByTestId('confirm-modal')).toBeInTheDocument();
    expect(screen.getByText('Remove worker?')).toBeInTheDocument();
    expect(screen.getByTestId('confirm-modal-message')).toHaveTextContent('This cannot be undone.');
    expect(screen.getByTestId('confirm-modal-confirm')).toHaveTextContent('Confirm');
    expect(screen.getByTestId('confirm-modal-cancel')).toHaveTextContent('Cancel');
  });

  it('uses custom confirm/cancel labels', () => {
    render(
      <ConfirmModal
        open
        title="t"
        confirmLabel="Revoke"
        cancelLabel="Keep"
        onConfirm={noop}
        onCancel={noop}
      />,
    );
    expect(screen.getByTestId('confirm-modal-confirm')).toHaveTextContent('Revoke');
    expect(screen.getByTestId('confirm-modal-cancel')).toHaveTextContent('Keep');
  });

  it('fires onConfirm when the confirm button is clicked', () => {
    const onConfirm = vi.fn();
    render(<ConfirmModal open title="t" onConfirm={onConfirm} onCancel={noop} />);
    fireEvent.click(screen.getByTestId('confirm-modal-confirm'));
    expect(onConfirm).toHaveBeenCalledTimes(1);
  });

  it('fires onCancel when the cancel button is clicked', () => {
    const onCancel = vi.fn();
    render(<ConfirmModal open title="t" onConfirm={noop} onCancel={onCancel} />);
    fireEvent.click(screen.getByTestId('confirm-modal-cancel'));
    expect(onCancel).toHaveBeenCalledTimes(1);
  });

  it('fires onCancel on Escape (modal a11y focus-trap)', () => {
    const onCancel = vi.fn();
    render(<ConfirmModal open title="t" onConfirm={noop} onCancel={onCancel} />);
    fireEvent.keyDown(document, { key: 'Escape' });
    expect(onCancel).toHaveBeenCalledTimes(1);
  });

  it('disables both buttons while busy', () => {
    render(<ConfirmModal open title="t" busy onConfirm={noop} onCancel={noop} />);
    expect(screen.getByTestId('confirm-modal-confirm')).toBeDisabled();
    expect(screen.getByTestId('confirm-modal-cancel')).toBeDisabled();
  });
});
