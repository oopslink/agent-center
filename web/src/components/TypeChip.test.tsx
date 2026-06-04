import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, render, screen } from '@testing-library/react';
import { TypeChip, type EntityChipKind } from './TypeChip';

describe('TypeChip (#218)', () => {
  afterEach(() => cleanup());

  const cases: Array<[EntityChipKind, string]> = [
    ['issue', 'Issue'],
    ['task', 'Task'],
    ['dm', 'DM'],
    ['channel', 'Channel'],
  ];

  it.each(cases)('renders the %s chip with label + data-kind', (kind, label) => {
    render(<TypeChip kind={kind} />);
    const chip = screen.getByTestId('type-chip');
    expect(chip).toHaveTextContent(label);
    expect(chip).toHaveAttribute('data-kind', kind);
  });

  it('color-codes each kind distinctly', () => {
    const classes = cases.map(([kind]) => {
      const { container } = render(<TypeChip kind={kind} />);
      const cls = (container.querySelector('[data-testid="type-chip"]') as HTMLElement).className;
      cleanup();
      return cls;
    });
    expect(new Set(classes).size).toBe(4);
  });
});
