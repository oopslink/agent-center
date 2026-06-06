import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { MentionPicker, optionElementId } from './MentionPicker';
import type { MentionOption } from './MentionPicker';

const opts: MentionOption[] = [
  { id: 'a', name: 'Alice', secondary: 'agent:a1' },
  { id: 'b', name: 'Bob', secondary: 'user:b1' },
];

afterEach(cleanup);

describe('MentionPicker', () => {
  it('renders options as a listbox with stable option-id anchoring', () => {
    render(<MentionPicker options={opts} activeId="a" listboxId="lb" onSelect={() => {}} />);
    expect(screen.getByTestId('mention-picker')).toHaveAttribute('role', 'listbox');
    const all = screen.getAllByTestId('mention-option');
    expect(all).toHaveLength(2);
    // option element id = stable id (not DOM index)
    expect(all[0]).toHaveAttribute('id', optionElementId('lb', 'a'));
    expect(all[0]).toHaveTextContent('Alice');
  });

  it('marks the active option by id (aria-selected + data-active), not index', () => {
    render(<MentionPicker options={opts} activeId="b" listboxId="lb" onSelect={() => {}} />);
    const [a, b] = screen.getAllByTestId('mention-option');
    expect(a).toHaveAttribute('aria-selected', 'false');
    expect(b).toHaveAttribute('aria-selected', 'true');
    expect(b).toHaveAttribute('data-active', 'true');
  });

  it('shows secondary text with full-id title (#192 hover)', () => {
    render(<MentionPicker options={opts} activeId="a" listboxId="lb" onSelect={() => {}} />);
    expect(screen.getByText('agent:a1')).toHaveAttribute('title', 'agent:a1');
  });

  it('selects on mousedown (avoids textarea blur)', () => {
    const onSelect = vi.fn();
    render(<MentionPicker options={opts} activeId="a" listboxId="lb" onSelect={onSelect} />);
    fireEvent.mouseDown(screen.getAllByTestId('mention-option')[1]);
    expect(onSelect).toHaveBeenCalledWith(opts[1]);
  });

  it('shows explicit "No matches" when empty (T-9, not silent)', () => {
    render(<MentionPicker options={[]} activeId={null} listboxId="lb" onSelect={() => {}} />);
    expect(screen.getByTestId('mention-picker-empty')).toHaveTextContent('No matches');
    expect(screen.queryAllByTestId('mention-option')).toHaveLength(0);
  });
});
