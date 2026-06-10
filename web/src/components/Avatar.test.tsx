import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, render, screen } from '@testing-library/react';
import { Avatar } from './Avatar';

afterEach(() => cleanup());

describe('Avatar', () => {
  it('renders 2 initials from a multi-word name (uppercase)', () => {
    render(<Avatar name="Alice Smith" />);
    expect(screen.getByTestId('avatar')).toHaveTextContent('AS');
  });

  it('renders 1 initial from a single-word name', () => {
    render(<Avatar name="bob" />);
    expect(screen.getByTestId('avatar')).toHaveTextContent('B');
  });

  // v2.8.1 7th-bubbles: the palette is INDIGO-family only (matches the chat
  // indigo accent). Sample a spread of names; every gradient stop must be an
  // indigo/violet hue (no blue/green/teal/amber/rose/slate).
  it('uses an indigo-family gradient (indigo/violet only)', () => {
    const names = ['Alice Smith', 'bob', 'arch1', 'Builder', 'Zoe', 'Q', 'mallory jones', 'ci-bot'];
    for (const name of names) {
      render(<Avatar name={name} />);
      const cls = screen.getByTestId('avatar').className;
      // a from-* and to-* gradient stop are present, both indigo or violet.
      const stops = cls.split(/\s+/).filter((c) => c.startsWith('from-') || c.startsWith('to-'));
      expect(stops.length).toBeGreaterThanOrEqual(2);
      for (const s of stops) {
        expect(s).toMatch(/^(from|to)-(indigo|violet)-\d{3}$/);
      }
      cleanup();
    }
  });

  it('is deterministic: the same name maps to the same gradient', () => {
    render(<Avatar name="Alice Smith" />);
    const a = screen.getByTestId('avatar').className;
    cleanup();
    render(<Avatar name="Alice Smith" />);
    const b = screen.getByTestId('avatar').className;
    expect(a).toBe(b);
  });

  it('shape discriminator (not color-only): human=circle, agent=rounded-square', () => {
    render(<Avatar name="X" kind="human" />);
    expect(screen.getByTestId('avatar').className).toContain('rounded-full');
    cleanup();
    render(<Avatar name="X" kind="agent" />);
    expect(screen.getByTestId('avatar').className).toContain('rounded-lg');
  });

  it('exposes an accessible label (the name + agent marker)', () => {
    render(<Avatar name="Builder" kind="agent" />);
    const el = screen.getByTestId('avatar');
    expect(el).toHaveAttribute('role', 'img');
    expect(el.getAttribute('aria-label')).toContain('Builder');
    expect(el.getAttribute('aria-label')).toContain('agent');
  });

  it('size variants change the box size', () => {
    render(<Avatar name="X" size="sm" />);
    expect(screen.getByTestId('avatar').className).toContain('h-6');
    cleanup();
    render(<Avatar name="X" size="lg" />);
    expect(screen.getByTestId('avatar').className).toContain('h-10');
  });

  it('online status dot is not color-only (has aria-label + title)', () => {
    render(<Avatar name="X" online />);
    const dot = screen.getByTestId('avatar-status');
    expect(dot).toHaveAttribute('aria-label', 'online');
    expect(dot).toHaveAttribute('title', 'Online');
  });
});
