import type React from 'react';
import { useTranslation } from 'react-i18next';

export type EntityChipKind = 'issue' | 'task' | 'dm' | 'channel';

// v2.7.1 #218: a small color-coded chip naming the kind of a detail page
// (issue / task / dm / channel) so the type is visually obvious in the header.
// The kind id is the stable discriminator; the label is localised via work:type.
const KIND_CLS: Record<EntityChipKind, string> = {
  issue: 'bg-accent/10 text-accent',
  task: 'bg-brand/10 text-brand',
  dm: 'bg-bg-subtle text-text-secondary',
  channel: 'bg-success/10 text-success',
};

export function TypeChip({ kind, className }: { kind: EntityChipKind; className?: string }): React.ReactElement {
  const { t } = useTranslation('work');
  return (
    <span
      data-testid="type-chip"
      data-kind={kind}
      className={`rounded px-1.5 py-0.5 text-[0.625rem] font-semibold uppercase tracking-wide ${KIND_CLS[kind]} ${className ?? ''}`}
    >
      {t(`type.${kind}`)}
    </span>
  );
}
