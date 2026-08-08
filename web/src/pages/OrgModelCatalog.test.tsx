import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter, useLocation } from 'react-router-dom';
import type React from 'react';
import { OrgContext } from '@/OrgContext';
import OrgModelCatalog from './OrgModelCatalog';

function LocationProbe(): React.ReactElement {
  return <div data-testid="loc">{useLocation().pathname}</div>;
}

afterEach(() => cleanup());

describe('OrgModelCatalog compatibility shim', () => {
  it('redirects stale model-catalog links to canonical AI Runtime', async () => {
    render(
      <MemoryRouter initialEntries={['/organizations/test/model-catalog']}>
        <OrgContext.Provider value={{ slug: 'test', orgId: 'org-test', orgName: 'Test Org' }}>
          <OrgModelCatalog />
          <LocationProbe />
        </OrgContext.Provider>
      </MemoryRouter>,
    );
    await waitFor(() => expect(screen.getByTestId('loc')).toHaveTextContent('/organizations/test/ai-runtime'));
  });
});
