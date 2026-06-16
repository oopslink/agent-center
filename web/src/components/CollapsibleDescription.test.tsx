import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, render, screen, fireEvent } from '@testing-library/react';
import { CollapsibleDescription } from './CollapsibleDescription';

// T179 — Task/Issue detail description, collapsible so a long description never
// pushes the conversation off-screen on mobile. Collapse is decided by raw
// content size (line count OR char length), so it is deterministic in jsdom.
const lines = (n: number) => Array.from({ length: n }, (_, i) => `line ${i + 1}`).join('\n');

describe('CollapsibleDescription (T179)', () => {
  afterEach(() => cleanup());

  it('renders full markdown with NO toggle when short (under thresholds)', () => {
    render(<CollapsibleDescription content={'# Heading\n\n- one\n- two'} testId="task-description" ariaLabel="Task description" />);
    const region = screen.getByTestId('task-description');
    // not collapsible → keeps the height-cap + keyboard-scrollable region (prior behavior).
    expect(region).toHaveClass('max-h-64', 'overflow-y-auto');
    expect(region).toHaveAttribute('tabindex', '0');
    // markdown is actually rendered, not raw text.
    expect(region.querySelector('h1')).toBeInTheDocument();
    expect(region.querySelectorAll('li')).toHaveLength(2);
    // no toggle for short content.
    expect(screen.queryByTestId('task-description-toggle')).toBeNull();
  });

  it('collapses a many-line description: clipped region + Show more, aria wired', () => {
    render(<CollapsibleDescription content={lines(20)} testId="task-description" ariaLabel="Task description" />);
    const region = screen.getByTestId('task-description');
    // collapsed → clipped (no internal scroll), not keyboard-focusable for scroll.
    expect(region).toHaveClass('max-h-20', 'overflow-hidden');
    expect(region).toHaveAttribute('tabindex', '-1');
    const toggle = screen.getByTestId('task-description-toggle');
    expect(toggle).toHaveTextContent('Show more');
    expect(toggle).toHaveAttribute('aria-expanded', 'false');
    // aria-controls points to the (useId-generated, non-empty) region id.
    const regionId = region.getAttribute('id');
    expect(regionId).toBeTruthy();
    expect(toggle).toHaveAttribute('aria-controls', regionId as string);
    // full content is in the DOM even while collapsed (CSS-clipped, not sliced) —
    // screen readers can still reach it.
    expect(region).toHaveTextContent('line 20');
  });

  it('expands on toggle: full height-capped scroll region + Show less', () => {
    render(<CollapsibleDescription content={lines(20)} testId="issue-description" ariaLabel="Issue description" />);
    fireEvent.click(screen.getByTestId('issue-description-toggle'));
    const region = screen.getByTestId('issue-description');
    expect(region).toHaveClass('max-h-64', 'overflow-y-auto');
    expect(region).toHaveAttribute('tabindex', '0');
    const toggle = screen.getByTestId('issue-description-toggle');
    expect(toggle).toHaveTextContent('Show less');
    expect(toggle).toHaveAttribute('aria-expanded', 'true');
  });

  it('collapses long single-line prose by char length (not just line count)', () => {
    render(<CollapsibleDescription content={'x'.repeat(500)} testId="task-description" ariaLabel="Task description" />);
    expect(screen.getByTestId('task-description-toggle')).toHaveTextContent('Show more');
  });
});
