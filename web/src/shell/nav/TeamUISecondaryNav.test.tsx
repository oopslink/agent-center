import { describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import TeamUISecondaryNav from './TeamUISecondaryNav';

function renderAt(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <TeamUISecondaryNav orgBase="/organizations/ooo" />
    </MemoryRouter>,
  );
}

describe('TeamUISecondaryNav', () => {
  it('renders the TEAMS and DIRECTORY groups with links', () => {
    renderAt('/organizations/ooo/teams');
    expect(screen.getByTestId('teamui-nav')).toBeInTheDocument();
    expect(screen.getByTestId('teamui-nav-teams')).toHaveAttribute('href', '/organizations/ooo/teams');
    expect(screen.getByTestId('teamui-nav-templates')).toHaveAttribute('href', '/organizations/ooo/teams/templates');
    expect(screen.getByTestId('teamui-nav-agents')).toHaveAttribute('href', '/organizations/ooo/teams/agents');
    expect(screen.getByTestId('teamui-nav-humans')).toHaveAttribute('href', '/organizations/ooo/teams/humans');
  });

  it('marks All teams active only on the exact /teams path', () => {
    renderAt('/organizations/ooo/teams');
    expect(screen.getByTestId('teamui-nav-teams').className).toContain('bg-brand-hover');
  });

  it('does not keep All teams active on a nested page', () => {
    renderAt('/organizations/ooo/teams/templates');
    expect(screen.getByTestId('teamui-nav-teams').className).not.toContain('bg-brand-hover');
    expect(screen.getByTestId('teamui-nav-templates').className).toContain('bg-brand-hover');
  });
});
