import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { useState } from 'react';
import { EntityMultiSelect } from './EntityMultiSelect';
import type { EntityOption } from './EntitySelect';

// §1a — EntityMultiSelect is the no-checkbox multi-pick control: a searchable
// dropdown whose chosen entities render as removable chips. These tests lock the
// core behaviour (toggle in/out, chip remove, search filter) and assert no
// checkbox input is ever rendered.

const OPTS: EntityOption[] = [
  { value: 'a', label: 'Alpha' },
  { value: 'b', label: 'Beta' },
  { value: 'c', label: 'Gamma' },
];

function Harness() {
  const [values, setValues] = useState<string[]>([]);
  return <EntityMultiSelect testId="ms" options={OPTS} values={values} onChange={setValues} />;
}

function open() {
  if (!screen.queryByTestId('ms-search')) fireEvent.click(screen.getByTestId('ms-trigger'));
}
function clickOption(value: string) {
  open();
  const opt = screen.getAllByTestId('ms-option').find((el) => el.getAttribute('data-value') === value);
  if (!opt) throw new Error(`no option ${value}`);
  fireEvent.click(opt);
}

describe('EntityMultiSelect', () => {
  afterEach(cleanup);

  it('toggles options in and out, rendering chips, with no checkbox input', () => {
    render(<Harness />);
    clickOption('a');
    clickOption('c');
    let chips = screen.getAllByTestId('ms-chip').map((el) => el.getAttribute('data-value'));
    expect(chips).toEqual(['a', 'c']);
    // aria-selected reflects state; no checkbox inputs anywhere.
    expect(document.querySelector('input[type="checkbox"]')).toBeNull();
    // Toggle 'a' back off via the option row.
    clickOption('a');
    chips = screen.getAllByTestId('ms-chip').map((el) => el.getAttribute('data-value'));
    expect(chips).toEqual(['c']);
  });

  it('removes a selection via the chip remove affordance', () => {
    render(<Harness />);
    clickOption('b');
    expect(screen.getAllByTestId('ms-chip')).toHaveLength(1);
    fireEvent.click(screen.getByTestId('ms-chip-remove'));
    expect(screen.queryByTestId('ms-chip')).toBeNull();
  });

  it('filters options by the search query', () => {
    render(<Harness />);
    open();
    fireEvent.change(screen.getByTestId('ms-search'), { target: { value: 'gam' } });
    const visible = screen.getAllByTestId('ms-option').map((el) => el.getAttribute('data-value'));
    expect(visible).toEqual(['c']);
  });

  it('shows the empty label when nothing matches', () => {
    render(<Harness />);
    open();
    fireEvent.change(screen.getByTestId('ms-search'), { target: { value: 'zzz' } });
    expect(screen.getByTestId('ms-empty')).toBeTruthy();
  });
});
