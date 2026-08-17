import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import Access from './Access';

function renderPage(path = '/organizations/test/access') {
  window.history.pushState({}, '', path);
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <Access />
    </QueryClientProvider>,
  );
}

afterEach(() => cleanup());

describe('Access page', () => {
  it('renders API-sourced subject and role views with risk and terminal statuses visible', async () => {
    renderPage();
    expect(await screen.findByTestId('page-Access')).toBeInTheDocument();
    expect(await screen.findByText('Permission catalog')).toBeInTheDocument();
    expect(screen.getByTestId('access-subject-view')).toBeInTheDocument();
    expect(screen.getAllByText('High risk').length).toBeGreaterThan(0);
    expect(screen.getAllByText('No access').length).toBeGreaterThan(0);
    expect(screen.getAllByText('Not applicable').length).toBeGreaterThan(0);
    expect(screen.getByText('subject is not a joined organization member')).toBeInTheDocument();
    expect(screen.getByText('file.download does not apply to team resources')).toBeInTheDocument();

    fireEvent.change(screen.getByTestId('access-filter-status'), { target: { value: 'unauthorized' } });
    await waitFor(() => {
      expect(screen.getByText('subject is not a joined organization member')).toBeInTheDocument();
      expect(screen.queryByText('file.download does not apply to team resources')).not.toBeInTheDocument();
    });

    fireEvent.click(screen.getByTestId('access-view-roles'));
    expect(await screen.findByTestId('access-role-view')).toBeInTheDocument();
    expect(screen.getByText('member')).toBeInTheDocument();
  });

  it('previews and applies a four-step batch grant without deriving final permissions in the UI', async () => {
    renderPage();
    expect(await screen.findByTestId('page-Access')).toBeInTheDocument();
    fireEvent.click(screen.getByTestId('access-open-batch'));

    const drawer = await screen.findByTestId('access-batch-drawer');
    fireEvent.click(within(drawer).getByRole('button', { name: /Builder/ }));
    fireEvent.click(within(drawer).getByRole('button', { name: /External Bot/ }));
    fireEvent.click(within(drawer).getByRole('button', { name: /project\.write/ }));
    fireEvent.click(within(drawer).getByRole('button', { name: /team\.memory\.review/ }));
    fireEvent.click(within(drawer).getByRole('button', { name: /Project Alpha/ }));
    fireEvent.change(within(drawer).getByTestId('access-batch-expires'), {
      target: { value: '2026-08-20T12:30' },
    });
    fireEvent.change(within(drawer).getByTestId('access-batch-reason'), {
      target: { value: 'temporary release support' },
    });
    fireEvent.click(within(drawer).getByTestId('access-run-preview'));

    const preview = await within(drawer).findByTestId('access-preview-summary');
    expect(preview).toHaveTextContent('High risk');
    expect(preview).toHaveTextContent('No access');
    expect(preview).toHaveTextContent('N/A');
    expect(preview).toHaveTextContent('Expires');
    expect(preview).not.toHaveTextContent('Expires -');
    const previewRows = within(drawer).getByTestId('access-batch-items');
    expect(previewRows).toHaveTextContent('No access');
    expect(previewRows).toHaveTextContent('Not applicable');

    const continueButton = within(drawer).getByTestId('access-preview-continue');
    expect(continueButton).toBeDisabled();
    fireEvent.click(within(drawer).getByTestId('access-high-risk-ack'));
    expect(continueButton).not.toBeDisabled();
    fireEvent.click(continueButton);

    await within(drawer).findByText(/grantable items/);
    fireEvent.click(within(drawer).getByTestId('access-apply-batch'));
    const result = await within(drawer).findByTestId('access-result');
    expect(result).toHaveTextContent('Partial failure');
    expect(result).toHaveTextContent('no access');
    expect(result).toHaveTextContent('not applicable');
  });

  it('previews, confirms, and reports selected grant revokes', async () => {
    renderPage();
    expect(await screen.findByTestId('page-Access')).toBeInTheDocument();
    const grants = await screen.findByTestId('access-grants');
    fireEvent.click(within(grants).getByRole('checkbox', { name: /Select project\.write for revoke/ }));
    fireEvent.click(within(grants).getByRole('checkbox', { name: /Select org\.member\.role\.manage for revoke/ }));
    fireEvent.click(within(grants).getByTestId('access-revoke-preview'));

    const preview = await within(grants).findByTestId('access-revoke-preview-panel');
    expect(preview).toHaveTextContent('derived permission and must be revoked at its source');
    fireEvent.click(within(preview).getByTestId('access-revoke-confirm'));

    const result = await within(grants).findByTestId('access-result');
    expect(result).toHaveTextContent('Partial failure');
    expect(result).toHaveTextContent('derived permission and must be revoked at its source');
    expect(result).toHaveTextContent('Not applicable');
  });

  it('browses profile versions and creates then publishes explicit profile versions', async () => {
    renderPage();
    expect(await screen.findByTestId('page-Access')).toBeInTheDocument();
    fireEvent.click(screen.getByTestId('access-view-profiles'));

    const view = await screen.findByTestId('access-profiles-view');
    expect(await within(view).findByTestId('access-profile-row-team-curator')).toHaveTextContent('v2');
    fireEvent.click(within(view).getByTestId('access-profile-row-team-curator'));
    expect(await within(view).findByTestId('access-profile-versions')).toHaveTextContent('v1');
    expect(within(view).getByTestId('access-profile-versions')).toHaveTextContent('v2');

    const create = within(view).getByTestId('access-profile-create');
    fireEvent.change(within(create).getByTestId('access-profile-name'), { target: { value: 'Release operator' } });
    fireEvent.change(within(create).getByTestId('access-profile-description'), { target: { value: 'release work' } });
    fireEvent.click(within(create).getByText('org.read'));
    fireEvent.click(within(create).getByText('project.write'));
    fireEvent.click(within(create).getByTestId('access-profile-create-submit'));

    await waitFor(() => expect(screen.getByTestId('access-profile-detail')).toHaveTextContent('Release operator'));
    const detail = screen.getByTestId('access-profile-detail');
    await waitFor(() => expect(within(detail).getByTestId('access-profile-new-version-submit')).toHaveTextContent('Publish v2'));
    expect(within(detail).getByTestId('access-profile-new-version-submit')).not.toBeDisabled();
  });
});
