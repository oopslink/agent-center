import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import i18n from '@/i18n';
import { InsightSecondaryNav } from './InsightSecondaryNav';

function renderAt(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <InsightSecondaryNav orgBase="/organizations/acme" />
    </MemoryRouter>,
  );
}

describe('InsightSecondaryNav', () => {
  afterEach(async () => {
    cleanup();
    await i18n.changeLanguage('en');
  });

  it('renders the complete desktop Insight second-level nav', () => {
    renderAt('/organizations/acme/insights/overview');

    expect(screen.getByTestId('insight-nav-overview')).toHaveAttribute('href', '/organizations/acme/insights/overview');
    expect(screen.getByTestId('insight-nav-agents')).toHaveAttribute('href', '/organizations/acme/insights/agents');
    expect(screen.getByTestId('insight-nav-projects')).toHaveAttribute('href', '/organizations/acme/insights/projects');
    expect(screen.getByTestId('insight-nav-collaboration')).toHaveAttribute('href', '/organizations/acme/insights/collaboration');
    expect(screen.getByTestId('insight-nav-executions')).toHaveAttribute('href', '/organizations/acme/insights/executions?window=24h');
    expect(screen.getByRole('link', { name: 'Overview' }).className).toContain('bg-brand-hover');
  });

  it('keeps Task executions active for execution list and detail drilldown routes', () => {
    renderAt('/organizations/acme/insights/executions/exec-1');

    expect(screen.getByRole('link', { name: 'Task executions' }).className).toContain('bg-brand-hover');
    expect(screen.getByRole('link', { name: 'Overview' }).className).not.toContain('bg-brand-hover');
  });

  it('uses localized labels without changing stable routes', async () => {
    await i18n.changeLanguage('zh');
    renderAt('/organizations/acme/insights/projects');

    expect(screen.getByTestId('section-label')).toHaveTextContent('洞察');
    expect(screen.getByRole('link', { name: '概览' })).toHaveAttribute('href', '/organizations/acme/insights/overview');
    expect(screen.getByRole('link', { name: '智能体' })).toHaveAttribute('href', '/organizations/acme/insights/agents');
    expect(screen.getByRole('link', { name: '项目' }).className).toContain('bg-brand-hover');
    expect(screen.getByRole('link', { name: '协作作用力' })).toHaveAttribute('href', '/organizations/acme/insights/collaboration');
    expect(screen.getByRole('link', { name: 'Task executions' })).toHaveAttribute('href', '/organizations/acme/insights/executions?window=24h');
  });
});
