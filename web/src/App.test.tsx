import { render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { App } from './App';

describe('App scaffold', () => {
  it('renders the agent-center heading on the root route', () => {
    window.history.pushState({}, '', '/channels');
    render(<App />);
    expect(screen.getByRole('heading', { name: /agent-center/i })).toBeInTheDocument();
  });

  it('falls through to the not-found placeholder for unknown routes', () => {
    window.history.pushState({}, '', '/definitely-not-a-route');
    render(<App />);
    expect(screen.getByText(/page: not-found/i)).toBeInTheDocument();
  });
});
