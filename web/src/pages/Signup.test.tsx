import { afterEach, describe, expect, it } from 'vitest';
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { MemoryRouter } from 'react-router-dom';
import { server } from '@/test/mswServer';
import Signup from './Signup';

function wrap() {
  return render(
    <MemoryRouter>
      <Signup />
    </MemoryRouter>,
  );
}

function fillExceptEmail() {
  fireEvent.change(screen.getByLabelText('Display name'), { target: { value: 'Alice' } });
  fireEvent.change(screen.getByLabelText('6-digit passcode'), { target: { value: '123456' } });
  fireEvent.change(screen.getByLabelText('Confirm passcode'), { target: { value: '123456' } });
  fireEvent.change(screen.getByLabelText('Organization name'), { target: { value: 'Acme' } });
}

describe('Signup email (#193)', () => {
  afterEach(() => cleanup());

  it('keeps submit disabled until a valid email is entered', () => {
    wrap();
    fillExceptEmail();
    const btn = screen.getByRole('button', { name: 'Create account' }) as HTMLButtonElement;
    expect(btn.disabled).toBe(true);
    fireEvent.change(screen.getByLabelText('Email'), { target: { value: 'alice@example.com' } });
    expect(btn.disabled).toBe(false);
  });

  it('includes email in the signup payload', async () => {
    let posted: Record<string, unknown> | undefined;
    server.use(
      http.post('/api/auth/signup', async ({ request }) => {
        posted = (await request.json()) as Record<string, unknown>;
        return HttpResponse.json({ identity_id: 'user-x', organization_id: 'o', display_name: 'Alice' });
      }),
    );
    wrap();
    fillExceptEmail();
    fireEvent.change(screen.getByLabelText('Email'), { target: { value: 'alice@example.com' } });
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Create account' }));
    });
    await waitFor(() => expect(posted).toMatchObject({ email: 'alice@example.com', display_name: 'Alice' }));
  });

  it('surfaces a duplicate-email conflict on the email field', async () => {
    server.use(
      http.post('/api/auth/signup', () =>
        HttpResponse.json({ error: 'email_taken', message: 'email in use' }, { status: 409 }),
      ),
    );
    wrap();
    fillExceptEmail();
    fireEvent.change(screen.getByLabelText('Email'), { target: { value: 'dup@example.com' } });
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Create account' }));
    });
    await waitFor(() => expect(screen.getByText('That email is already in use')).toBeInTheDocument());
  });
});
