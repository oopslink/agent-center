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
  fireEvent.change(screen.getByLabelText('Passcode'), { target: { value: 'Passw0rd!' } });
  fireEvent.change(screen.getByLabelText('Confirm passcode'), { target: { value: 'Passw0rd!' } });
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

  it('accepts a passcode longer than 6 characters (maxLength is 128, not 6)', () => {
    wrap();
    const passcode = screen.getByLabelText('Passcode') as HTMLInputElement;
    expect(passcode.maxLength).toBe(128);
    // A >6-char passcode like "Passw0rd!" must be retained, not truncated to 6.
    fireEvent.change(passcode, { target: { value: 'Passw0rd!' } });
    expect(passcode.value).toBe('Passw0rd!');
  });

  it('renders the passcode rule hint', () => {
    wrap();
    expect(
      screen.getByText('At least 6 characters, including a letter, a digit, and a symbol.'),
    ).toBeInTheDocument();
  });

  // #290: client validation matches backend ValidatePasscodePlain
  // (>=6 + letter + digit + symbol + <=128), with distinct messages.
  describe('passcode strength (#290)', () => {
    function submitWith(passcode: string) {
      wrap();
      fireEvent.change(screen.getByLabelText('Display name'), { target: { value: 'Alice' } });
      fireEvent.change(screen.getByLabelText('Email'), { target: { value: 'alice@example.com' } });
      fireEvent.change(screen.getByLabelText('Passcode'), { target: { value: passcode } });
      fireEvent.change(screen.getByLabelText('Confirm passcode'), { target: { value: passcode } });
      fireEvent.change(screen.getByLabelText('Organization name'), { target: { value: 'Acme' } });
      fireEvent.submit(screen.getByRole('button', { name: 'Create account' }).closest('form')!);
    }

    it('rejects a too-short passcode', () => {
      submitWith('abc!');
      expect(screen.getByText('Passcode must be at least 6 characters')).toBeInTheDocument();
    });

    it('rejects a passcode longer than 128 characters', () => {
      submitWith('a'.repeat(129));
      expect(screen.getByText('Passcode must be at most 128 characters')).toBeInTheDocument();
    });

    it('rejects a passcode with no letter', () => {
      submitWith('123456@@');
      expect(screen.getByText('Passcode must contain a letter')).toBeInTheDocument();
    });

    it('rejects a passcode with no digit', () => {
      submitWith('abcdef!');
      expect(screen.getByText('Passcode must contain a digit')).toBeInTheDocument();
    });

    it('rejects a passcode with no symbol', () => {
      submitWith('Passw0rd');
      expect(screen.getByText('Passcode must contain a symbol')).toBeInTheDocument();
    });

    it('accepts a compliant passcode', () => {
      submitWith('Passw0rd!');
      expect(screen.queryByText(/^Passcode must/)).not.toBeInTheDocument();
    });

    // Run-real render seam: submit is disabled while invalid, so handleSubmit
    // never fires. The distinct error MUST render inline on blur/touched.
    it('renders the distinct strength error inline on blur of an invalid passcode', () => {
      wrap();
      const passcode = screen.getByLabelText('Passcode');
      fireEvent.change(passcode, { target: { value: 'Passw0rd' } }); // no symbol
      // Untouched (not yet blurred): no error shown.
      expect(screen.queryByText('Passcode must contain a symbol')).not.toBeInTheDocument();
      fireEvent.blur(passcode);
      expect(screen.getByText('Passcode must contain a symbol')).toBeInTheDocument();
    });

    it('updates the inline error as the user types after first blur', () => {
      wrap();
      const passcode = screen.getByLabelText('Passcode');
      fireEvent.change(passcode, { target: { value: 'abc' } }); // too short
      fireEvent.blur(passcode);
      expect(screen.getByText('Passcode must be at least 6 characters')).toBeInTheDocument();
      // Once touched, change re-validates and the distinct message updates.
      fireEvent.change(passcode, { target: { value: 'abcdef' } }); // long enough, no digit
      expect(screen.getByText('Passcode must contain a digit')).toBeInTheDocument();
    });

    it('shows no inline error for a valid passcode after blur', () => {
      wrap();
      const passcode = screen.getByLabelText('Passcode');
      fireEvent.change(passcode, { target: { value: 'Passw0rd!' } });
      fireEvent.blur(passcode);
      expect(screen.queryByText(/^Passcode must/)).not.toBeInTheDocument();
    });

    it('shows no inline error on an untouched passcode field at first load', () => {
      wrap();
      expect(screen.queryByText(/^Passcode must/)).not.toBeInTheDocument();
    });
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
