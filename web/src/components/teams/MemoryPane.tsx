// Team WebUI — read-only team-memory two-pane (index tree ↔ rendered doc).
// Used by the Team detail "Team Memory" tab and the Template "Seed Memory" tab.
// MEMORY.md is the always-resident index; entries/<slug> are lazy.
import { useState } from 'react';
import type React from 'react';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { useTeamMemoryDoc, useTeamMemoryIndex } from '@/api/teams';
import { Skeleton } from '@/components/Skeleton';
import { btnSm } from './kit';
import { DocIcon, PinIcon } from './teamsUi';

export function MemoryPane({ teamId, heading }: { teamId: string; heading: string }): React.ReactElement {
  const index = useTeamMemoryIndex(teamId);
  const [slug, setSlug] = useState('MEMORY.md');
  const doc = useTeamMemoryDoc(teamId, slug);

  return (
    <div
      className="grid min-h-[400px] overflow-hidden rounded-lg border border-border-base bg-bg-elevated shadow-1 md:grid-cols-[260px_1fr]"
      data-testid="memory-pane"
    >
      <div className="border-b border-border-base bg-bg-subtle p-3 md:border-b-0 md:border-r">
        <div className="px-1.5 pb-2.5 pt-1 text-[0.6875rem] font-semibold uppercase tracking-wider text-text-muted">{heading}</div>
        {index.isLoading && <Skeleton height="8rem" />}
        <div className="font-mono text-xs">
          {(index.data ?? []).map((node, i) => {
            if (node.group) {
              return (
                <div key={`g-${i}`} className="px-2 pb-1 pt-3 font-sans text-[0.625rem] font-semibold uppercase tracking-wider text-text-muted">
                  {node.group}
                </div>
              );
            }
            const active = node.slug === slug;
            return (
              <div key={node.slug} className={node.pinned ? '' : 'ml-3.5 border-l border-border-base'}>
                <button
                  type="button"
                  data-testid={`memory-node-${node.slug}`}
                  aria-current={active ? 'true' : undefined}
                  className={[
                    'flex w-full items-center gap-2 rounded px-2 py-1.5 text-left motion-safe:transition-colors',
                    active ? 'bg-brand/10 text-brand-hover' : 'text-text-secondary hover:bg-bg-elevated hover:text-text-primary',
                  ].join(' ')}
                  onClick={() => setSlug(node.slug!)}
                >
                  <span className={node.pinned ? 'text-brand' : 'opacity-70'} aria-hidden="true">
                    {node.pinned ? <PinIcon className="h-3.5 w-3.5" /> : <DocIcon className="h-3.5 w-3.5" />}
                  </span>
                  <span className="truncate">{node.slug}</span>
                  <span
                    className={[
                      'ml-auto rounded px-1.5 py-px font-sans text-[0.55rem] font-semibold',
                      node.pinned ? 'border border-brand/25 text-brand' : 'border border-border-base text-text-muted',
                    ].join(' ')}
                  >
                    {node.pinned ? '常驻' : 'lazy'}
                  </span>
                </button>
              </div>
            );
          })}
        </div>
      </div>

      <div className="overflow-auto p-6" data-testid="memory-view">
        {doc.isLoading && <Skeleton height="10rem" />}
        {doc.isSuccess && (
          <>
            <div className="mb-4 flex items-start justify-between gap-3">
              <div>
                <div className="font-mono text-xs text-text-muted">{doc.data.path}</div>
                <h2 className="mt-0.5 text-[1.05rem] font-semibold text-text-primary">{doc.data.title}</h2>
              </div>
              <div className="flex gap-2">
                <button type="button" className={btnSm}>
                  Raw
                </button>
                <button type="button" className={btnSm}>
                  Copy path
                </button>
              </div>
            </div>
            {doc.data.frontmatter && (
              <pre className="mb-4 whitespace-pre-wrap rounded-lg border border-border-base bg-bg-subtle p-3.5 font-mono text-[0.7rem] text-text-muted">
                {doc.data.frontmatter}
              </pre>
            )}
            <div className="prose-team text-sm leading-relaxed text-text-secondary [&_code]:rounded [&_code]:bg-brand/10 [&_code]:px-1.5 [&_code]:py-px [&_code]:font-mono [&_code]:text-[0.75rem] [&_code]:text-brand-hover [&_h4]:mb-2 [&_h4]:mt-4 [&_h4]:text-[0.7rem] [&_h4]:font-semibold [&_h4]:uppercase [&_h4]:tracking-wide [&_h4]:text-brand-hover [&_li]:my-1 [&_p]:mb-2.5 [&_ul]:ml-4 [&_ul]:list-disc [&_ul]:text-text-muted">
              <ReactMarkdown remarkPlugins={[remarkGfm]}>{doc.data.body}</ReactMarkdown>
            </div>
          </>
        )}
      </div>
    </div>
  );
}
