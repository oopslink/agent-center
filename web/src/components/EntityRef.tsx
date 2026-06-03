import type React from 'react';
import { OrgLink } from '@/OrgContext';

interface EntityRefProps {
  /** Raw id / ref of the entity — shown only on hover (title), never as text. */
  id: string;
  /** Resolved display name. Empty/undefined → fallback (if given) or "(deleted)". */
  name?: string | null;
  /** Optional org-scoped path; when set and the entity is resolved, renders a link. */
  to?: string;
  /**
   * Visible text when `name` is unresolved BUT the entity is known to exist
   * (e.g. a present member row that simply has no display name). Use this for
   * self-entities; omit it for references that may dangle (→ "(deleted)").
   */
  fallback?: string;
  /** Placeholder text for an unresolved (deleted) reference. */
  deletedLabel?: string;
  className?: string;
  testId?: string;
}

// EntityRef (#192) — the single way to render a reference to an entity
// (agent / user / project / worker / channel / DM / member). It shows the
// human display NAME, with the raw id available only on hover (title) — never
// as visible text (UX Rule 2: no raw ids). When the name can't be resolved
// (e.g. the entity was deleted), it renders a "(deleted)" placeholder instead
// of leaking the orphan id (Rule 9). Callers resolve the name (commonly via
// useDisplayNameResolver / entity data) and pass it in.
export function EntityRef({
  id,
  name,
  to,
  fallback,
  deletedLabel = '(deleted)',
  className,
  testId = 'entity-ref',
}: EntityRefProps): React.ReactElement {
  const resolved = typeof name === 'string' && name.trim().length > 0;
  // A dangling reference: not resolved AND no fallback → "(deleted)", never linked.
  const deleted = !resolved && fallback === undefined;

  if (deleted) {
    return (
      <span
        data-testid={testId}
        data-entity-id={id}
        data-deleted="true"
        title={id}
        className={`italic text-text-muted ${className ?? ''}`}
      >
        {deletedLabel}
      </span>
    );
  }

  // Present entity: show the name, or the fallback when it has none.
  const text = resolved ? name : fallback;

  if (to) {
    return (
      <OrgLink
        to={to}
        data-testid={testId}
        data-entity-id={id}
        title={id}
        className={`hover:underline ${className ?? ''}`}
      >
        {text}
      </OrgLink>
    );
  }

  return (
    <span data-testid={testId} data-entity-id={id} title={id} className={className}>
      {text}
    </span>
  );
}
