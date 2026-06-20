import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MessageCopyButton } from './MessageCopyButton';

// T246 — the per-message copy affordance: copies the raw content and shows a
// brief "Copied" confirmation (icon swap + aria-live SR status).
describe('MessageCopyButton', () => {
  beforeEach(() => {
    Object.assign(navigator, { clipboard: { writeText: vi.fn().mockResolvedValue(undefined) } });
  });
  afterEach(() => {
    cleanup();
    vi.restoreAllMocks();
  });

  it('copies the message content and flips to a "Copied" confirmation', async () => {
    render(<MessageCopyButton content={'**hello** world'} />);
    const btn = screen.getByTestId('message-copy-btn');
    expect(btn).toHaveAttribute('aria-label', 'Copy message');
    const status = screen.getByTestId('message-copy-status');
    expect(status).toHaveTextContent('');

    fireEvent.click(btn);
    expect(navigator.clipboard.writeText).toHaveBeenCalledWith('**hello** world');
    await waitFor(() => expect(status).toHaveTextContent('Copied'));
  });

  it('copies the RAW markdown source verbatim (not a rendered/plain form)', () => {
    const raw = '# Title\n\n- a\n- b\n\n`code`';
    render(<MessageCopyButton content={raw} />);
    fireEvent.click(screen.getByTestId('message-copy-btn'));
    expect(navigator.clipboard.writeText).toHaveBeenCalledWith(raw);
  });
});
