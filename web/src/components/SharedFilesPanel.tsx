import type React from 'react';
import { useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import { useMessages } from '@/api/conversations';
import { attachmentHref, attachmentKind, formatBytes } from '@/components/MessageList';
import type { MessageAttachment } from '@/api/types';

// v2.10.0 [T2 / T64] col④ — "Shared files": the files attached to any message in
// the conversation, aggregated from the message list (there is no dedicated
// "list files" endpoint, so we derive from the same useMessages cache the
// ConversationView already populates — react-query dedups the fetch). Deduped by
// blob URI. Renders nothing when the conversation has no attachments, so the
// col④ panel stays clean (only participants show) until files exist. Mockup:
// docs/design/v2.10.0/shell-conversations-tasks.html 例1 ("共享文件 · N").
// useSharedFiles — the deduped (by blob URI) attachments across the
// conversation's messages, derived from the same useMessages cache. Shared by
// SharedFilesPanel and the v2.10.1 [T96] channel Files tab (for its count badge
// + empty state).
export function useSharedFiles(conversationId: string): MessageAttachment[] {
  const messages = useMessages(conversationId);
  return useMemo(() => {
    const seen = new Set<string>();
    const out: MessageAttachment[] = [];
    for (const m of messages.data ?? []) {
      for (const a of m.attachments ?? []) {
        if (seen.has(a.uri)) continue;
        seen.add(a.uri);
        out.push(a);
      }
    }
    return out;
  }, [messages.data]);
}

export function SharedFilesPanel({
  conversationId,
}: {
  conversationId: string;
}): React.ReactElement | null {
  const { t } = useTranslation('chat');
  const files = useSharedFiles(conversationId);

  if (files.length === 0) return null;

  return (
    <section
      aria-label={t('panels.sharedFiles.ariaLabel')}
      data-testid="shared-files-panel"
      className="border-t border-border-base px-4 py-3"
    >
      <h4 className="mb-2 flex items-center gap-2 text-sm font-semibold text-text-primary">
        {t('panels.sharedFiles.heading')}
        <span
          data-testid="shared-files-count"
          className="rounded-full bg-bg-elevated px-1.5 text-[0.6875rem] text-text-muted tabular-nums"
        >
          {files.length}
        </span>
      </h4>
      <ul className="space-y-1">
        {files.map((f) => (
          <li key={f.uri}>
            <a
              href={attachmentHref(f.uri)}
              target="_blank"
              rel="noreferrer"
              data-testid="shared-file-link"
              className="flex items-center gap-2 rounded px-1 py-0.5 text-xs text-text-secondary hover:bg-bg-subtle hover:text-text-primary"
            >
              <span aria-hidden="true" className="shrink-0 rounded bg-bg-elevated px-1 text-[0.625rem] uppercase text-text-muted">
                {attachmentKind(f.mime_type)}
              </span>
              <span className="min-w-0 flex-1 truncate">{f.filename}</span>
              <span className="shrink-0 text-text-muted">{formatBytes(f.size)}</span>
            </a>
          </li>
        ))}
      </ul>
    </section>
  );
}
