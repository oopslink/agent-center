import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import AccountPanel from './AccountPanel';

function wrap() {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>
        <AccountPanel />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

afterEach(cleanup);

function submitWith(newPasscode: string) {
  fireEvent.change(screen.getByLabelText('Current password'), { target: { value: 'Curr3nt!' } });
  fireEvent.change(screen.getByLabelText('New password'), { target: { value: newPasscode } });
  fireEvent.change(screen.getByLabelText('Confirm new password'), { target: { value: newPasscode } });
  fireEvent.submit(screen.getByRole('button', { name: 'Change password' }).closest('form')!);
}

describe('AccountPanel passcode strength (#290)', () => {
  it('renders the passcode rule hint', () => {
    wrap();
    expect(
      screen.getByText('At least 6 characters, including a letter, a digit, and a symbol.'),
    ).toBeInTheDocument();
  });

  it('rejects a too-short passcode', () => {
    wrap();
    submitWith('abc!');
    expect(screen.getByText('Passcode must be at least 6 characters')).toBeInTheDocument();
  });

  it('rejects a passcode longer than 128 characters', () => {
    wrap();
    submitWith('a'.repeat(129));
    expect(screen.getByText('Passcode must be at most 128 characters')).toBeInTheDocument();
  });

  it('rejects a passcode with no letter', () => {
    wrap();
    submitWith('123456@@');
    expect(screen.getByText('Passcode must contain a letter')).toBeInTheDocument();
  });

  it('rejects a passcode with no digit', () => {
    wrap();
    submitWith('abcdef!');
    expect(screen.getByText('Passcode must contain a digit')).toBeInTheDocument();
  });

  it('rejects a passcode with no symbol', () => {
    wrap();
    submitWith('Passw0rd');
    expect(screen.getByText('Passcode must contain a symbol')).toBeInTheDocument();
  });

  it('accepts a compliant passcode (no strength error shown)', () => {
    wrap();
    submitWith('Passw0rd!');
    expect(screen.queryByText(/^Passcode must/)).not.toBeInTheDocument();
  });

  // #290 run-real render seam: the distinct strength message must render
  // inline under the new-password field on blur/touched, not only on submit.
  it('renders the distinct strength error inline on blur of the new password', () => {
    wrap();
    const newPw = screen.getByLabelText('New password');
    fireEvent.change(newPw, { target: { value: 'Passw0rd' } }); // no symbol
    // Untouched (not blurred yet): no error.
    expect(screen.queryByText('Passcode must contain a symbol')).not.toBeInTheDocument();
    fireEvent.blur(newPw);
    expect(screen.getByText('Passcode must contain a symbol')).toBeInTheDocument();
  });

  it('updates the inline new-password error as the user types after first blur', () => {
    wrap();
    const newPw = screen.getByLabelText('New password');
    fireEvent.change(newPw, { target: { value: 'abc' } }); // too short
    fireEvent.blur(newPw);
    expect(screen.getByText('Passcode must be at least 6 characters')).toBeInTheDocument();
    fireEvent.change(newPw, { target: { value: 'abcdef' } }); // no digit
    expect(screen.getByText('Passcode must contain a digit')).toBeInTheDocument();
  });

  it('shows no inline error for a valid new password after blur', () => {
    wrap();
    const newPw = screen.getByLabelText('New password');
    fireEvent.change(newPw, { target: { value: 'Passw0rd!' } });
    fireEvent.blur(newPw);
    expect(screen.queryByText(/^Passcode must/)).not.toBeInTheDocument();
  });

  it('shows no inline new-password error when untouched at first load', () => {
    wrap();
    expect(screen.queryByText(/^Passcode must/)).not.toBeInTheDocument();
  });
});
