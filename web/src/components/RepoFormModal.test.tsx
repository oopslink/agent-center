// T575 (issue-f980c8de) — RepoFormModal: create / edit a workspace repo, with a
// write-only credential field (only sent on edit when the operator types one).
import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { server } from '@/test/mswServer';
import { RepoFormModal } from './RepoFormModal';
import type { WorkspaceRepo } from '@/api/types';

const repo: WorkspaceRepo = {
  id: 'repo-1', organization_id: 'org-1', label: 'agent-center', description: 'mono',
  url: 'git@github.com:o/agent-center.git', provider: 'github', default_branch: 'main',
  has_credential: true, created_by: 'user:o', created_at: 'x', updated_at: 'x', version: 1,
};

function wrap(ui: React.ReactElement) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return render(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>);
}

afterEach(() => cleanup());

describe('RepoFormModal (T575)', () => {
  it('create: POSTs label/url/provider (+credential when typed)', async () => {
    let body: Record<string, unknown> | undefined;
    server.use(
      http.post('/api/code-repos', async ({ request }) => {
        body = (await request.json()) as Record<string, unknown>;
        return HttpResponse.json({ ...repo, id: 'new' }, { status: 201 });
      }),
    );
    const onClose = vi.fn();
    wrap(<RepoFormModal onClose={onClose} />);
    fireEvent.change(screen.getByTestId('repo-form-label'), { target: { value: 'infra' } });
    fireEvent.change(screen.getByTestId('repo-form-url'), { target: { value: 'https://git/infra.git' } });
    fireEvent.change(screen.getByTestId('repo-form-credential'), { target: { value: 'tok' } });
    fireEvent.click(screen.getByTestId('repo-form-save'));
    await waitFor(() => expect(onClose).toHaveBeenCalled());
    expect(body).toMatchObject({ label: 'infra', url: 'https://git/infra.git', provider: 'github', credential: 'tok' });
  });

  it('edit: prefills, and OMITS credential when left blank (keep stored)', async () => {
    let body: Record<string, unknown> | undefined;
    server.use(
      http.patch('/api/code-repos/repo-1', async ({ request }) => {
        body = (await request.json()) as Record<string, unknown>;
        return HttpResponse.json(repo);
      }),
    );
    const onClose = vi.fn();
    wrap(<RepoFormModal repo={repo} onClose={onClose} />);
    expect((screen.getByTestId('repo-form-label') as HTMLInputElement).value).toBe('agent-center');
    // change label only, leave credential blank
    fireEvent.change(screen.getByTestId('repo-form-label'), { target: { value: 'agent-center-2' } });
    fireEvent.click(screen.getByTestId('repo-form-save'));
    await waitFor(() => expect(onClose).toHaveBeenCalled());
    expect(body).toMatchObject({ label: 'agent-center-2' });
    expect(body).not.toHaveProperty('credential');
  });

  it('disables save until label + url present', () => {
    wrap(<RepoFormModal onClose={() => undefined} />);
    expect((screen.getByTestId('repo-form-save') as HTMLButtonElement).disabled).toBe(true);
    fireEvent.change(screen.getByTestId('repo-form-label'), { target: { value: 'x' } });
    expect((screen.getByTestId('repo-form-save') as HTMLButtonElement).disabled).toBe(true);
    fireEvent.change(screen.getByTestId('repo-form-url'), { target: { value: 'u' } });
    expect((screen.getByTestId('repo-form-save') as HTMLButtonElement).disabled).toBe(false);
  });
});
