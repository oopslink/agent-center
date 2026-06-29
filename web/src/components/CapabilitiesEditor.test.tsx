// T566 (issue-577a7b0e) — CapabilitiesEditor: canonical chip editor for a task's
// required_capabilities. Covers add (canonicalized), dedup, and remove.
import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { useState } from 'react';
import { CapabilitiesEditor, canonicalCapability } from './CapabilitiesEditor';

function Harness({ initial = [] as string[], onChange }: { initial?: string[]; onChange?: (v: string[]) => void }) {
  const [value, setValue] = useState<string[]>(initial);
  return (
    <CapabilitiesEditor
      value={value}
      onChange={(next) => {
        setValue(next);
        onChange?.(next);
      }}
    />
  );
}

afterEach(() => cleanup());

describe('canonicalCapability', () => {
  it('trims and lowercases', () => {
    expect(canonicalCapability('  Go ')).toBe('go');
    expect(canonicalCapability('RUST')).toBe('rust');
  });
});

describe('CapabilitiesEditor', () => {
  const type = (v: string) => {
    const input = screen.getByTestId('caps-input');
    fireEvent.change(input, { target: { value: v } });
    fireEvent.keyDown(input, { key: 'Enter' });
  };

  it('adds a canonical (trimmed + lowercased) chip on Enter', () => {
    render(<Harness />);
    type(' Go ');
    const chips = screen.getAllByTestId('caps-chip');
    expect(chips).toHaveLength(1);
    expect(chips[0]).toHaveTextContent('go');
  });

  it('dedups a capability already present (canonical compare)', () => {
    const onChange = vi.fn();
    render(<Harness initial={['go']} onChange={onChange} />);
    type('GO');
    expect(screen.getAllByTestId('caps-chip')).toHaveLength(1);
  });

  it('removes a chip', () => {
    render(<Harness initial={['go', 'rust']} />);
    expect(screen.getAllByTestId('caps-chip')).toHaveLength(2);
    fireEvent.click(screen.getAllByTestId('caps-remove')[0]);
    const chips = screen.getAllByTestId('caps-chip');
    expect(chips).toHaveLength(1);
    expect(chips[0]).toHaveTextContent('rust');
  });

  it('ignores an empty/whitespace-only draft', () => {
    render(<Harness />);
    type('   ');
    expect(screen.queryAllByTestId('caps-chip')).toHaveLength(0);
  });
});
