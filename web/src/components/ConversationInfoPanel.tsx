import type React from 'react';
import { useTranslation } from 'react-i18next';
import type { Participant } from '@/api/types';
import { useDisplayNameResolver } from '@/api/members';
import { useSharedFiles } from './SharedFilesPanel';
import { Avatar } from './Avatar';

// ============================================================================
// The content of the mobile ⓘ sheet — mobile-redesign-conversations.md §3.5 + §5,
// mockup frame ⑦ ("Context Panel 抽屉（点 ⓘ，ChannelDetail）").
//
// The ⓘ BUTTON itself is the shared <ContextPanelMobileButton> (bd895284), which
// already gave Issue/Task/PlanDetail their sheet entry point; Channel/DM were
// left out of that commit because they rendered no <ContextPanel> on mobile and
// an ⓘ would have opened an EMPTY sheet. This module supplies exactly the missing
// piece — the panel content — so the same shared button now works there too.
//
// Deliberately NOT the same content as the ConversationSurfaceMobile segments.
// The segments (Threads / Files / People) are the full interactive panels; this
// sheet is the compact READ-ONLY identity card the mockup draws: title,
// description, a Members preview and a Files preview. The spec's §3.5 (segments)
// and §5 (ⓘ → description/members/files) read as overlapping in prose; the
// mockup resolves them as two different densities of the same information.
// ============================================================================

/** How many rows each preview list shows before the "+N more" tail. */
const PREVIEW_LIMIT = 5;

export interface ConversationInfoSheetProps {
  title: string;
  description?: string;
  participants?: Participant[];
  conversationId: string;
  /** Show the Members preview (false for DMs — fixed 1:1). Default true. */
  showMembers?: boolean;
}

/**
 * The read-only content of the ⓘ sheet. A page renders it inside <ContextPanel>
 * so it portals into the shell's mobile bottom-sheet host.
 */
export function ConversationInfoSheet({
  title,
  description,
  participants = [],
  conversationId,
  showMembers = true,
}: ConversationInfoSheetProps): React.ReactElement {
  const { t } = useTranslation('chat');
  const resolve = useDisplayNameResolver();
  const files = useSharedFiles(conversationId);
  const active = participants.filter((p) => !p.left_at);
  const shownMembers = active.slice(0, PREVIEW_LIMIT);
  const memberOverflow = active.length - shownMembers.length;
  const shownFiles = files.slice(0, PREVIEW_LIMIT);
  const fileOverflow = files.length - shownFiles.length;

  return (
    <div data-testid="conversation-info-sheet">
      <p className="text-sm font-semibold text-text-primary" data-testid="conversation-info-title">
        {title}
      </p>
      {description && (
        <p className="mt-0.5 text-xs text-text-muted" data-testid="conversation-info-description">
          {description}
        </p>
      )}

      {showMembers && (
        <section className="mt-3" data-testid="conversation-info-members">
          <h3 className="mb-1.5 text-xs font-semibold text-text-secondary">
            {t('conversation.infoMembers', { count: active.length })}
          </h3>
          {active.length === 0 ? (
            <p className="text-xs text-text-muted" data-testid="conversation-info-members-empty">
              {t('conversation.infoNoMembers')}
            </p>
          ) : (
            <ul className="space-y-1">
              {shownMembers.map((p) => {
                const resolved = resolve(p.identity_id);
                const name = resolved === p.identity_id ? p.identity_id : resolved;
                return (
                  <li
                    key={p.identity_id}
                    data-testid="conversation-info-member-row"
                    className="flex min-w-0 items-center gap-2 py-1"
                  >
                    <Avatar name={name} kind={p.identity_id.startsWith('agent:') ? 'agent' : 'human'} size="sm" />
                    <span className="min-w-0 truncate text-xs text-text-primary">{name}</span>
                  </li>
                );
              })}
              {memberOverflow > 0 && (
                <li className="pt-0.5 text-xs text-text-muted" data-testid="conversation-info-members-more">
                  {t('conversation.infoMore', { count: memberOverflow })}
                </li>
              )}
            </ul>
          )}
        </section>
      )}

      <section className="mt-3" data-testid="conversation-info-files">
        <h3 className="mb-1.5 text-xs font-semibold text-text-secondary">
          {t('conversation.infoFiles', { count: files.length })}
        </h3>
        {files.length === 0 ? (
          <p className="text-xs text-text-muted" data-testid="conversation-info-files-empty">
            {t('conversation.noSharedFiles')}
          </p>
        ) : (
          <ul className="space-y-1">
            {shownFiles.map((f) => (
              <li
                key={f.uri}
                data-testid="conversation-info-file-row"
                className="flex min-w-0 items-center gap-2 py-1 text-xs text-text-primary"
              >
                <PaperclipIcon />
                <span className="min-w-0 truncate">{f.filename}</span>
              </li>
            ))}
            {fileOverflow > 0 && (
              <li className="pt-0.5 text-xs text-text-muted" data-testid="conversation-info-files-more">
                {t('conversation.infoMore', { count: fileOverflow })}
              </li>
            )}
          </ul>
        )}
      </section>
    </div>
  );
}

// ── Inline SVG icons (spec §3.2: linear stroke SVG, never emoji).

function PaperclipIcon(): React.ReactElement {
  return (
    <svg
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.8"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
      className="h-3.5 w-3.5 shrink-0 text-text-muted"
    >
      <path d="M21 11.5 12.5 20a5 5 0 0 1-7-7l8.5-8.5a3.5 3.5 0 0 1 5 5L10.5 18a2 2 0 0 1-3-3l8-8" />
    </svg>
  );
}
