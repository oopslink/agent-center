import { describe, expect, it, vi, afterEach } from 'vitest';
import { cleanup, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { ThreadButton } from './ThreadButton';

afterEach(() => cleanup());

describe('ThreadButton', () => {
  it('always renders a button and calls onClick', async () => {
    const onClick = vi.fn();
    render(<ThreadButton onClick={onClick} />);
    const btn = screen.getByTestId('thread-button');
    await userEvent.click(btn);
    expect(onClick).toHaveBeenCalledTimes(1);
  });

  it('shows the reply count when there are replies', () => {
    render(<ThreadButton replyCount={3} onClick={() => {}} />);
    expect(screen.getByTestId('thread-reply-count')).toHaveTextContent('3');
  });

  it('omits the count chip when there are no replies', () => {
    render(<ThreadButton replyCount={0} onClick={() => {}} />);
    expect(screen.queryByTestId('thread-reply-count')).toBeNull();
  });

  it('shows the activity dot when the thread has activity', () => {
    render(<ThreadButton replyCount={2} hasActivity onClick={() => {}} />);
    expect(screen.getByTestId('thread-activity-dot')).toBeInTheDocument();
  });

  it('omits the activity dot when there is no activity', () => {
    render(<ThreadButton replyCount={2} hasActivity={false} onClick={() => {}} />);
    expect(screen.queryByTestId('thread-activity-dot')).toBeNull();
  });

  it('has an accessible label reflecting the reply count', () => {
    render(<ThreadButton replyCount={2} onClick={() => {}} />);
    expect(screen.getByRole('button', { name: /2 repl/i })).toBeInTheDocument();
  });

  it('labels an empty thread as a reply affordance', () => {
    render(<ThreadButton onClick={() => {}} />);
    expect(screen.getByRole('button', { name: /repl/i })).toBeInTheDocument();
  });
});
