import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, within } from '@testing-library/react';
import { EntitySelect, type EntityOption } from './EntitySelect';

const opts: EntityOption[] = [
  { value: 'w-1', label: 'box-one', badge: 'online' },
  { value: 'w-2', label: 'box-two', badge: 'offline' },
  { value: 'a-1', label: 'builder-bot', badge: 'agent' },
];

function setup(props: Partial<React.ComponentProps<typeof EntitySelect>> = {}) {
  const onChange = vi.fn();
  render(
    <EntitySelect
      testId="sel"
      options={opts}
      value={props.value}
      onChange={props.onChange ?? onChange}
      placeholder="Pick one…"
      {...props}
    />,
  );
  return { onChange: (props.onChange as ReturnType<typeof vi.fn>) ?? onChange };
}

describe('EntitySelect (#191)', () => {
  afterEach(() => cleanup());

  it('shows the placeholder on the trigger when nothing is selected', () => {
    setup();
    expect(screen.getByTestId('sel-trigger')).toHaveTextContent('Pick one…');
    // Closed by default.
    expect(screen.queryByTestId('sel-options')).not.toBeInTheDocument();
  });

  it('shows the selected option label on the trigger', () => {
    setup({ value: 'w-2' });
    expect(screen.getByTestId('sel-trigger')).toHaveTextContent('box-two');
  });

  it('opens the popover with all options on trigger click', () => {
    setup();
    fireEvent.click(screen.getByTestId('sel-trigger'));
    expect(screen.getByTestId('sel-options')).toBeInTheDocument();
    expect(screen.getAllByTestId('sel-option')).toHaveLength(3);
  });

  it('filters options by the search query (label or value)', () => {
    setup();
    fireEvent.click(screen.getByTestId('sel-trigger'));
    fireEvent.change(screen.getByTestId('sel-search'), { target: { value: 'builder' } });
    const visible = screen.getAllByTestId('sel-option');
    expect(visible).toHaveLength(1);
    expect(visible[0]).toHaveAttribute('data-value', 'a-1');
  });

  it('selecting an option calls onChange and closes the popover', () => {
    const { onChange } = setup();
    fireEvent.click(screen.getByTestId('sel-trigger'));
    fireEvent.click(screen.getAllByTestId('sel-option')[1]);
    expect(onChange).toHaveBeenCalledWith('w-2');
    expect(screen.queryByTestId('sel-options')).not.toBeInTheDocument();
  });

  it('shows an empty hint when no option matches', () => {
    setup();
    fireEvent.click(screen.getByTestId('sel-trigger'));
    fireEvent.change(screen.getByTestId('sel-search'), { target: { value: 'zzz' } });
    expect(screen.getByTestId('sel-empty')).toBeInTheDocument();
    expect(screen.queryAllByTestId('sel-option')).toHaveLength(0);
  });

  it('closes on Escape without selecting', () => {
    const { onChange } = setup();
    fireEvent.click(screen.getByTestId('sel-trigger'));
    fireEvent.keyDown(screen.getByTestId('sel-search'), { key: 'Escape' });
    expect(screen.queryByTestId('sel-options')).not.toBeInTheDocument();
    expect(onChange).not.toHaveBeenCalled();
  });

  it('does not open when disabled', () => {
    setup({ disabled: true });
    fireEvent.click(screen.getByTestId('sel-trigger'));
    expect(screen.queryByTestId('sel-options')).not.toBeInTheDocument();
  });

  // T147: an optional `leading` slot (e.g. an avatar) renders in BOTH the trigger
  // (for the selected option) and each option row — without disturbing the label.
  it('renders the optional leading slot in the trigger and option rows', () => {
    const withLeading: EntityOption[] = [
      { value: 'a-1', label: 'builder-bot', leading: <span data-testid="lead-a1">A</span> },
      { value: 'b-2', label: 'helper-bot', leading: <span data-testid="lead-b2">B</span> },
    ];
    render(
      <EntitySelect testId="lead" options={withLeading} value="a-1" onChange={vi.fn()} />,
    );
    // trigger shows the selected option's leading + label.
    const trigger = screen.getByTestId('lead-trigger');
    expect(trigger).toHaveTextContent('builder-bot');
    expect(within(trigger).getByTestId('lead-a1')).toBeInTheDocument();
    // each option row carries its own leading.
    fireEvent.click(trigger);
    expect(screen.getByTestId('lead-b2')).toBeInTheDocument();
  });
});
