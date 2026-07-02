import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { http, HttpResponse, delay } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import { server } from '@/test/mswServer';
import OrgTemplates from './OrgTemplates';

// The list DTO (GET /templates) intentionally omits `content`; only the detail
// endpoint (GET /templates/{id}) returns it. These tests pin the fix for the
// bug where the Edit modal seeded its textarea from the list object and showed
// an empty Content field — risking a save that wiped the stored content.
const listItem = {
  id: 't1',
  name: '大开发计划',
  description: '大的开发计划,走全真验收流程',
  builtin: false,
  created_at: '2026-07-01T00:00:00Z',
};
const detail = { ...listItem, content: 'REAL TEMPLATE BODY', updated_at: '2026-07-01T00:00:00Z', version: 1 };

function wrap() {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>
        <OrgTemplates />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

afterEach(() => cleanup());

describe('OrgTemplates edit — content hydration', () => {
  it('populates the Content textarea from the detail endpoint on Edit', async () => {
    server.use(
      http.get('/api/templates', () => HttpResponse.json({ templates: [listItem] })),
      http.get('/api/templates/:id', () => HttpResponse.json(detail)),
    );
    wrap();
    fireEvent.click(await screen.findByTestId('template-card-edit'));
    // Content is fetched from the detail endpoint, not the (content-less) list item.
    const textarea = await screen.findByTestId<HTMLTextAreaElement>('template-form-content');
    await waitFor(() => expect(textarea.value).toBe('REAL TEMPLATE BODY'));
  });

  it('blocks Save while the content is still loading (prevents wiping stored content)', async () => {
    server.use(
      http.get('/api/templates', () => HttpResponse.json({ templates: [listItem] })),
      http.get('/api/templates/:id', async () => {
        await delay(50);
        return HttpResponse.json(detail);
      }),
    );
    wrap();
    fireEvent.click(await screen.findByTestId('template-card-edit'));
    // Before the detail resolves, the submit button is disabled.
    expect(screen.getByTestId('template-form-submit')).toBeDisabled();
    // Once loaded, it becomes enabled and the body is present.
    const textarea = await screen.findByTestId<HTMLTextAreaElement>('template-form-content');
    await waitFor(() => expect(textarea.value).toBe('REAL TEMPLATE BODY'));
    expect(screen.getByTestId('template-form-submit')).not.toBeDisabled();
  });

  it('View panel renders content from the detail endpoint', async () => {
    server.use(
      http.get('/api/templates', () => HttpResponse.json({ templates: [listItem] })),
      http.get('/api/templates/:id', () => HttpResponse.json(detail)),
    );
    wrap();
    fireEvent.click(await screen.findByTestId('template-card-view'));
    const panel = await screen.findByTestId('template-content-panel');
    await waitFor(() => expect(panel).toHaveTextContent('REAL TEMPLATE BODY'));
  });
});
