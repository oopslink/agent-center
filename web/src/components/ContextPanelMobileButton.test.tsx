import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { ContextPanelMobileButton } from './ContextPanelMobileButton';

describe('ContextPanelMobileButton', () => {
  afterEach(() => cleanup());

  it('calls onClick when tapped', () => {
    const onClick = vi.fn();
    render(<ContextPanelMobileButton onClick={onClick} />);
    fireEvent.click(screen.getByTestId('context-panel-mobile-open'));
    expect(onClick).toHaveBeenCalledTimes(1);
  });

  // Regression: the button shipped reading `shell.contextPanel.openMobileSheet`
  // from the default ('common') namespace, but that key existed in NEITHER
  // locale — so i18next fell back to returning the key itself and the ⓘ's
  // accessible name was the literal string "shell.contextPanel.openMobileSheet".
  // The existing entry tests only ever queried by test-id, so nothing caught it.
  it('resolves a real translated accessible name (not the raw i18n key)', () => {
    render(<ContextPanelMobileButton onClick={() => {}} />);
    const btn = screen.getByTestId('context-panel-mobile-open');
    const label = btn.getAttribute('aria-label') ?? '';
    expect(label).not.toContain('shell.contextPanel');
    expect(label).toBe('Show details');
    expect(btn).toHaveAttribute('title', 'Show details');
  });

  it('keeps a ≥44px touch target (v2.10.1 touch baseline)', () => {
    render(<ContextPanelMobileButton onClick={() => {}} />);
    const btn = screen.getByTestId('context-panel-mobile-open');
    expect(btn.className).toContain('min-h-[2.75rem]');
    expect(btn.className).toContain('min-w-[2.75rem]');
  });
});
