import type React from 'react';

export type EntityChipKind = 'issue' | 'task' | 'dm' | 'channel';

// v2.7.1 #218: a small color-coded chip naming the kind of a detail page
// (issue / task / dm / channel) so the type is visually obvious in the header.
const KINDS: Record<EntityChipKind, { label: string; cls: string }> = {
  issue: { label: 'Issue', cls: 'bg-accent/10 text-accent' },
  task: { label: 'Task', cls: 'bg-brand/10 text-brand' },
  dm: { label: 'DM', cls: 'bg-bg-subtle text-text-secondary' },
  channel: { label: 'Channel', cls: 'bg-success/10 text-success' },
};

export function TypeChip({ kind, className }: { kind: EntityChipKind; className?: string }): React.ReactElement {
  const k = KINDS[kind];
  return (
    <span
      data-testid="type-chip"
      data-kind={kind}
      className={`rounded px-1.5 py-0.5 text-[0.625rem] font-semibold uppercase tracking-wide ${k.cls} ${className ?? ''}`}
    >
      {k.label}
    </span>
  );
}
