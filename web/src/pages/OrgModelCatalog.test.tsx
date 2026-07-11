import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import { server } from '@/test/mswServer';
import OrgModelCatalog from './OrgModelCatalog';

const entry = {
  id: 'mdl-1',
  model_id: 'claude-opus-4-8',
  display_name: 'Opus 4.8',
  input_cost: 15,
  output_cost: 75,
  context_window: 200000,
  tier: 'hardest tasks',
};

function wrap() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>
        <OrgModelCatalog />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

afterEach(() => cleanup());

describe('OrgModelCatalog', () => {
  it('renders the catalog rows + add button + bulk-import button', async () => {
    server.use(http.get('/api/model-catalog', () => HttpResponse.json({ entries: [entry] })));
    wrap();
    const row = await screen.findByTestId('model-catalog-row');
    expect(row).toHaveAttribute('data-model-id', 'claude-opus-4-8');
    expect(screen.getByText('Opus 4.8')).toBeInTheDocument();
    expect(screen.getByTestId('model-catalog-add-btn')).toBeInTheDocument();
    // The import UI is a standalone top-right button; the modal is closed until clicked.
    expect(screen.getByTestId('model-catalog-import-btn')).toBeInTheDocument();
    expect(screen.queryByTestId('model-catalog-import')).not.toBeInTheDocument();
  });

  it('opens the bulk-import modal from the top-right button', async () => {
    server.use(http.get('/api/model-catalog', () => HttpResponse.json({ entries: [] })));
    wrap();
    await screen.findByTestId('model-catalog-empty');
    expect(screen.queryByTestId('model-catalog-import')).not.toBeInTheDocument();
    fireEvent.click(screen.getByTestId('model-catalog-import-btn'));
    expect(screen.getByTestId('model-catalog-import')).toBeInTheDocument();
    expect(screen.getByTestId('model-catalog-import-json')).toBeInTheDocument();
    // Close returns to the catalog with the modal gone.
    fireEvent.click(screen.getByTestId('model-catalog-import-close'));
    expect(screen.queryByTestId('model-catalog-import')).not.toBeInTheDocument();
  });

  it('rejects malformed JSON client-side without calling the server', async () => {
    let called = false;
    server.use(
      http.get('/api/model-catalog', () => HttpResponse.json({ entries: [] })),
      http.post('/api/model-catalog/import', () => {
        called = true;
        return HttpResponse.json({ ok: true, mode: 'upsert', imported: 0 });
      }),
    );
    wrap();
    await screen.findByTestId('model-catalog-empty');
    fireEvent.click(screen.getByTestId('model-catalog-import-btn'));
    fireEvent.change(screen.getByTestId('model-catalog-import-json'), { target: { value: '[{bad json' } });
    fireEvent.click(screen.getByTestId('model-catalog-import-run'));
    await waitFor(() => expect(screen.getByTestId('model-catalog-import-parse-error')).toBeInTheDocument());
    expect(called).toBe(false);
  });

  it('rejects non-array JSON client-side', async () => {
    server.use(http.get('/api/model-catalog', () => HttpResponse.json({ entries: [] })));
    wrap();
    await screen.findByTestId('model-catalog-empty');
    fireEvent.click(screen.getByTestId('model-catalog-import-btn'));
    fireEvent.change(screen.getByTestId('model-catalog-import-json'), { target: { value: '{"model_id":"x"}' } });
    fireEvent.click(screen.getByTestId('model-catalog-import-run'));
    await waitFor(() => expect(screen.getByTestId('model-catalog-import-parse-error')).toBeInTheDocument());
  });

  it('surfaces a whole-batch import rejection error', async () => {
    server.use(
      http.get('/api/model-catalog', () => HttpResponse.json({ entries: [] })),
      http.post('/api/model-catalog/import', () =>
        HttpResponse.json({ error: 'invalid_import', message: 'duplicate model_id "x" (whole batch rejected)' }, { status: 400 }),
      ),
    );
    wrap();
    await screen.findByTestId('model-catalog-empty');
    fireEvent.click(screen.getByTestId('model-catalog-import-btn'));
    fireEvent.change(screen.getByTestId('model-catalog-import-json'), {
      target: { value: '[{"model_id":"x"},{"model_id":"x"}]' },
    });
    fireEvent.click(screen.getByTestId('model-catalog-import-run'));
    await waitFor(() => expect(screen.getByTestId('model-catalog-import-error')).toBeInTheDocument());
    expect(screen.getByTestId('model-catalog-import-error').textContent).toContain('whole batch rejected');
  });

  it('reports a successful import count', async () => {
    server.use(
      http.get('/api/model-catalog', () => HttpResponse.json({ entries: [] })),
      http.post('/api/model-catalog/import', () => HttpResponse.json({ ok: true, mode: 'replace', imported: 2 })),
    );
    wrap();
    await screen.findByTestId('model-catalog-empty');
    fireEvent.click(screen.getByTestId('model-catalog-import-btn'));
    fireEvent.change(screen.getByTestId('model-catalog-import-json'), { target: { value: '[{"model_id":"a"},{"model_id":"b"}]' } });
    fireEvent.click(screen.getByTestId('model-catalog-import-replace'));
    fireEvent.click(screen.getByTestId('model-catalog-import-run'));
    await waitFor(() => expect(screen.getByTestId('model-catalog-import-ok')).toBeInTheDocument());
    expect(screen.getByTestId('model-catalog-import-ok').textContent).toContain('2');
  });
});
