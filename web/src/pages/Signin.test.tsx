import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import Signin from './Signin';

const mocks = vi.hoisted(() => ({
  signin: vi.fn(),
  reloadAfterSignin: vi.fn(),
}));

vi.mock('@/api/auth', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/api/auth')>();
  return {
    ...actual,
    authApi: {
      ...actual.authApi,
      signin: mocks.signin,
    },
  };
});

vi.mock('@/api/authRedirect', () => ({
  reloadAfterSignin: mocks.reloadAfterSignin,
}));

describe('Signin', () => {
  afterEach(() => {
    cleanup();
    mocks.signin.mockReset();
    mocks.reloadAfterSignin.mockReset();
  });

  it('uses a full-page navigation after successful signin', async () => {
    mocks.signin.mockResolvedValue({ identity_id: 'user-1' });

    render(
      <MemoryRouter initialEntries={['/signin']}>
        <Signin />
      </MemoryRouter>,
    );

    fireEvent.change(screen.getByLabelText('Email or display name'), {
      target: { value: 'oopslink' },
    });
    fireEvent.change(screen.getByLabelText('Password'), {
      target: { value: 'Passw0rd1!' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Sign in' }));

    await waitFor(() =>
      expect(mocks.signin).toHaveBeenCalledWith({ login: 'oopslink', passcode: 'Passw0rd1!' }),
    );
    expect(mocks.reloadAfterSignin).toHaveBeenCalledTimes(1);
  });
});
