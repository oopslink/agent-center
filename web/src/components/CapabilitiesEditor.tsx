// CapabilitiesEditor — the required_capabilities chip editor (issue-577a7b0e).
//
// A task's required_capabilities drives STRICT auto-assignment: the reconciler
// only auto-assigns a pool task to an agent when
//   required_capabilities(task) ⊆ capability_tags(agent)
// (canonical set subset). Values are CANONICAL — trimmed + lowercased + deduped —
// matching the backend's single choke point (NormalizeCapabilities), so the chips
// always show exactly what gets stored. An EMPTY set = no requirement: any
// eligible agent may be auto-assigned. Shared by the task create / edit / board
// modals so the editor behaves identically everywhere.
import React, { useState } from 'react';

// canonicalCapability mirrors the backend NormalizeCapabilities per-element rule
// (trim + lowercase). Dedup happens at the set level in commit().
export function canonicalCapability(s: string): string {
  return s.trim().toLowerCase();
}

export function CapabilitiesEditor({
  value,
  onChange,
  idPrefix = 'caps',
}: {
  value: string[];
  onChange: (next: string[]) => void;
  /** Prefixes the input id + every data-testid so multiple editors don't collide. */
  idPrefix?: string;
}): React.ReactElement {
  const [draft, setDraft] = useState('');

  const commit = () => {
    const c = canonicalCapability(draft);
    if (c === '') {
      setDraft('');
      return;
    }
    // dedup against the canonical set (no-op if already present).
    if (!value.includes(c)) onChange([...value, c]);
    setDraft('');
  };

  const remove = (cap: string) => onChange(value.filter((c) => c !== cap));

  const onKeyDown = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'Enter' || e.key === ',') {
      e.preventDefault();
      commit();
    }
  };

  return (
    <div data-testid={`${idPrefix}-editor`}>
      {value.length > 0 && (
        <div className="mb-1.5 flex flex-wrap gap-1.5">
          {value.map((cap) => (
            <span
              key={cap}
              data-testid={`${idPrefix}-chip`}
              className="inline-flex items-center gap-1 rounded bg-bg-subtle px-2 py-0.5 font-mono text-xs text-text-primary"
            >
              {cap}
              <button
                type="button"
                className="text-text-muted hover:text-danger"
                onClick={() => remove(cap)}
                aria-label={`Remove capability ${cap}`}
                data-testid={`${idPrefix}-remove`}
              >
                <span aria-hidden="true">×</span>
              </button>
            </span>
          ))}
        </div>
      )}
      <input
        id={`${idPrefix}-input`}
        data-testid={`${idPrefix}-input`}
        className="block w-full rounded border border-border-base bg-bg-elevated px-3 py-2 text-sm text-text-primary placeholder:text-text-muted focus:border-accent"
        value={draft}
        onChange={(e) => setDraft(e.target.value)}
        onKeyDown={onKeyDown}
        onBlur={commit}
        placeholder="Type a capability, press Enter or comma…"
        aria-describedby={`${idPrefix}-hint`}
      />
      <p id={`${idPrefix}-hint`} className="mt-1 text-[0.6875rem] text-text-muted">
        Canonical (lowercased &amp; trimmed). Empty = no requirement — any eligible agent
        may be auto-assigned. Non-empty applies a strict subset gate.
      </p>
    </div>
  );
}
