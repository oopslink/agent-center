import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import { OrgSettingsModal } from './OrgSettingsModal';

// Default MSW handlers expose one org: { id: 'org-test', slug: 'test', name: 'Test Org' }.
function wrap(orgId: string, onClose: () => void = () => {}) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>
        <OrgSettingsModal orgId={orgId} onClose={onClose} />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('OrgSettingsModal (#186-6)', () => {
  afterEach(() => cleanup());

  it('prefills the name + slug from the resolved org', async () => {
    wrap('org-test');
    await waitFor(() =>
      expect((screen.getByTestId('org-settings-name') as HTMLInputElement).value).toBe('Test Org'),
    );
    expect((screen.getByTestId('org-settings-slug') as HTMLInputElement).value).toBe('test');
  });

  it('closes via the cancel/close affordance', async () => {
    let closed = false;
    wrap('org-test', () => { closed = true; });
    await waitFor(() => expect(screen.getByTestId('org-settings-modal')).toBeInTheDocument());
    fireEvent.click(screen.getByTestId('org-settings-cancel'));
    expect(closed).toBe(true);
  });

  it('requires an explicit confirmation step before deleting', async () => {
    wrap('org-test');
    await waitFor(() => expect(screen.getByTestId('org-settings-delete')).toBeInTheDocument());
    expect(screen.queryByTestId('org-settings-delete-confirm')).not.toBeInTheDocument();
    fireEvent.click(screen.getByTestId('org-settings-delete'));
    expect(screen.getByTestId('org-settings-delete-confirm')).toBeInTheDocument();
  });

  it('shows a not-found message when the org id is unknown', async () => {
    wrap('org-missing');
    await waitFor(() => expect(screen.getByTestId('org-settings-missing')).toBeInTheDocument());
  });
});
