import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { EntityRef } from './EntityRef';

function wrap(ui: React.ReactElement) {
  return render(<MemoryRouter>{ui}</MemoryRouter>);
}

describe('EntityRef (#192)', () => {
  afterEach(() => cleanup());

  it('renders the display name with the raw id available on hover (title)', () => {
    wrap(<EntityRef id="agent-bld1" name="builder" />);
    const el = screen.getByTestId('entity-ref');
    expect(el).toHaveTextContent('builder');
    // raw id is NOT visible text — only on hover.
    expect(el).not.toHaveTextContent('agent-bld1');
    expect(el).toHaveAttribute('title', 'agent-bld1');
    expect(el).toHaveAttribute('data-entity-id', 'agent-bld1');
  });

  it('renders a "(deleted)" placeholder (never the raw id) when the name is unresolved', () => {
    wrap(<EntityRef id="agent-gone" name={undefined} />);
    const el = screen.getByTestId('entity-ref');
    expect(el).toHaveTextContent('(deleted)');
    expect(el).not.toHaveTextContent('agent-gone');
    expect(el).toHaveAttribute('data-deleted', 'true');
  });

  it('treats an empty name as unresolved → "(deleted)"', () => {
    wrap(<EntityRef id="x" name="" />);
    expect(screen.getByTestId('entity-ref')).toHaveTextContent('(deleted)');
  });

  it('shows the fallback (not "(deleted)") for a present entity with no name', () => {
    wrap(<EntityRef id="user-54ac96f4" name={undefined} fallback="user-54ac96f4" />);
    const el = screen.getByTestId('entity-ref');
    expect(el).toHaveTextContent('user-54ac96f4');
    expect(el).not.toHaveAttribute('data-deleted');
  });

  it('renders a link to the given org-scoped path when resolved', () => {
    wrap(<EntityRef id="agent-bld1" name="builder" to="/agents/agent-bld1" />);
    const el = screen.getByTestId('entity-ref');
    expect(el.tagName).toBe('A');
    expect(el).toHaveAttribute('href', '/agents/agent-bld1');
  });

  it('does not link a deleted entity even when a path is given', () => {
    wrap(<EntityRef id="agent-gone" name={undefined} to="/agents/agent-gone" />);
    const el = screen.getByTestId('entity-ref');
    expect(el.tagName).not.toBe('A');
    expect(el).toHaveTextContent('(deleted)');
  });
});
