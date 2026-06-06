import type React from 'react';

// MentionPicker — the #275 popup listbox for the #/@ picker. A dumb ARIA listbox:
// the composer owns trigger detection, the active index, the keyboard, and the
// aria-activedescendant on the textarea. Active option is anchored by stable
// OPTION-ID (`${listboxId}-${option.id}`), not DOM index — options re-filter as
// the user types, so an index anchor would point at the wrong row (Tester2 §4.3
// + Dev2 lock). Empty → explicit "No matches" (T-9, not a silent empty popup).
export interface MentionOption {
  id: string; // stable id (anchors aria-activedescendant + the option element id)
  name: string; // display name — inserted as the mention token
  secondary?: string; // muted secondary text (handle / full id, #192 hover)
}

interface Props {
  options: MentionOption[];
  activeId: string | null;
  listboxId: string;
  onSelect: (o: MentionOption) => void;
  onHoverActivate?: (id: string) => void;
}

export function optionElementId(listboxId: string, optionId: string): string {
  return `${listboxId}-opt-${optionId}`;
}

export function MentionPicker({
  options,
  activeId,
  listboxId,
  onSelect,
  onHoverActivate,
}: Props): React.ReactElement {
  return (
    <ul
      role="listbox"
      id={listboxId}
      className="max-h-56 overflow-auto rounded border border-border-base bg-bg-elevated py-1 text-sm shadow-lg"
      data-testid="mention-picker"
    >
      {options.length === 0 ? (
        <li className="px-3 py-1.5 text-text-muted" data-testid="mention-picker-empty">
          No matches
        </li>
      ) : (
        options.map((o) => {
          const active = o.id === activeId;
          return (
            <li
              key={o.id}
              id={optionElementId(listboxId, o.id)}
              role="option"
              aria-selected={active}
              data-testid="mention-option"
              data-active={active}
              // mousedown (not click) so selecting doesn't blur the textarea first
              onMouseDown={(e) => {
                e.preventDefault();
                onSelect(o);
              }}
              onMouseEnter={() => onHoverActivate?.(o.id)}
              // not-color-only: active gets a ring + background, not just color
              className={`flex cursor-pointer items-center gap-2 px-3 py-1.5 ${
                active ? 'bg-bg-subtle ring-1 ring-inset ring-accent' : ''
              }`}
            >
              <span className="font-medium text-text-primary">{o.name}</span>
              {o.secondary && (
                <span className="text-xs text-text-muted" title={o.secondary}>
                  {o.secondary}
                </span>
              )}
            </li>
          );
        })
      )}
    </ul>
  );
}
