import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { DeriveBar } from './DeriveBar';

describe('DeriveBar', () => {
  afterEach(() => cleanup());

  it('renders nothing when count is 0', () => {
    const { container } = render(
      <DeriveBar count={0} onOpenIssue={() => undefined} onOpenTask={() => undefined} onCancel={() => undefined} />,
    );
    expect(container.firstChild).toBeNull();
  });

  it('renders count + 3 buttons when count > 0', () => {
    const onIssue = vi.fn();
    const onTask = vi.fn();
    const onCancel = vi.fn();
    render(
      <DeriveBar count={3} onOpenIssue={onIssue} onOpenTask={onTask} onCancel={onCancel} />,
    );
    expect(screen.getByTestId('derive-bar-count')).toHaveTextContent('3 messages selected');
    fireEvent.click(screen.getByTestId('derive-open-issue'));
    fireEvent.click(screen.getByTestId('derive-open-task'));
    fireEvent.click(screen.getByTestId('derive-cancel'));
    expect(onIssue).toHaveBeenCalledTimes(1);
    expect(onTask).toHaveBeenCalledTimes(1);
    expect(onCancel).toHaveBeenCalledTimes(1);
  });

  it('uses singular noun for count=1', () => {
    render(<DeriveBar count={1} onOpenIssue={() => undefined} onOpenTask={() => undefined} onCancel={() => undefined} />);
    expect(screen.getByTestId('derive-bar-count')).toHaveTextContent('1 message selected');
  });
});
