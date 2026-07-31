import React, { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useCreateConversation, useSendMessage } from '@/api/conversations';
import { AgentRuntime } from '@/components/AgentRuntime';

export function AgentMemoryManager({ agentId }: { agentId: string }): React.ReactElement {
  const { t } = useTranslation('members');
  const createDm = useCreateConversation();
  const send = useSendMessage();
  const [direction, setDirection] = useState('');
  const [sentConversationId, setSentConversationId] = useState<string | null>(null);
  const pending = createDm.isPending || send.isPending;
  const error = (createDm.error as Error | null)?.message ?? (send.error as Error | null)?.message ?? null;

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (pending) return;
    setSentConversationId(null);
    const dm = await createDm.mutateAsync({ kind: 'dm', members: [`agent:${agentId}`] });
    await send.mutateAsync({
      conversationId: dm.conversation_id,
      content: t('agentRuntime.memoryManager.optimizeMessage', {
        direction: direction.trim() || t('agentRuntime.memoryManager.defaultDirection'),
      }),
    });
    setSentConversationId(dm.conversation_id);
  };

  return (
    <section className="space-y-3" data-testid="agent-memory-manager">
      <form
        onSubmit={(e) => void submit(e)}
        className="rounded-lg border border-border-base bg-bg-elevated p-3"
        data-testid="agent-memory-optimize-form"
      >
        <div className="flex flex-col gap-3 md:flex-row md:items-end">
          <label className="min-w-0 flex-1 text-xs font-medium text-text-secondary">
            <span className="mb-1 block">{t('agentRuntime.memoryManager.directionLabel')}</span>
            <textarea
              value={direction}
              onChange={(e) => setDirection(e.target.value)}
              className="min-h-20 w-full resize-y rounded border border-border-base bg-bg-base px-3 py-2 text-sm text-text-primary placeholder:text-text-muted focus:border-brand"
              placeholder={t('agentRuntime.memoryManager.directionPlaceholder')}
              data-testid="agent-memory-optimize-direction"
            />
          </label>
          <button
            type="submit"
            disabled={pending}
            className="inline-flex min-h-[44px] shrink-0 items-center justify-center gap-2 rounded bg-brand px-3 py-2 text-sm font-medium text-white hover:bg-brand-hover disabled:opacity-50 md:min-h-0"
            data-testid="agent-memory-optimize-submit"
            aria-busy={pending}
          >
            <RefreshIcon />
            {pending ? t('agentRuntime.memoryManager.sending') : t('agentRuntime.memoryManager.optimize')}
          </button>
        </div>
        {sentConversationId && (
          <p className="mt-2 text-xs text-success" data-testid="agent-memory-optimize-sent">
            {t('agentRuntime.memoryManager.sent')}
          </p>
        )}
        {error && (
          <p className="mt-2 text-xs text-danger" data-testid="agent-memory-optimize-error">
            {error}
          </p>
        )}
      </form>
      <AgentRuntime agentId={agentId} mode="memory" />
    </section>
  );
}

function RefreshIcon(): React.ReactElement {
  return (
    <svg viewBox="0 0 16 16" className="h-4 w-4" fill="none" stroke="currentColor" strokeWidth="1.6" aria-hidden="true">
      <path d="M13 5.5A5 5 0 1 0 14 8" strokeLinecap="round" />
      <path d="M13 2.5v3h-3" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}
