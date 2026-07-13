import { beforeEach, describe, expect, it, vi } from 'vitest';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen, fireEvent, waitFor, within } from '@testing-library/react';
import { MemoryRouter, Routes, Route, useLocation } from 'react-router-dom';
import TeamTemplateDetail from './TeamTemplateDetail';
import { resetTeamsStore } from '@/api/teamsFixtures';

function Loc(): React.ReactElement {
  const l = useLocation();
  return <div data-testid="loc">{l.pathname}</div>;
}

function renderAt(id: string) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[`/teams/templates/${id}`]}>
        <Routes>
          <Route path="/teams/templates/:templateId" element={<TeamTemplateDetail />} />
          <Route path="/teams/templates" element={<Loc />} />
          <Route path="/teams/:teamId" element={<Loc />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('TeamTemplateDetail', () => {
  beforeEach(() => resetTeamsStore());

  it('renders the overview with provenance', async () => {
    renderAt('tmpl-core');
    expect(await screen.findByRole('heading', { name: 'Core Feature Squad' })).toBeInTheDocument();
    expect(screen.getByText('模版信息')).toBeInTheDocument();
    expect(screen.getByText('provenance')).toBeInTheDocument();
  });

  it('errors on an unknown template', async () => {
    renderAt('tmpl-nope');
    expect(await screen.findByTestId('template-detail-error')).toHaveTextContent('template_not_found');
  });

  it('shows the seed memory pane', async () => {
    renderAt('tmpl-core');
    fireEvent.click(await screen.findByTestId('tab-sm'));
    expect(await screen.findByTestId('memory-pane')).toBeInTheDocument();
  });

  it('shows the curation audit list', async () => {
    renderAt('tmpl-core');
    fireEvent.click(await screen.findByTestId('tab-cu'));
    expect(await screen.findByTestId('curation-list')).toBeInTheDocument();
    expect(await screen.findByTestId('curation-0')).toBeInTheDocument();
  });

  it('lists instances and an empty state', async () => {
    renderAt('tmpl-core');
    fireEvent.click(await screen.findByTestId('tab-in'));
    expect(await screen.findByTestId('instance-team-7c19b0')).toBeInTheDocument();
    // navigate to an instance team
    fireEvent.click(within(screen.getByTestId('instance-team-7c19b0')).getByText('打开'));
    await waitFor(() => expect(screen.getByTestId('loc')).toHaveTextContent('/teams/team-7c19b0'));
  });

  it('shows an empty instances state for a fresh template', async () => {
    renderAt('tmpl-triage');
    fireEvent.click(await screen.findByTestId('tab-in'));
    expect(await screen.findByTestId('instances-empty')).toBeInTheDocument();
  });

  it('instantiates from the header', async () => {
    renderAt('tmpl-core');
    fireEvent.click(await screen.findByTestId('template-instantiate'));
    const modal = await screen.findByTestId('instantiate-modal');
    fireEvent.click(within(modal).getByTestId('instantiate-submit'));
    await waitFor(() => expect(screen.getByTestId('loc').textContent).toMatch(/^\/teams\/team-/));
  });

  it('exports the template JSON from the header', async () => {
    const createURL = vi.fn(() => 'blob:x');
    vi.stubGlobal('URL', { ...URL, createObjectURL: createURL, revokeObjectURL: vi.fn() });
    const clickSpy = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {});
    renderAt('tmpl-core');
    fireEvent.click(await screen.findByTestId('template-export'));
    expect(createURL).toHaveBeenCalled();
    clickSpy.mockRestore();
    vi.unstubAllGlobals();
  });
});
