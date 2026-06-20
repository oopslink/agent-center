import type React from 'react';
import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { MemoryRouter, useLocation } from 'react-router-dom';
import { RemindersSecondaryNav } from './RemindersSecondaryNav';

// T248 — the Reminders col② filter rail writes the filter state to the URL query
// (?range=&status=&q=); the Reminders page reads it. These tests assert the
// write side.

function LocationProbe(): React.ReactElement {
  const loc = useLocation();
  return <div data-testid="loc">{loc.search}</div>;
}

function renderNav(initial = '/organizations/acme/reminders') {
  return render(
    <MemoryRouter initialEntries={[initial]}>
      <RemindersSecondaryNav orgBase="/organizations/acme" />
      <LocationProbe />
    </MemoryRouter>,
  );
}

describe('RemindersSecondaryNav', () => {
  afterEach(() => cleanup());

  it('selecting a scope writes ?range= (and "All" clears it)', () => {
    renderNav();
    fireEvent.click(screen.getByTestId('reminder-range-created'));
    expect(screen.getByTestId('loc').textContent).toBe('?range=created');
    // "All" is the default scope → it clears the param rather than writing range=all.
    fireEvent.click(screen.getByTestId('reminder-range-all'));
    expect(screen.getByTestId('loc').textContent).toBe('');
  });

  it('selecting a status writes ?status= (and "All statuses" clears it)', () => {
    renderNav();
    fireEvent.click(screen.getByTestId('reminder-status-active'));
    expect(screen.getByTestId('loc').textContent).toBe('?status=active');
    fireEvent.click(screen.getByTestId('reminder-status-all'));
    expect(screen.getByTestId('loc').textContent).toBe('');
  });

  it('typing in search writes ?q= and preserves the other filters', () => {
    renderNav('/organizations/acme/reminders?range=created');
    fireEvent.change(screen.getByTestId('reminder-search'), { target: { value: 'deploy' } });
    const search = screen.getByTestId('loc').textContent ?? '';
    expect(search).toContain('range=created');
    expect(search).toContain('q=deploy');
  });

  it('reflects the active filters from the URL (aria-pressed)', () => {
    renderNav('/organizations/acme/reminders?range=remindee&status=paused');
    expect(screen.getByTestId('reminder-range-remindee')).toHaveAttribute('aria-pressed', 'true');
    expect(screen.getByTestId('reminder-range-all')).toHaveAttribute('aria-pressed', 'false');
    expect(screen.getByTestId('reminder-status-paused')).toHaveAttribute('aria-pressed', 'true');
    expect(screen.getByTestId('reminder-status-all')).toHaveAttribute('aria-pressed', 'false');
  });
});
