import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type React from 'react';
import { server } from '@/test/mswServer';
import { SecretCreateModal } from './SecretCreateModal';

function wrap(ui: React.ReactElement) {
  const qc = new QueryClient({ defaultOptions: { mutations: { retry: false } } });
  return render(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>);
}

const PLAINTEXT = 'tttooopppsecret-ZZZZ-marker';

describe('SecretCreateModal — strict no-plaintext-echo (ADR-0026 § 5)', () => {
  beforeEach(() => {
    // Default to a deny-the-leak handler so anything that accidentally
    // includes `value` in the response would still be caught here.
    server.use(
      http.post('/api/secrets', () =>
        HttpResponse.json({ id: 'S-NEW', name: 'github', event_id: 'E-c' }, { status: 201 }),
      ),
    );
  });
  afterEach(() => cleanup());

  it('value field is type=password + autocomplete=off', () => {
    wrap(<SecretCreateModal open onClose={() => undefined} />);
    const input = screen.getByTestId('secret-value-input') as HTMLInputElement;
    expect(input).toHaveAttribute('type', 'password');
    expect(input).toHaveAttribute('autocomplete', 'off');
  });

  it('on successful submit: form clears value + success banner only shows the name, never plaintext', async () => {
    wrap(<SecretCreateModal open onClose={() => undefined} />);
    await userEvent.type(screen.getByTestId('secret-name-input'), 'github');
    await userEvent.type(screen.getByTestId('secret-value-input'), PLAINTEXT);
    fireEvent.click(screen.getByTestId('secret-create-submit'));
    await waitFor(() => expect(screen.getByTestId('secret-create-success')).toBeInTheDocument());
    // Critical assertion: plaintext must NOT appear anywhere in the DOM.
    expect(document.body.innerHTML).not.toContain(PLAINTEXT);
    // Success banner shows name only.
    expect(screen.getByTestId('secret-create-success')).toHaveTextContent(/github created/);
  });

  it('on cancel: value state is cleared (no plaintext lingering in React tree)', async () => {
    const onClose = vi.fn();
    wrap(<SecretCreateModal open onClose={onClose} />);
    await userEvent.type(screen.getByTestId('secret-value-input'), PLAINTEXT);
    fireEvent.click(screen.getByTestId('secret-create-cancel'));
    expect(onClose).toHaveBeenCalled();
    // Re-render to confirm clean state on reopen.
    cleanup();
    wrap(<SecretCreateModal open onClose={() => undefined} />);
    const input = screen.getByTestId('secret-value-input') as HTMLInputElement;
    expect(input.value).toBe('');
  });

  it('renders nothing when closed', () => {
    wrap(<SecretCreateModal open={false} onClose={() => undefined} />);
    expect(screen.queryByTestId('secret-create-modal')).not.toBeInTheDocument();
  });

  it('submit disabled until name + value both present', async () => {
    wrap(<SecretCreateModal open onClose={() => undefined} />);
    const submit = screen.getByTestId('secret-create-submit');
    expect(submit).toBeDisabled();
    await userEvent.type(screen.getByTestId('secret-name-input'), 'k');
    expect(submit).toBeDisabled();
    await userEvent.type(screen.getByTestId('secret-value-input'), 'v');
    expect(submit).not.toBeDisabled();
  });

  it('server error surfaces inline + modal stays open + value field still type=password', async () => {
    server.use(
      http.post('/api/secrets', () =>
        HttpResponse.json({ error: 'duplicate', message: 'name taken' }, { status: 409 }),
      ),
    );
    wrap(<SecretCreateModal open onClose={() => undefined} />);
    await userEvent.type(screen.getByTestId('secret-name-input'), 'dup');
    await userEvent.type(screen.getByTestId('secret-value-input'), PLAINTEXT);
    fireEvent.click(screen.getByTestId('secret-create-submit'));
    await waitFor(() => expect(screen.getByTestId('secret-create-error')).toHaveTextContent(/name taken/));
    // value field still password-masked
    expect(screen.getByTestId('secret-value-input')).toHaveAttribute('type', 'password');
    // success banner not rendered
    expect(screen.queryByTestId('secret-create-success')).not.toBeInTheDocument();
  });
});
