import type React from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { server } from '@/test/mswServer';
import { OrgWorkItemCreateModal } from './OrgWorkItemCreateModal';

function wrap(ui: React.ReactElement) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>);
}

describe('OrgWorkItemCreateModal', () => {
  afterEach(() => cleanup());

  it('renders the project-picker phase with dialog a11y', () => {
    server.use(http.get('/api/projects', () => HttpResponse.json({ projects: [] })));
    wrap(<OrgWorkItemCreateModal kind="issue" onClose={() => undefined} />);
    const modal = screen.getByTestId('org-create-modal');
    expect(modal).toHaveAttribute('role', 'dialog');
    expect(modal).toHaveAttribute('aria-modal', 'true');
    expect(screen.getByTestId('org-create-project-select')).toBeInTheDocument();
  });

  it('closes on Escape in the picker phase (WAI-ARIA dialog — useModalA11y)', () => {
    server.use(http.get('/api/projects', () => HttpResponse.json({ projects: [] })));
    const onClose = vi.fn();
    wrap(<OrgWorkItemCreateModal kind="issue" onClose={onClose} />);
    fireEvent.keyDown(document, { key: 'Escape' });
    expect(onClose).toHaveBeenCalledTimes(1);
  });
});
