import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, render, screen } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom';
import OrgModelCatalog from './OrgModelCatalog';

function LocationProbe(): React.ReactElement {
  const location = useLocation();
  return <div data-testid="location">{location.pathname}{location.search}</div>;
}

function wrap(initial = '/organizations/test/model-catalog') {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[initial]}>
        <Routes>
          <Route path="/organizations/:slug/model-catalog" element={<OrgModelCatalog />} />
          <Route path="/organizations/:slug/ai-runtime" element={<LocationProbe />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

afterEach(() => cleanup());

describe('OrgModelCatalog compatibility route', () => {
  it('redirects the retired model catalog UI to AI Runtime Models', async () => {
    wrap();
    expect(await screen.findByTestId('location')).toHaveTextContent('/organizations/test/ai-runtime?tab=models');
  });
});
