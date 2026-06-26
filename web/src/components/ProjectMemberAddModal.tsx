import type React from 'react';
import { useMemo, useState } from 'react';
import { useAddProjectMember } from '@/api/projects';
import { useMembers, normalizeIdentityRef, identityRefOf } from '@/api/members';
import type { ProjectMember } from '@/api/types';

interface Props {
  projectId: string;
  existing: ProjectMember[]; // current project members — excluded from candidates
  onClose: () => void;
}

// ProjectMemberAddModal (v2.7 #207) — mirrors the #167 channel-invite pattern,
// including its selection UX (toggle-button rows with ring+checkmark and a
// select-all/count header) so the project "Add members" and channel "Invite
// members" dialogs look and behave identically. The actor searches org members
// by name, sees Human/Agent tags, multi-selects, and confirms a batch add.
// Candidates are org members (status joined) not already on the project.
// Selecting submits "<kind>:<id>" refs the pm add endpoint expects.
export function ProjectMemberAddModal({ projectId, existing, onClose }: Props): React.ReactElement {
  const members = useMembers();
  const add = useAddProjectMember(projectId);
  const [query, setQuery] = useState('');
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [errorMsg, setErrorMsg] = useState('');

  const existingIds = useMemo(
    () => new Set(existing.map((m) => normalizeIdentityRef(m.identity_id))),
    [existing],
  );

  const candidates = useMemo(() => {
    const q = query.trim().toLowerCase();
    return (members.data ?? [])
      .filter((m) => m.status === 'joined' && !existingIds.has(normalizeIdentityRef(m.identity_id)))
      .filter((m) => {
        if (!q) return true;
        const name = (m.display_name ?? '').toLowerCase();
        return name.includes(q) || m.identity_id.toLowerCase().includes(q);
      });
  }, [members.data, existingIds, query]);

  const toggle = (ref: string) =>
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(ref)) next.delete(ref);
      else next.add(ref);
      return next;
    });

  // Select-all over the currently-visible (search-filtered) candidates.
  const visibleRefs = useMemo(() => candidates.map(identityRefOf), [candidates]);
  const allVisibleSelected =
    visibleRefs.length > 0 && visibleRefs.every((r) => selected.has(r));
  const toggleAll = () =>
    setSelected((prev) => {
      const next = new Set(prev);
      if (allVisibleSelected) visibleRefs.forEach((r) => next.delete(r));
      else visibleRefs.forEach((r) => next.add(r));
      return next;
    });

  const confirm = async () => {
    if (selected.size === 0) return;
    setErrorMsg('');
    try {
      for (const ref of selected) {
        await add.mutateAsync({ identityId: ref, role: 'member' });
      }
      onClose();
    } catch (e) {
      setErrorMsg((e as Error).message);
    }
  };

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/40"
      role="dialog"
      aria-modal="true"
      aria-label="Add project members"
      data-testid="project-add-member-modal"
      onClick={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}
    >
      <div className="w-full max-w-sm rounded-lg border border-border bg-bg-elevated p-4 shadow-[var(--shadow-3)]">
        <div className="mb-3 flex items-center justify-between">
          <h2 className="text-sm font-semibold text-text-primary">Add members</h2>
          <button
            type="button"
            className="text-text-muted hover:text-text-primary"
            onClick={onClose}
            aria-label="Close"
            data-testid="project-add-close"
          >
            X
          </button>
        </div>
        <input
          type="text"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder="Search members…"
          autoFocus
          className="mb-3 w-full rounded border border-border-base bg-bg-elevated px-2 py-1 text-sm text-text-primary placeholder:text-text-muted focus:border-accent"
          data-testid="project-add-search"
        />
        <div className="mb-2 flex items-center justify-between px-1 text-xs text-text-muted">
          <span data-testid="project-add-selected-count">{selected.size} selected</span>
          {visibleRefs.length > 0 && (
            <button
              type="button"
              onClick={toggleAll}
              className="font-medium text-accent hover:underline"
              data-testid="project-add-select-all"
            >
              {allVisibleSelected ? 'Clear' : 'Select all'}
            </button>
          )}
        </div>
        <ul className="max-h-64 space-y-1 overflow-y-auto" data-testid="project-add-candidates">
          {candidates.length === 0 && (
            <li className="px-1 py-2 text-xs italic text-text-muted" data-testid="project-add-no-candidates">
              No matching members.
            </li>
          )}
          {candidates.map((m) => {
            const ref = identityRefOf(m);
            return (
              <li key={ref}>
                <button
                  type="button"
                  onClick={() => toggle(ref)}
                  aria-pressed={selected.has(ref)}
                  className={`flex w-full items-center gap-2 rounded px-2 py-1.5 text-left text-sm ${
                    selected.has(ref)
                      ? 'bg-bg-subtle text-text-primary ring-1 ring-inset ring-accent'
                      : 'text-text-secondary hover:bg-bg-subtle'
                  }`}
                  data-testid="project-add-candidate"
                  data-ref={ref}
                  data-selected={selected.has(ref) ? 'true' : 'false'}
                >
                  <span className="flex-1 truncate">{m.display_name || m.identity_id}</span>
                  <span
                    className="rounded bg-bg-subtle px-1.5 text-[0.625rem] uppercase text-text-muted"
                    data-testid="project-add-candidate-kind"
                  >
                    {m.kind === 'agent' ? 'Agent' : 'Human'}
                  </span>
                  <span className="flex h-4 w-4 shrink-0 items-center justify-center text-accent" aria-hidden="true">
                    {selected.has(ref) && <CheckIcon />}
                  </span>
                </button>
              </li>
            );
          })}
        </ul>
        {errorMsg && (
          <p className="mt-2 text-xs text-danger" data-testid="project-add-error">
            {errorMsg}
          </p>
        )}
        <div className="mt-3 flex justify-end gap-2">
          <button
            type="button"
            onClick={onClose}
            className="rounded px-3 py-1 text-xs text-text-secondary hover:bg-bg-subtle"
          >
            Cancel
          </button>
          <button
            type="button"
            onClick={confirm}
            disabled={selected.size === 0 || add.isPending}
            className="rounded bg-btn-primary-bg px-3 py-1 text-xs font-medium text-btn-primary-fg hover:opacity-90 disabled:bg-bg-subtle disabled:text-text-muted"
            data-testid="project-add-confirm"
          >
            {add.isPending ? 'Adding…' : `Add${selected.size > 0 ? ` (${selected.size})` : ''}`}
          </button>
        </div>
      </div>
    </div>
  );
}

// inline check (no-emoji UX rule — single-stroke SVG), shown on selected rows.
function CheckIcon(): React.ReactElement {
  return (
    <svg viewBox="0 0 20 20" fill="none" className="h-4 w-4 stroke-current" strokeWidth="2" aria-hidden="true">
      <path d="M5 10.5l3.5 3.5L15 6.5" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}
