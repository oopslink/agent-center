import type React from 'react';
import {
  createContext,
  lazy,
  Suspense,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
} from 'react';
import { useLocation } from 'react-router-dom';
import { useTranslation } from 'react-i18next';

const FloatingDmWindow = lazy(() =>
  import('./FloatingDmWindow').then((mod) => ({ default: mod.FloatingDmWindow })),
);

interface FloatingDmContextValue {
  open: (conversationId: string) => void;
}

const FloatingDmContext = createContext<FloatingDmContextValue | null>(null);

export function useFloatingDm(): FloatingDmContextValue | null {
  return useContext(FloatingDmContext);
}

export function FloatingDmProvider({
  children,
}: {
  children: React.ReactNode;
}): React.ReactElement {
  const [conversationId, setConversationId] = useState<string | null>(null);
  const open = useCallback((id: string) => setConversationId(id), []);
  const close = useCallback(() => setConversationId(null), []);
  const value = useMemo(() => ({ open }), [open]);

  const location = useLocation();
  useEffect(() => {
    if (!conversationId) return;
    if (location.pathname.endsWith(`/dms/${encodeURIComponent(conversationId)}`)) close();
  }, [close, conversationId, location.pathname]);

  return (
    <FloatingDmContext.Provider value={value}>
      {children}
      {conversationId && (
        <Suspense fallback={<FloatingDmFallback conversationId={conversationId} onClose={close} />}>
          <FloatingDmWindow conversationId={conversationId} onClose={close} />
        </Suspense>
      )}
    </FloatingDmContext.Provider>
  );
}

function FloatingDmFallback({
  conversationId,
  onClose,
}: {
  conversationId: string;
  onClose: () => void;
}): React.ReactElement {
  const { t } = useTranslation('chat');
  return (
    <aside
      role="dialog"
      aria-label={t('dms.directMessage')}
      data-testid="dm-floating-chat"
      data-conversation-id={conversationId}
      className="fixed bottom-4 right-4 z-40 hidden h-[min(42rem,calc(100vh-5rem))] w-[min(28rem,calc(100vw-2rem))] flex-col overflow-hidden rounded-lg border border-border-base bg-bg-elevated text-text-primary shadow-3 md:flex"
    >
      <header className="flex h-12 shrink-0 items-center gap-3 border-b border-border-base px-3">
        <p className="min-w-0 flex-1 truncate text-sm font-semibold">{t('dms.directMessage')}</p>
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
      <div className="flex flex-1 items-center justify-center text-sm text-text-muted">
        {t('dms.detail.loading')}
      </div>
    </aside>
  );
}

function CloseIcon(): React.ReactElement {
  return (
    <svg viewBox="0 0 20 20" fill="none" stroke="currentColor" strokeWidth="1.7" aria-hidden="true" className="h-4 w-4">
      <path strokeLinecap="round" strokeLinejoin="round" d="M5 5l10 10M15 5 5 15" />
    </svg>
  );
}
