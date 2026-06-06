import type React from 'react';
import { useId, useState } from 'react';

// v2.8 #276/#274 — the shared collapsible code/output block. Pure prop-driven so
// it is reused by BOTH the markdown code renderer (#276, react-markdown custom
// `code`) and the Activity tool_result inline output (#274). It takes a raw
// string; callers do any extraction (e.g. #274 derives `.content` ||
// JSON.stringify(tool_result)). No data fetching, no markdown parsing here.
//
// Design locks honored:
//  • container is a <div>, NOT a button — the disclosure toggle + copy button
//    are SIBLINGS, never nested (lock1, avoids invalid nested-interactive).
//  • disclosure: aria-expanded + aria-controls → the code region's useId() id
//    (unique per instance — multiple fences on one page don't collide); BIDI
//    (Show N more ↔ Show less); aria-label "Code|Output, N lines, state".
//  • code region: aria-live="off" so expanding 100 lines isn't read aloud;
//    body is content-exempt for #192 (raw ids allowed) — only chrome guards it.
//  • copy: copies the FULL code (not the preview) even while collapsed;
//    contextual aria-label + an aria-live="polite" "Copied" SR confirmation.
//  • v2.8: no syntax highlighting (plain monospace), no line numbers; default
//    collapsed (no persistence) — which is also the large-code perf guard.
export interface CollapsibleCodeBlockProps {
  /** raw code/output text. */
  code: string;
  /** language label (chrome badge); omitted → no badge. */
  language?: string;
  /** a11y noun: 'code' (default) or 'output' (#274 tool_result). */
  contextLabel?: 'code' | 'output';
  /** lines above which the block defaults to collapsed (v2.8 hardcode 20). */
  collapsedThreshold?: number;
  /** lines shown in the collapsed preview. */
  previewLines?: number;
}

export function CollapsibleCodeBlock({
  code,
  language,
  contextLabel = 'code',
  collapsedThreshold = 20,
  previewLines = 5,
}: CollapsibleCodeBlockProps): React.ReactElement {
  const lines = code.split('\n');
  const total = lines.length;
  const collapsible = total > collapsedThreshold;
  const [expanded, setExpanded] = useState(false);
  const [copied, setCopied] = useState(false);
  const regionId = useId();

  const noun = contextLabel === 'output' ? 'Output' : 'Code';
  const showCollapsed = collapsible && !expanded;
  const shown = showCollapsed ? lines.slice(0, previewLines).join('\n') : code;
  const hidden = total - previewLines;

  const copy = () => {
    // copy the FULL code, never the preview — the user shouldn't have to expand.
    void navigator.clipboard.writeText(code);
    setCopied(true);
    window.setTimeout(() => setCopied(false), 2000);
  };

  return (
    <div
      className="my-1 overflow-hidden rounded border border-border-base bg-bg-subtle text-sm"
      data-testid="collapsible-code-block"
    >
      {/* chrome row — language badge (non-interactive) + copy (sibling button). */}
      <div className="flex items-center justify-between gap-2 border-b border-border-base px-2 py-1">
        {language ? (
          <span className="font-mono text-xs text-text-muted" data-testid="code-lang-badge">
            {language}
          </span>
        ) : (
          <span aria-hidden="true" />
        )}
        <span className="flex items-center gap-2">
          <span
            className="text-xs text-text-muted"
            data-testid="code-copy-status"
            aria-live="polite"
          >
            {copied ? 'Copied' : ''}
          </span>
          <button
            type="button"
            className="rounded px-1.5 py-0.5 text-xs text-text-secondary hover:bg-bg-elevated"
            data-testid="code-copy-btn"
            aria-label={`Copy ${contextLabel}`}
            onClick={copy}
          >
            Copy
          </button>
        </span>
      </div>

      <pre className="overflow-x-auto px-3 py-2">
        <code
          id={regionId}
          aria-live="off"
          className="font-mono text-text-primary"
          data-testid="code-region"
        >
          {shown}
        </code>
      </pre>

      {collapsible && (
        <button
          type="button"
          className="w-full border-t border-border-base px-3 py-1 text-left text-xs text-accent hover:bg-bg-elevated"
          data-testid="code-disclosure-btn"
          aria-expanded={expanded}
          aria-controls={regionId}
          aria-label={`${noun}, ${total} lines, ${expanded ? 'expanded' : 'collapsed'}`}
          onClick={() => setExpanded((e) => !e)}
        >
          {expanded ? 'Show less' : `Show ${hidden} more lines`}
        </button>
      )}
    </div>
  );
}
