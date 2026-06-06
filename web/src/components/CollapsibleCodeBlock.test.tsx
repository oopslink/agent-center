import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, render, screen, fireEvent, waitFor } from '@testing-library/react';
import { CollapsibleCodeBlock } from './CollapsibleCodeBlock';

// v2.8 #276/#274 shared CollapsibleCodeBlock — pure prop-driven (reused by the
// markdown code renderer #276 + the Activity tool_result inline output #274).
// 13 design locks (API + 8 a11y + useId/bidirectional/copy-full).
const lines = (n: number) => Array.from({ length: n }, (_, i) => `line ${i + 1}`).join('\n');

describe('CollapsibleCodeBlock (#276/#274)', () => {
  beforeEach(() => {
    Object.assign(navigator, { clipboard: { writeText: vi.fn().mockResolvedValue(undefined) } });
  });
  afterEach(() => cleanup());

  it('renders full code with NO disclosure when at/under the threshold', () => {
    render(<CollapsibleCodeBlock code={lines(20)} collapsedThreshold={20} />);
    expect(screen.getByTestId('code-region')).toHaveTextContent('line 20');
    expect(screen.queryByTestId('code-disclosure-btn')).toBeNull();
  });

  it('collapses over-threshold code: preview lines + disclosure (aria-expanded=false, aria-controls→region id)', () => {
    render(<CollapsibleCodeBlock code={lines(30)} collapsedThreshold={20} previewLines={5} />);
    const region = screen.getByTestId('code-region');
    // collapsed → only first 5 lines visible, not line 6+.
    expect(region).toHaveTextContent('line 5');
    expect(region).not.toHaveTextContent('line 6');
    // code region not read aloud as content (#192/a11y lock2).
    expect(region).toHaveAttribute('aria-live', 'off');
    const disc = screen.getByTestId('code-disclosure-btn');
    expect(disc).toHaveAttribute('aria-expanded', 'false');
    expect(disc).toHaveTextContent('Show 25 more lines');
    // aria-controls points to the (useId-generated, non-empty) region id.
    const regionId = region.getAttribute('id');
    expect(regionId).toBeTruthy();
    expect(disc).toHaveAttribute('aria-controls', regionId as string);
    // contextual aria-label.
    expect(disc).toHaveAttribute('aria-label', 'Code, 30 lines, collapsed');
  });

  it('disclosure is BIDIRECTIONAL: expand shows all + "Show less", collapse hides again', () => {
    render(<CollapsibleCodeBlock code={lines(30)} collapsedThreshold={20} previewLines={5} />);
    const disc = screen.getByTestId('code-disclosure-btn');
    fireEvent.click(disc);
    expect(disc).toHaveAttribute('aria-expanded', 'true');
    expect(disc).toHaveTextContent('Show less');
    expect(disc).toHaveAttribute('aria-label', 'Code, 30 lines, expanded');
    expect(screen.getByTestId('code-region')).toHaveTextContent('line 30');
    // collapse back
    fireEvent.click(disc);
    expect(disc).toHaveAttribute('aria-expanded', 'false');
    expect(screen.getByTestId('code-region')).not.toHaveTextContent('line 30');
  });

  it('uses a contextual "Output" label when contextLabel="output" (#274 tool_result reuse)', () => {
    render(<CollapsibleCodeBlock code={lines(30)} contextLabel="output" collapsedThreshold={20} />);
    expect(screen.getByTestId('code-disclosure-btn')).toHaveAttribute('aria-label', 'Output, 30 lines, collapsed');
  });

  it('copy writes the FULL code (not the preview) even while collapsed, with SR "Copied" + contextual aria-label', async () => {
    render(<CollapsibleCodeBlock code={lines(30)} collapsedThreshold={20} previewLines={5} />);
    const copy = screen.getByTestId('code-copy-btn');
    expect(copy).toHaveAttribute('aria-label', 'Copy code');
    fireEvent.click(copy);
    expect(navigator.clipboard.writeText).toHaveBeenCalledWith(lines(30));
    const status = screen.getByTestId('code-copy-status');
    expect(status).toHaveAttribute('aria-live', 'polite');
    await waitFor(() => expect(status).toHaveTextContent('Copied'));
  });

  it('shows a language badge when a language is given', () => {
    render(<CollapsibleCodeBlock code={lines(3)} language="ts" />);
    expect(screen.getByTestId('code-lang-badge')).toHaveTextContent('ts');
  });

  it('container is a <div>, not a button (sibling-not-nested lock1)', () => {
    render(<CollapsibleCodeBlock code={lines(30)} collapsedThreshold={20} />);
    const container = screen.getByTestId('collapsible-code-block');
    expect(container.tagName).toBe('DIV');
    // disclosure + copy are not nested inside one another.
    const disc = screen.getByTestId('code-disclosure-btn');
    expect(disc.querySelector('[data-testid="code-copy-btn"]')).toBeNull();
  });
});
