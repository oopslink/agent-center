import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { ForceDeleteModal } from './ForceDeleteModal';

afterEach(() => cleanup());

const noop = () => {};

describe('ForceDeleteModal', () => {
  it('renders nothing when closed', () => {
    render(
      <ForceDeleteModal
        open={false}
        entityKind="agent"
        entityName="bot-1"
        onConfirm={noop}
        onCancel={noop}
      />,
    );
    expect(screen.queryByTestId('force-delete-modal')).toBeNull();
  });

  it('renders the typed-name input + a destructive confirm button when open', () => {
    render(
      <ForceDeleteModal
        open
        entityKind="agent"
        entityName="bot-1"
        onConfirm={noop}
        onCancel={noop}
      />,
    );
    expect(screen.getByTestId('force-delete-modal')).toBeInTheDocument();
    expect(screen.getByTestId('force-delete-input')).toBeInTheDocument();
    // the input is labelled (associated <label> / aria-label)
    expect(screen.getByLabelText(/type bot-1 to confirm/i)).toBe(
      screen.getByTestId('force-delete-input'),
    );
    expect(screen.getByTestId('force-delete-confirm')).toHaveTextContent('Force delete');
  });

  it('confirm is disabled when the input is empty', () => {
    render(
      <ForceDeleteModal open entityKind="agent" entityName="bot-1" onConfirm={noop} onCancel={noop} />,
    );
    expect(screen.getByTestId('force-delete-confirm')).toBeDisabled();
  });

  it('confirm stays disabled for a partial / wrong name', () => {
    render(
      <ForceDeleteModal open entityKind="agent" entityName="bot-1" onConfirm={noop} onCancel={noop} />,
    );
    const input = screen.getByTestId('force-delete-input');
    fireEvent.change(input, { target: { value: 'bot-' } });
    expect(screen.getByTestId('force-delete-confirm')).toBeDisabled();
    fireEvent.change(input, { target: { value: 'bot-2' } });
    expect(screen.getByTestId('force-delete-confirm')).toBeDisabled();
    // case must match exactly
    fireEvent.change(input, { target: { value: 'Bot-1' } });
    expect(screen.getByTestId('force-delete-confirm')).toBeDisabled();
  });

  it('confirm enables only when the input === entityName exactly, and fires onConfirm', () => {
    const onConfirm = vi.fn();
    render(
      <ForceDeleteModal
        open
        entityKind="worker"
        entityName="Worker One"
        onConfirm={onConfirm}
        onCancel={noop}
      />,
    );
    const input = screen.getByTestId('force-delete-input');
    fireEvent.change(input, { target: { value: 'Worker One' } });
    const confirm = screen.getByTestId('force-delete-confirm');
    expect(confirm).toBeEnabled();
    fireEvent.click(confirm);
    expect(onConfirm).toHaveBeenCalledTimes(1);
  });

  it('disables input + buttons while busy (even when the name matches)', () => {
    render(
      <ForceDeleteModal
        open
        entityKind="agent"
        entityName="bot-1"
        busy
        onConfirm={noop}
        onCancel={noop}
      />,
    );
    fireEvent.change(screen.getByTestId('force-delete-input'), { target: { value: 'bot-1' } });
    expect(screen.getByTestId('force-delete-confirm')).toBeDisabled();
    expect(screen.getByTestId('force-delete-cancel')).toBeDisabled();
    expect(screen.getByTestId('force-delete-input')).toBeDisabled();
  });

  it('renders the error when set', () => {
    render(
      <ForceDeleteModal
        open
        entityKind="worker"
        entityName="w"
        error="worker_busy"
        onConfirm={noop}
        onCancel={noop}
      />,
    );
    expect(screen.getByTestId('force-delete-error')).toHaveTextContent('worker_busy');
  });

  it('fires onCancel from the Cancel button and on Escape (modal a11y)', () => {
    const onCancel = vi.fn();
    render(
      <ForceDeleteModal open entityKind="agent" entityName="bot-1" onConfirm={noop} onCancel={onCancel} />,
    );
    fireEvent.click(screen.getByTestId('force-delete-cancel'));
    expect(onCancel).toHaveBeenCalledTimes(1);
    fireEvent.keyDown(document, { key: 'Escape' });
    expect(onCancel).toHaveBeenCalledTimes(2);
  });

  it('worker copy mentions unbinding agents', () => {
    render(
      <ForceDeleteModal open entityKind="worker" entityName="w" onConfirm={noop} onCancel={noop} />,
    );
    expect(screen.getByTestId('force-delete-message')).toHaveTextContent(/unbinds its agents/i);
  });
});
