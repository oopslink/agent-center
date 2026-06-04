import type React from 'react';
import { OrgLink } from '@/OrgContext';
import { useState } from 'react';
import { useParams } from 'react-router-dom';
import {
  ISSUE_TRANSITIONS,
  useIssue,
  useTransitionIssue,
} from '@/api/issues';
import { useProject } from '@/api/projects';
import { IssueEditModal } from '@/components/IssueEditModal';
import { WorkItemConversation } from '@/components/WorkItemConversation';
import { EntityRef } from '@/components/EntityRef';
import { TypeChip } from '@/components/TypeChip';
import { useDisplayNameResolver } from '@/api/members';
import type { IssueStatus } from '@/api/types';

// IssueDetail page (/projects/:projectId/issues/:id). v2.7
// ProjectManager BC: the issue is project-scoped and driven entirely by
// its projection. State changes go through the single transition
// endpoint; metadata edits via PATCH.
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
  const targets = ISSUE_TRANSITIONS[iss.status] ?? [];
  const isTerminal = iss.status === 'withdrawn';

  return (
    <section className="flex h-full flex-col" data-testid="page-IssueDetail" data-issue-id={iss.id}>
      <header className="flex items-start justify-between border-b border-border-base pb-3">
        <div className="space-y-1">
          <div className="flex flex-wrap items-center gap-2">
            <h2 className="text-xl font-semibold">{iss.title || iss.id}</h2>
            <TypeChip kind="issue" />
          </div>
          <div className="flex flex-wrap items-center gap-2 text-xs text-text-muted">
            <span
              className="rounded bg-bg-subtle px-2 py-0.5 uppercase text-text-secondary"
              data-testid="issue-status"
            >
              {iss.status.replace(/_/g, ' ')}
            </span>
            <span>
              by{' '}
              <EntityRef
                id={iss.created_by}
                name={resolveName(iss.created_by) === iss.created_by ? undefined : resolveName(iss.created_by)}
                testId="issue-created-by"
              />
            </span>
            {iss.project_id && (
              <OrgLink
                to={`/projects/${encodeURIComponent(iss.project_id)}`}
                className="text-accent hover:underline"
                data-testid="issue-project-link"
                title={iss.project_id}
              >
                project · {project.data?.name || iss.project_id}
              </OrgLink>
            )}
          </div>
        </div>
        <div className="flex flex-wrap items-center gap-2">
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
        </div>
      </header>

      {transition.isError && (
        <p className="mt-2 text-xs text-danger" data-testid="issue-transition-error">
          {(transition.error as Error).message}
        </p>
      )}

      {iss.description ? (
        <p className="mt-4 whitespace-pre-wrap text-sm text-text-secondary" data-testid="issue-description">
          {iss.description}
        </p>
      ) : (
        <p className="mt-4 text-sm italic text-text-muted">No description.</p>
      )}

      <WorkItemConversation ownerRef={`pm://issues/${iss.id}`} bannerLabel={iss.title || iss.id} />

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
    case 'withdrawn':
      return 'Withdraw';
    case 'open':
      return 'Move to Open';
    default:
      return status;
  }
}
