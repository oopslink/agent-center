import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { Breadcrumb } from './Breadcrumb';

// No OrgContext provider → OrgLink renders the bare path (orgPath returns `to`
// unchanged when there's no slug), which is enough to assert link vs non-link.
function wrap(items: Parameters<typeof Breadcrumb>[0]['items']) {
  return render(
    <MemoryRouter>
      <Breadcrumb items={items} />
    </MemoryRouter>,
  );
}

describe('Breadcrumb (#238)', () => {
  afterEach(() => cleanup());

  it('renders a linked section, a plain section label, and a bold current leaf', () => {
    wrap([
      { label: 'Projects', to: '/projects' },
      { label: 'Acme', to: '/projects/p-1' },
      { label: 'Issues' },
      { label: 'Login bug' },
    ]);

    // Linked segments are anchors (org-scoped via OrgLink; bare path here).
    const projects = screen.getByTestId('breadcrumb-segment-0');
    expect(projects.tagName).toBe('A');
    expect(projects.getAttribute('href')).toBe('/projects');
    const proj = screen.getByTestId('breadcrumb-segment-1');
    expect(proj.getAttribute('href')).toBe('/projects/p-1');

    // Section label without `to` is plain text (not a link, not current).
    const issues = screen.getByTestId('breadcrumb-segment-2');
    expect(issues.tagName).not.toBe('A');
    expect(issues).not.toHaveAttribute('aria-current');

    // Last segment is the current page: bold, non-clickable, aria-current.
    const leaf = screen.getByTestId('breadcrumb-segment-3');
    expect(leaf.tagName).not.toBe('A');
    expect(leaf).toHaveAttribute('aria-current', 'page');
    expect(leaf).toHaveTextContent('Login bug');
    expect(leaf.className).toContain('font-semibold');
  });

  it('uses "/" separators between segments (one fewer than segments)', () => {
    const { container } = wrap([
      { label: 'Channels', to: '/channels' },
      { label: 'general' },
    ]);
    const seps = Array.from(container.querySelectorAll('[aria-hidden="true"]')).filter(
      (n) => n.textContent === '/',
    );
    expect(seps).toHaveLength(1);
  });

  it('a single current-only item renders no separator', () => {
    const { container } = wrap([{ label: 'general' }]);
    expect(screen.getByTestId('breadcrumb-segment-0')).toHaveAttribute('aria-current', 'page');
    expect(container.textContent).not.toContain('/');
  });
});
