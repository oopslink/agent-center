import type React from 'react';
import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom';
import OrgModelCatalog from './OrgModelCatalog';

function LocationProbe(): React.ReactElement {
  return <div data-testid="location">{useLocation().pathname}</div>;
}

afterEach(() => cleanup());

describe('OrgModelCatalog compatibility', () => {
  it('redirects the retired model catalog page to AI Runtime', async () => {
    render(
      <MemoryRouter initialEntries={['/organizations/test/model-catalog']}>
        <Routes>
          <Route path="/organizations/:slug/model-catalog" element={<OrgModelCatalog />} />
          <Route path="/organizations/:slug/ai-runtime" element={<LocationProbe />} />
        </Routes>
      </MemoryRouter>,
    );
    await waitFor(() => expect(screen.getByTestId('location')).toHaveTextContent('/organizations/test/ai-runtime'));
  });
});
