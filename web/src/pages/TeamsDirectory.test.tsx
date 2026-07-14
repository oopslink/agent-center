import { beforeEach, describe, expect, it } from 'vitest';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import TeamsDirectoryAgents from './TeamsDirectoryAgents';
import TeamsDirectoryHumans from './TeamsDirectoryHumans';
import { resetTeamsStore } from '@/api/teamsFixtures';

function renderPage(el: React.ReactElement) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>{el}</MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('TeamsDirectoryAgents', () => {
  beforeEach(() => resetTeamsStore());

  it('renders the agents table with a TEAMS column', async () => {
    renderPage(<TeamsDirectoryAgents />);
    expect(await screen.findByTestId('agents-table')).toBeInTheDocument();
    expect(screen.getByTestId('agent-row-agent-center-pd')).toBeInTheDocument();
    // an unassigned agent shows 未编入
    expect(screen.getAllByText('未编入').length).toBeGreaterThan(0);
  });

  it('filters by status', async () => {
    renderPage(<TeamsDirectoryAgents />);
    await screen.findByTestId('agents-table');
    fireEvent.click(screen.getByTestId('agents-filter-working'));
    await waitFor(() => expect(screen.queryByTestId('agent-row-agent-center-dev1')).not.toBeInTheDocument());
    expect(screen.getByTestId('agent-row-agent-center-pd')).toBeInTheDocument();
  });

  it('filters by team', async () => {
    renderPage(<TeamsDirectoryAgents />);
    await screen.findByTestId('agents-table');
    fireEvent.change(screen.getByTestId('agents-team-filter'), { target: { value: 'growth-experiments' } });
    await waitFor(() => expect(screen.queryByTestId('agent-row-agent-center-pd')).not.toBeInTheDocument());
    expect(screen.getByTestId('agent-row-agent-center-tester3')).toBeInTheDocument();
  });

  it('searches by name', async () => {
    renderPage(<TeamsDirectoryAgents />);
    await screen.findByTestId('agents-table');
    fireEvent.change(screen.getByTestId('agents-search'), { target: { value: 'UDE' } });
    await waitFor(() => expect(screen.queryByTestId('agent-row-agent-center-pd')).not.toBeInTheDocument());
    expect(screen.getByTestId('agent-row-UDE')).toBeInTheDocument();
  });

  it('shows an empty state when nothing matches', async () => {
    renderPage(<TeamsDirectoryAgents />);
    await screen.findByTestId('agents-table');
    fireEvent.change(screen.getByTestId('agents-search'), { target: { value: 'zzzz-none' } });
    expect(await screen.findByTestId('agents-empty')).toBeInTheDocument();
  });
});

describe('TeamsDirectoryHumans', () => {
  beforeEach(() => resetTeamsStore());

  it('renders humans with a multi-team TEAMS column', async () => {
    renderPage(<TeamsDirectoryHumans />);
    expect(await screen.findByTestId('humans-table')).toBeInTheDocument();
    expect(screen.getByTestId('human-row-oopslink')).toBeInTheDocument();
    // carol is invited with no teams → the TEAMS cell shows 未编入
    expect(screen.getAllByText('未编入').length).toBeGreaterThan(0);
  });

  it('filters to joined only', async () => {
    renderPage(<TeamsDirectoryHumans />);
    await screen.findByTestId('humans-table');
    fireEvent.click(screen.getByTestId('humans-filter-joined'));
    await waitFor(() => expect(screen.queryByTestId('human-row-carol')).not.toBeInTheDocument());
  });

  it('filters by team', async () => {
    renderPage(<TeamsDirectoryHumans />);
    await screen.findByTestId('humans-table');
    fireEvent.change(screen.getByTestId('humans-team-filter'), { target: { value: 'docs-and-dx' } });
    await waitFor(() => expect(screen.queryByTestId('human-row-alice')).not.toBeInTheDocument());
    expect(screen.getByTestId('human-row-bob')).toBeInTheDocument();
  });
});
