import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { BottomSheet } from './BottomSheet';

describe('BottomSheet (v2.10.1 [M1] generic mobile sheet)', () => {
  afterEach(() => cleanup());

  it('renders nothing when closed', () => {
    render(
      <BottomSheet open={false} onClose={() => {}} testId="sheet">
        body
      </BottomSheet>,
    );
    expect(screen.queryByTestId('sheet')).not.toBeInTheDocument();
  });

  it('renders an accessible dialog with a title, grab handle and content when open', () => {
    render(
      <BottomSheet open onClose={() => {}} title="Participants" testId="sheet">
        <div data-testid="body">3 people</div>
      </BottomSheet>,
    );
    const sheet = screen.getByTestId('sheet');
    expect(sheet).toHaveAttribute('role', 'dialog');
    expect(sheet).toHaveAttribute('aria-modal', 'true');
    // aria-labelledby wires to the visible title.
    const labelledBy = sheet.getAttribute('aria-labelledby');
    expect(labelledBy).toBeTruthy();
    expect(sheet.querySelector(`#${labelledBy}`)).toHaveTextContent('Participants');
    expect(screen.getByTestId('body')).toHaveTextContent('3 people');
  });

  it('falls back to ariaLabel when there is no visible title', () => {
    render(
      <BottomSheet open onClose={() => {}} ariaLabel="Account menu" testId="sheet">
        x
      </BottomSheet>,
    );
    const sheet = screen.getByTestId('sheet');
    expect(sheet).toHaveAttribute('aria-label', 'Account menu');
    expect(sheet).not.toHaveAttribute('aria-labelledby');
  });

  it('dismisses on scrim tap and on Escape', () => {
    const onClose = vi.fn();
    render(
      <BottomSheet open onClose={onClose} testId="sheet">
        x
      </BottomSheet>,
    );
    fireEvent.click(screen.getByTestId('bottom-sheet-scrim'));
    expect(onClose).toHaveBeenCalledTimes(1);
    fireEvent.keyDown(document, { key: 'Escape' });
    expect(onClose).toHaveBeenCalledTimes(2);
  });

  it('closes when the grab handle is dragged down past the close threshold', () => {
    const onClose = vi.fn();
    render(
      <BottomSheet open onClose={onClose} testId="sheet">
        x
      </BottomSheet>,
    );
    const handle = screen.getByTestId('bottom-sheet-drag-handle');
    fireEvent.pointerDown(handle, { clientY: 100, pointerId: 1 });
    fireEvent.pointerMove(handle, { clientY: 220, pointerId: 1 });
    fireEvent.pointerUp(handle, { clientY: 220, pointerId: 1 });
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it('does not close when the grab handle drag stays under the close threshold', () => {
    const onClose = vi.fn();
    render(
      <BottomSheet open onClose={onClose} testId="sheet">
        x
      </BottomSheet>,
    );
    const handle = screen.getByTestId('bottom-sheet-drag-handle');
    fireEvent.pointerDown(handle, { clientY: 100, pointerId: 1 });
    fireEvent.pointerMove(handle, { clientY: 140, pointerId: 1 });
    fireEvent.pointerUp(handle, { clientY: 140, pointerId: 1 });
    expect(onClose).not.toHaveBeenCalled();
  });
});
