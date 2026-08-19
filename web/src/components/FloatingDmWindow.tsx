import type React from 'react';
import { useEffect, useRef } from 'react';
import { Link } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { useConversation } from '@/api/conversations';
import { normalizeIdentityRef, useDisplayNameResolver } from '@/api/members';
import type { Conversation } from '@/api/types';
import { orgPath, useOptionalOrgContext } from '@/OrgContext';
import { useAppStore } from '@/store/app';
import { ConversationView } from './ConversationView';
import { SenderSidebarProvider } from './SenderSidebarContext';
import { dmDisplayName } from './dmDisplay';

export function FloatingDmWindow({
  conversationId,
  onClose,
}: {
  conversationId: string;
  onClose: () => void;
}): React.ReactElement {
  const { t } = useTranslation('chat');
  const panelRef = useRef<HTMLElement>(null);
  const org = useOptionalOrgContext();
  const conv = useConversation(conversationId);
  const me = useAppStore((s) => s.currentUserId);
  const resolveName = useDisplayNameResolver();
  const title = conversationTitle(
    conv.data,
    me,
    resolveName,
    t('dms.directMessage'),
    t('dms.detail.deleted'),
  );

  useEffect(() => {
    panelRef.current?.focus();
  }, [conversationId]);

  return (
    <aside
      ref={panelRef}
      role="dialog"
      aria-labelledby="floating-dm-title"
      tabIndex={-1}
      data-testid="dm-floating-chat"
      data-conversation-id={conversationId}
      onKeyDown={(event) => {
        if (event.key === 'Escape') {
          event.stopPropagation();
          onClose();
        }
      }}
      className="fixed bottom-4 right-4 z-40 hidden h-[min(42rem,calc(100vh-5rem))] w-[min(28rem,calc(100vw-2rem))] flex-col overflow-hidden rounded-lg border border-border-base bg-bg-elevated text-text-primary shadow-3 md:flex"
    >
      <header className="flex h-12 shrink-0 items-center gap-3 border-b border-border-base px-3">
        <div className="min-w-0 flex-1">
          <h2 id="floating-dm-title" className="truncate text-sm font-semibold">
            {title}
          </h2>
          <p className="truncate text-xs text-text-muted">{t('dms.floating.subtitle')}</p>
        </div>
        <Link
          to={orgPath(`/dms/${encodeURIComponent(conversationId)}`, org?.slug)}
          onClick={onClose}
          aria-label={t('dms.floating.openFullPage')}
          title={t('dms.floating.openFullPage')}
          className="inline-flex h-8 w-8 shrink-0 items-center justify-center rounded text-text-muted hover:bg-bg-subtle hover:text-text-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
        >
          <OpenFullPageIcon />
        </Link>
        <button
          type="button"
          onClick={onClose}
          aria-label={t('dms.floating.close')}
          title={t('dms.floating.close')}
          className="inline-flex h-8 w-8 shrink-0 items-center justify-center rounded text-text-muted hover:bg-bg-subtle hover:text-text-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
        >
          <CloseIcon />
        </button>
      </header>
      <div className="flex min-h-0 flex-1">
        <SenderSidebarProvider>
          <ConversationView surface="dm" conversationId={conversationId} canSend={canSendToDM(conv.data)} />
        </SenderSidebarProvider>
      </div>
    </aside>
  );
}

function canSendToDM(c: Conversation | undefined): boolean {
  return !c?.dm_type || c.dm_type === 'my_dm';
}

function conversationTitle(
  c: Conversation | undefined,
  me: string,
  resolveName: (ref: string) => string,
  fallback: string,
  deleted: string,
): string {
  if (!c) return fallback;
  if (c.dm_type === 'agent_agent_dm') return dmDisplayName(c);
  if (c.peer_display_name) return `@${c.peer_display_name}`;

  const meBare = me ? normalizeIdentityRef(me) : '';
  const peerRef =
    c.peer_identity_id ||
    (c.participants ?? [])
      .filter((p) => !p.left_at)
      .map((p) => p.identity_id)
      .find((pid) => normalizeIdentityRef(pid) !== meBare) ||
    '';
  if (peerRef) {
    const resolved = resolveName(peerRef);
    if (resolved && resolved !== peerRef) return `@${resolved}`;
    return deleted;
  }
  return c.name || fallback;
}

function OpenFullPageIcon(): React.ReactElement {
  return (
    <svg viewBox="0 0 20 20" fill="none" stroke="currentColor" strokeWidth="1.6" aria-hidden="true" className="h-4 w-4">
      <path strokeLinecap="round" strokeLinejoin="round" d="M8 5H5.5A1.5 1.5 0 0 0 4 6.5v8A1.5 1.5 0 0 0 5.5 16h8a1.5 1.5 0 0 0 1.5-1.5V12" />
      <path strokeLinecap="round" strokeLinejoin="round" d="M11 4h5v5M10 10l5.5-5.5" />
    </svg>
  );
}

function CloseIcon(): React.ReactElement {
  return (
    <svg viewBox="0 0 20 20" fill="none" stroke="currentColor" strokeWidth="1.7" aria-hidden="true" className="h-4 w-4">
      <path strokeLinecap="round" strokeLinejoin="round" d="M5 5l10 10M15 5 5 15" />
    </svg>
  );
}
