import type React from 'react';
import { OrgLink } from '@/OrgContext';
import { useState } from 'react';
import { useParams } from 'react-router-dom';
import {
  useIssue,
  useTransitionIssue,
} from '@/api/issues';
import { useProject } from '@/api/projects';
import { IssueEditModal } from '@/components/IssueEditModal';
import { MarkdownMessage } from '@/components/MarkdownMessage';
import { WorkItemConversation } from '@/components/WorkItemConversation';
import { IssueTaskSidebar, type SidebarMetaRow } from '@/components/IssueTaskSidebar';
import { EntityRef } from '@/components/EntityRef';
import { TypeChip } from '@/components/TypeChip';
import { Breadcrumb } from '@/components/Breadcrumb';
import { useDisplayNameResolver } from '@/api/members';
import type { IssueStatus } from '@/api/types';

// v2.8.1 free-state model (@oopslink): the canonical full Issue status enum the
// transition menu iterates (showing all states minus the current one).
const ISSUE_STATUSES: IssueStatus[] = [
  'open',
  'in_progress',
  'resolved',
  'closed',
  'discarded',
  'reopened',
];

// IssueDetail page (/projects/:projectId/issues/:id). v2.7
// ProjectManager BC: the issue is project-scoped and driven entirely by
// its projection. State changes go through the single transition
// endpoint; metadata edits via PATCH.
//
// 5th task (Phabricator-style): two-column responsive layout — the main column
// holds the title + description + conversation; the right IssueTaskSidebar
// holds the prominent status block + actions (Edit / transitions) + metadata.
export default function IssueDetail(): React.ReactElement {
  const { projectId = '', id = '' } = useParams<{ projectId: string; id: string }>();
  const issue = useIssue(projectId, id);
  const transition = useTransitionIssue(projectId, id);
  // v2.7 #192: resolve created_by ref → display name (raw ref on hover); a
  // deleted/unresolvable author renders "(deleted)".
  const resolveName = useDisplayNameResolver();
  // v2.7 #192: parent project shown by name (raw id on hover), not raw project id.
  const project = useProject(issue.data?.project_id);
  const [editOpen, setEditOpen] = useState(false);

  if (issue.isLoading) {
    return (
      <section className="text-sm text-text-muted" data-testid="page-IssueDetail">
        Loading issue…
      </section>
    );
  }
  if (issue.isError) {
    return (
      <section className="space-y-3" data-testid="page-IssueDetail">
        <p className="text-sm text-danger" data-testid="issue-not-found">
          {(issue.error as Error).message}
        </p>
        <OrgLink to={`/projects/${encodeURIComponent(projectId)}`} className="text-accent hover:underline">
          Back to project
        </OrgLink>
      </section>
    );
  }
  if (!issue.data) {
    return (
      <section className="text-sm text-danger" data-testid="page-IssueDetail">
        Issue lookup failed.
      </section>
    );
  }

  const iss = issue.data;
  // v2.8.1 free-state model (@oopslink): the Issue status machine is fully free —
  // any valid state → any valid state (no adjacency constraints). The transition
  // menu lists ALL issue states EXCEPT the current one.
  const targets = ISSUE_STATUSES.filter((s) => s !== iss.status);
  const isTerminal = iss.status === 'discarded';

  const actions = (
    <>
      {!isTerminal && (
        <button
          type="button"
          onClick={() => setEditOpen(true)}
          className="rounded bg-bg-subtle px-2.5 py-1 text-xs font-medium text-text-primary hover:bg-border-base"
          data-testid="issue-edit-button"
        >
          Edit
        </button>
      )}
      {targets.map((t) => (
        <button
          key={t}
          type="button"
          onClick={() => transition.mutate(t)}
          disabled={transition.isPending}
          className="rounded bg-brand px-2.5 py-1 text-xs font-medium text-white hover:bg-brand-hover disabled:opacity-50"
          data-testid={`issue-transition-${t}`}
        >
          {transitionLabel(t)}
        </button>
      ))}
    </>
  );

  const meta: SidebarMetaRow[] = [
    {
      label: 'Created by',
      value: (
        <EntityRef
          id={iss.created_by}
          name={resolveName(iss.created_by) === iss.created_by ? undefined : resolveName(iss.created_by)}
          testId="issue-created-by"
        />
      ),
    },
    ...(iss.project_id
      ? [
          {
            label: 'Project',
            value: (
              <OrgLink
                to={`/projects/${encodeURIComponent(iss.project_id)}`}
                className="text-accent hover:underline"
                data-testid="issue-project-link"
                title={iss.project_id}
              >
                {project.data?.name || iss.project_id}
              </OrgLink>
            ),
          } satisfies SidebarMetaRow,
        ]
      : []),
  ];

  return (
    <section className="flex h-full flex-col" data-testid="page-IssueDetail" data-issue-id={iss.id}>
      <div className="mb-2">
        <Breadcrumb
          items={[
            { label: 'Projects', to: '/projects' },
            { label: project.data?.name || 'Project', to: `/projects/${encodeURIComponent(iss.project_id)}` },
            { label: 'Issues' },
            { label: iss.org_ref ? `${iss.org_ref} - ${iss.title || iss.id}` : iss.title || iss.id },
          ]}
        />
      </div>

      <div className="flex min-h-0 flex-1 flex-col gap-4 overflow-hidden lg:flex-row">
        {/* main column — title + description + conversation */}
        <div className="flex min-h-0 min-w-0 flex-1 flex-col">
          <header className="border-b border-border-base pb-3">
            <div className="flex flex-wrap items-center gap-2">
              <h2 className="text-xl font-semibold">
                {iss.org_ref && <span className="text-text-muted" data-testid="issue-org-ref">{iss.org_ref} · </span>}
                {iss.title || iss.id}
              </h2>
              <TypeChip kind="issue" />
            </div>
          </header>

          {transition.isError && (
            <p className="mt-2 text-xs text-danger" data-testid="issue-transition-error">
              {(transition.error as Error).message}
            </p>
          )}

          {iss.description ? (
            // @oopslink: markdown render + capped height so a long description
            // scrolls internally rather than pushing the conversation off-screen.
            // tabIndex keeps the scroll region keyboard-reachable (WCAG 2.1.1).
            <div
              className="mt-4 max-h-64 overflow-y-auto text-sm text-text-secondary"
              data-testid="issue-description"
              tabIndex={0}
              role="region"
              aria-label="Issue description"
            >
              <MarkdownMessage content={iss.description} />
            </div>
          ) : (
            <p className="mt-4 text-sm italic text-text-muted">No description.</p>
          )}

          <WorkItemConversation ownerRef={`pm://issues/${iss.id}`} bannerLabel={iss.title || iss.id} />
        </div>

        {/* right sidebar — status block + actions + metadata */}
        <div className="shrink-0 overflow-y-auto lg:w-72">
          <IssueTaskSidebar status={iss.status} actions={actions} meta={meta} />
        </div>
      </div>

      {editOpen && (
        <IssueEditModal projectId={projectId} issue={iss} onClose={() => setEditOpen(false)} />
      )}
    </section>
  );
}

function transitionLabel(status: IssueStatus): string {
  switch (status) {
    case 'in_progress':
      return 'Start';
    case 'resolved':
      return 'Resolve';
    case 'closed':
      return 'Close';
    case 'reopened':
      return 'Reopen';
    case 'discarded':
      return 'Discard';
    case 'open':
      return 'Move to Open';
    default:
      return status;
  }
}
