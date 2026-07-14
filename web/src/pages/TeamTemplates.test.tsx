import { beforeEach, describe, expect, it, vi } from 'vitest';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen, fireEvent, waitFor, within } from '@testing-library/react';
import { MemoryRouter, Routes, Route, useLocation } from 'react-router-dom';
import TeamTemplates from './TeamTemplates';
import { resetTeamsStore } from '@/api/teamsFixtures';

function Loc(): React.ReactElement {
  const l = useLocation();
  return <div data-testid="loc">{l.pathname}</div>;
}

function renderPage() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={['/teams/templates']}>
        <Routes>
          <Route path="/teams/templates" element={<TeamTemplates />} />
          <Route path="/teams/templates/:templateId" element={<Loc />} />
          <Route path="/teams/:teamId" element={<Loc />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('TeamTemplates', () => {
  beforeEach(() => resetTeamsStore());

  it('renders template cards', async () => {
    renderPage();
    expect(await screen.findByTestId('templates-grid')).toBeInTheDocument();
    expect(screen.getByTestId('template-card-tmpl-core')).toBeInTheDocument();
    expect(screen.getByText('Core Feature Squad')).toBeInTheDocument();
  });

  it('opens a template detail on card click', async () => {
    renderPage();
    fireEvent.click(await screen.findByTestId('template-card-tmpl-triage'));
    await waitFor(() => expect(screen.getByTestId('loc')).toHaveTextContent('/teams/templates/tmpl-triage'));
  });

  it('instantiates a template from a card and navigates to the new team', async () => {
    renderPage();
    fireEvent.click(await screen.findByTestId('template-instantiate-tmpl-core'));
    const modal = await screen.findByTestId('instantiate-modal');
    // bump the first role count via the shared builder
    fireEvent.click(within(modal).getByTestId('instantiate-role-0-inc'));
    fireEvent.click(within(modal).getByTestId('instantiate-submit'));
    await waitFor(() => expect(screen.getByTestId('loc').textContent).toMatch(/^\/teams\/team-/));
  });

  it('rejects invalid JSON and imports a valid template', async () => {
    renderPage();
    fireEvent.click(await screen.findByTestId('templates-import'));
    const modal = await screen.findByTestId('import-modal');
    fireEvent.change(within(modal).getByTestId('import-json'), { target: { value: 'not json' } });
    fireEvent.click(within(modal).getByTestId('import-submit'));
    expect(await within(modal).findByTestId('import-error')).toHaveTextContent('Not valid JSON');

    fireEvent.change(within(modal).getByTestId('import-json'), {
      target: { value: JSON.stringify({ name: 'imported-x', roles: [{ role: 'coder', count: 2 }] }) },
    });
    fireEvent.click(within(modal).getByTestId('import-submit'));
    await waitFor(() => expect(screen.getByTestId('loc').textContent).toMatch(/^\/teams\/templates\/tmpl-/));
  });

  it('opens the extract-from-team modal', async () => {
    renderPage();
    fireEvent.click(await screen.findByTestId('templates-extract'));
    expect(await screen.findByTestId('extract-modal')).toBeInTheDocument();
  });

  it('exports a template as JSON', async () => {
    // Spy the two static methods rather than replacing the URL global — the page
    // now fetches templates through MSW, which needs a working `new URL()`.
    const createURL = vi.spyOn(URL, 'createObjectURL').mockReturnValue('blob:x');
    const revoke = vi.spyOn(URL, 'revokeObjectURL').mockImplementation(() => {});
    const clickSpy = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {});
    renderPage();
    fireEvent.click(await screen.findByTestId('template-export-tmpl-core'));
    expect(createURL).toHaveBeenCalled();
    expect(clickSpy).toHaveBeenCalled();
    clickSpy.mockRestore();
    createURL.mockRestore();
    revoke.mockRestore();
  });
});
