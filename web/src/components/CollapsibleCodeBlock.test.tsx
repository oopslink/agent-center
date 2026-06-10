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

  it('renders copy as an icon (no "Copy" text), keeping the aria-label, and swaps to a check on success (v2.8.1)', () => {
    render(<CollapsibleCodeBlock code={lines(3)} />);
    const copy = screen.getByTestId('code-copy-btn');
    // icon-only: an <svg>, no visible "Copy" text — semantic stays on aria-label.
    expect(copy.querySelector('svg')).not.toBeNull();
    expect(copy).not.toHaveTextContent('Copy');
    expect(copy).toHaveAttribute('aria-label', 'Copy code');
    fireEvent.click(copy);
    // still an icon after copy (the success check); SR feedback is the aria-live status.
    expect(copy.querySelector('svg')).not.toBeNull();
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

  // v2.8.1 (@oopslink) — lazy syntax highlighting (highlight.js, code context only).
  it('lazily syntax-highlights a known language in code context (loading → tokens)', async () => {
    render(<CollapsibleCodeBlock code={'const x = 1;'} language="javascript" />);
    const region = screen.getByTestId('code-region');
    // loading state is plain text; the highlighter chunk resolves async → tokens.
    await waitFor(() => expect(region.querySelector('.hljs-keyword')).not.toBeNull());
    expect(region.textContent).toContain('const x = 1;'); // text preserved
  });

  it('does NOT highlight output context (#274 tool_result stays plain)', async () => {
    render(<CollapsibleCodeBlock code={'const x = 1;'} language="javascript" contextLabel="output" />);
    const region = screen.getByTestId('code-region');
    await new Promise((r) => setTimeout(r, 30)); // give any (wrong) async highlight a chance
    expect(region.querySelector('[class^="hljs-"]')).toBeNull();
    expect(region.textContent).toContain('const x = 1;');
  });

  it('does NOT highlight an unknown language (falls back to plain, React-escaped)', async () => {
    render(<CollapsibleCodeBlock code={'foo bar baz'} language="not-a-real-lang" />);
    const region = screen.getByTestId('code-region');
    await new Promise((r) => setTimeout(r, 30));
    expect(region.querySelector('[class^="hljs-"]')).toBeNull();
    expect(region.textContent).toContain('foo bar baz');
  });

  it('SECURITY: escapes injected markup inside a highlighted fence (#187 not regressed)', async () => {
    delete (window as unknown as Record<string, unknown>).__pwn;
    render(
      <CollapsibleCodeBlock code={'<script>window.__pwn=1</script>\nconst y = 2;'} language="javascript" />,
    );
    const region = screen.getByTestId('code-region');
    await waitFor(() => expect(region.querySelector('.hljs-keyword')).not.toBeNull());
    // the <script> is inert text (hljs escapes), never a real element; nothing ran.
    expect(region.querySelector('script')).toBeNull();
    expect((window as unknown as Record<string, unknown>).__pwn).toBeUndefined();
    expect(region.textContent).toContain('window.__pwn=1');
  });
});
