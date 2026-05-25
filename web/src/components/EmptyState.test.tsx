import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { EmptyState } from './EmptyState';

describe('EmptyState', () => {
  afterEach(() => cleanup());

  it('renders title + body + default icon', () => {
    render(<EmptyState title="No widgets" body="Widgets appear here when created." />);
    expect(screen.getByText('No widgets')).toBeInTheDocument();
    expect(screen.getByText('Widgets appear here when created.')).toBeInTheDocument();
  });

  it('renders action button + fires callback', () => {
    const onClick = vi.fn();
    render(<EmptyState title="No widgets" action={{ label: 'Create', onClick }} />);
    fireEvent.click(screen.getByRole('button', { name: 'Create' }));
    expect(onClick).toHaveBeenCalledTimes(1);
  });

  it('renders navigation link when `to` provided', () => {
    render(<EmptyState title="No widgets" to={{ label: 'Open docs', href: '/help' }} />);
    const link = screen.getByRole('link', { name: /Open docs/ });
    expect(link).toHaveAttribute('href', '/help');
  });

  it('uses caller-supplied testId', () => {
    render(<EmptyState title="hi" testId="widgets-empty" />);
    expect(screen.getByTestId('widgets-empty')).toBeInTheDocument();
  });
});
