import type React from 'react';
import { OrgLink } from '@/OrgContext';
import { useState } from 'react';
import { useParams } from 'react-router-dom';
import { useIssue } from '@/api/issues';
import { useProject } from '@/api/projects';
import { IssueEditModal } from '@/components/IssueEditModal';
import { CollapsibleDescription } from '@/components/CollapsibleDescription';
import { WorkItemConversation } from '@/components/WorkItemConversation';
import { useConversationByOwnerRef } from '@/api/conversations';
import { ConversationSidebar } from '@/components/ConversationSidebar';
import { ContextPanel } from '@/shell/contextPanel';
import { IssueDetailSidebar, DerivedTasksBlock } from '@/components/IssueDetailSidebar';
import { IssueAttachments } from '@/components/AttachmentsSection';
import { TypeChip } from '@/components/TypeChip';
import { Breadcrumb } from '@/components/Breadcrumb';
import { MobileWorkItemBar, MobileDetailsContent, useIsMobile } from '@/components/WorkItemMobileMeta';

// IssueDetail page (/projects/:projectId/issues/:id). v2.7
// ProjectManager BC: the issue is project-scoped and driven entirely by
// its projection. Status is READ-ONLY display; all metadata edits
// (title / description / status / tags) go through the single Edit →
// IssueEditModal PATCH (Dev's #251 contract).
//
// 5th task (Phabricator-style): two-column responsive layout — the main column
// holds the title + description + conversation; the right IssueDetailSidebar
// holds the two-section read-only layout (Status + duration / Tags, then
// Project / Issue ID / Created) with the SOLE edit entry being the Edit-Issue
// pencil → IssueEditModal. v2.8.1 sidebar-align: this mirrors TaskDetailSidebar
// (minus assignee — Issues have none), symmetric with TaskDetail.
export default function IssueDetail(): React.ReactElement {
  const { projectId = '', id = '' } = useParams<{ projectId: string; id: string }>();
  const issue = useIssue(projectId, id);
  // v2.7 #192: parent project shown by name (raw id on hover), not raw project id.
  const project = useProject(issue.data?.project_id);
  // T184: resolve the issue's bound conversation for the shared col④ sidebar.
  const conv = useConversationByOwnerRef(`pm://issues/${id}`);
  const [editOpen, setEditOpen] = useState(false);
  // T309: mobile "Show info" toggle — the description/attachments/details panel
  // is collapsed by default so the chat below fills the screen (@oopslink mockup).
  const [showInfo, setShowInfo] = useState(false);
  // T145: drop the title from the breadcrumb leaf on mobile (the <h2> shows it).
  const isMobile = useIsMobile();

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
  // The Edit-Issue button hides on a terminal (discarded) issue — nothing to edit.
  const isTerminal = iss.status === 'discarded';

  return (
    <section className="flex h-full flex-col" data-testid="page-IssueDetail" data-issue-id={iss.id}>
      <div className="mb-2">
        <Breadcrumb
          items={[
            { label: 'Projects', to: '/projects' },
            { label: project.data?.name || 'Project', to: `/projects/${encodeURIComponent(iss.project_id)}` },
            { label: 'Issues' },
            {
              label: isMobile
                ? iss.org_ref || 'Issue'
                : iss.org_ref
                  ? `${iss.org_ref} - ${iss.title || iss.id}`
                  : iss.title || iss.id,
            },
          ]}
        />
      </div>

      <div className="flex min-h-0 flex-1 flex-col gap-4 overflow-hidden lg:flex-row">
        {/* main column — title + description + conversation */}
        <div className="flex min-h-0 min-w-0 flex-1 flex-col">
          <header className="border-b border-border-base pb-3">
            <div className="flex flex-wrap items-center gap-2">
              {/* T145: clamp the title to 2 lines on mobile (full title on ≥md). */}
              <h2 className="line-clamp-2 text-lg font-semibold md:line-clamp-none md:text-xl">
                {iss.org_ref && <span className="text-text-muted" data-testid="issue-org-ref">{iss.org_ref} · </span>}
                {iss.title || iss.id}
              </h2>
              <TypeChip kind="issue" />
            </div>
          </header>

          {/* T309 (@oopslink mockup): on MOBILE the secondary info collapses behind
              a compact bar (status + Show info + Edit) so the CHAT fills the rest;
              on DESKTOP the description + attachments stay inline above the
              conversation (the sidebar carries the details). */}
          {isMobile ? (
            <>
              <MobileWorkItemBar
                kind="issue"
                status={iss.status}
                statusChangedAt={iss.status_changed_at}
                showInfo={showInfo}
                onToggleInfo={() => setShowInfo((v) => !v)}
                editable={!isTerminal}
                onEdit={() => setEditOpen(true)}
              />
              {showInfo && (
                <div
                  className="mb-3 rounded-lg border border-border-base bg-bg-elevated p-3"
                  data-testid="wi-mobile-info"
                >
                  {iss.description ? (
                    <CollapsibleDescription content={iss.description} testId="issue-description" ariaLabel="Issue description" />
                  ) : (
                    <p className="text-sm italic text-text-muted">No description.</p>
                  )}
                  <div className="mt-3 border-t border-border-base pt-3">
                    <IssueAttachments projectId={iss.project_id} issueId={iss.id} />
                  </div>
                  <div className="mt-3 border-t border-border-base pt-3">
                    <MobileDetailsContent
                      kind="issue"
                      projectId={iss.project_id}
                      projectName={project.data?.name}
                      itemId={iss.id}
                      orgRef={iss.org_ref}
                      createdAt={iss.created_at}
                      tags={iss.tags ?? []}
                    />
                  </div>
                </div>
              )}
              <div className="flex min-h-[60vh] flex-1 flex-col">
                <WorkItemConversation ownerRef={`pm://issues/${iss.id}`} bannerLabel={iss.title || iss.id} ownerCode={iss.org_ref} />
              </div>
            </>
          ) : (
            <>
              {iss.description ? (
                // T179: long descriptions default-collapse (Show more) so they don't
                // push the conversation off-screen; expanding reveals the full markdown.
                <CollapsibleDescription content={iss.description} testId="issue-description" ariaLabel="Issue description" />
              ) : (
                <p className="mt-4 text-sm italic text-text-muted">No description.</p>
              )}
              <div className="mt-4 border-t border-border-base pt-3">
                <IssueAttachments projectId={iss.project_id} issueId={iss.id} />
              </div>
              <WorkItemConversation ownerRef={`pm://issues/${iss.id}`} bannerLabel={iss.title || iss.id} ownerCode={iss.org_ref} />
            </>
          )}
        </div>

        {/* metadata sidebar — 2-section IssueDetail layout (read-only display top /
            read-only bottom), mirror of TaskDetailSidebar minus assignee. The
            ONLY edit path is the Edit-Issue pencil → modal.
            T145: hidden on mobile (<md) — the mobile meta summary + Details panel
            above replace it so status isn't buried at the bottom.
            T324: this metadata (DETAILS) rail stays on the RIGHT; the
            conversation's Participants/Threads/Files panel is now embedded INSIDE
            the chat box (WorkItemConversation's right pane), not the shell col④.
            Below md the rail stacks after the conversation as before. */}
        <div className="hidden shrink-0 overflow-y-auto md:block lg:w-72">
          <IssueDetailSidebar
            issue={iss}
            projectName={project.data?.name}
            onEdit={() => setEditOpen(true)}
            editable={!isTerminal}
          />
          {/* T191: tasks derived from this issue (org_ref + title + status, links
              into each task). Read-only list below the metadata sidebar. */}
          <DerivedTasksBlock projectId={iss.project_id} issueId={iss.id} />
        </div>
      </div>

      {editOpen && (
        <IssueEditModal projectId={projectId} issue={iss} onClose={() => setEditOpen(false)} />
      )}

      {/* T324: on MOBILE the conversation's Participants/Threads/Files panel
          stays in the col④ bottom sheet; on DESKTOP it is embedded inside the
          chat box (WorkItemConversation's right pane), so we mount the col④
          panel for mobile only — avoiding a duplicate + an empty desktop col④. */}
      {conv.data && isMobile && (
        <ContextPanel>
          <ConversationSidebar conversationId={conv.data.id} participants={conv.data.participants ?? []} />
        </ContextPanel>
      )}
    </section>
  );
}
