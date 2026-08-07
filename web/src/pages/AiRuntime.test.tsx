import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import { server } from '@/test/mswServer';
import { OrgContext } from '@/OrgContext';
import AiRuntime from './AiRuntime';

function renderPage(path = '/organizations/test/organization-settings/ai-runtime') {
  window.history.pushState({}, '', path);
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <OrgContext.Provider value={{ slug: 'test', orgId: 'org-test', orgName: 'Test Org' }}>
        <MemoryRouter initialEntries={[path]}>
          <AiRuntime />
        </MemoryRouter>
      </OrgContext.Provider>
    </QueryClientProvider>,
  );
}

afterEach(() => cleanup());

describe('AiRuntime page', () => {
  it('renders the organization runtime catalog and admin controls for owners', async () => {
    renderPage();
    expect(await screen.findByTestId('page-AiRuntime')).toBeInTheDocument();
    await waitFor(() =>
      expect(screen.getByTestId('ai-runtime-permission')).toHaveAttribute('data-can-manage', 'true'),
    );
    expect(screen.getByTestId('ai-runtime-export-yaml')).toHaveAttribute(
      'href',
      '/api/orgs/test/ai-runtime/export?format=yaml',
    );
    expect(await screen.findByTestId('ai-runtime-default-profile')).toHaveTextContent('Default coding');
    expect(screen.getAllByTestId('ai-runtime-profile-row')).toHaveLength(2);
    expect(screen.getAllByTestId('ai-runtime-set-default')).toHaveLength(2);

    fireEvent.click(screen.getByTestId('ai-runtime-tab-models'));
    expect(await screen.findByTestId('ai-runtime-model-row')).toHaveTextContent('GPT-5');
    fireEvent.click(screen.getByTestId('ai-runtime-tab-clis'));
    expect(await screen.findByTestId('ai-runtime-cli-row')).toHaveTextContent('Codex CLI');
  });

  it('keeps organization members read-only while leaving the page visible', async () => {
    server.use(
      http.get('/api/orgs', () =>
        HttpResponse.json([
          { id: 'org-test', slug: 'test', name: 'Test Org', role: 'member', created_at: '2026-01-01T00:00:00Z' },
        ]),
      ),
    );
    renderPage();
    expect(await screen.findByTestId('page-AiRuntime')).toBeInTheDocument();
    expect(screen.getByTestId('ai-runtime-permission')).toHaveAttribute('data-can-manage', 'false');
    expect(screen.getByTestId('ai-runtime-export-yaml')).toBeInTheDocument();
    expect(screen.queryByTestId('ai-runtime-set-default')).not.toBeInTheDocument();
    const table = await screen.findByTestId('ai-runtime-catalog');
    expect(within(table).getByText('Default coding')).toBeInTheDocument();
  });

  it('sets a new default profile through the org-scoped endpoint', async () => {
    let called = false;
    server.use(
      http.put('/api/orgs/:slug/ai-runtime/default-profile', async ({ params, request }) => {
        const body = (await request.json()) as { expected_revision?: number; profile_id?: string };
        called = params.slug === 'test' && body.expected_revision === 3 && body.profile_id === 'runtime-profile-review';
        return HttpResponse.json({ revision: 4, default_runtime_profile_id: body.profile_id });
      }),
    );
    renderPage();
    const buttons = await screen.findAllByTestId('ai-runtime-set-default');
    fireEvent.click(buttons[1]);
    await waitFor(() => expect(called).toBe(true));
  });
});
