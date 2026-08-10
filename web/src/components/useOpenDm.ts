import { useCallback } from 'react';
import { useNavigate } from 'react-router-dom';
import { useCreateConversation } from '@/api/conversations';
import { useOptionalOrgContext } from '@/OrgContext';
import { useIsMobile } from '@/components/WorkItemMobileMeta';
import { useFloatingDm } from './FloatingDmContext';

// useOpenDm (v2.10.1 M6) — open (or create) a 1:1 DM with an identity ref
// (`agent:…` / `user:…`) and open it. Desktop app-shell callers use the floating
// chat window; mobile and standalone tests without the shell provider still
// navigate to the full DM page. The backend dedupes a DM by its member set, so a
// repeat call reuses the existing conversation.
export function useOpenDm(): {
  open: (identityRef: string) => Promise<boolean>;
  pending: boolean;
  error: unknown;
} {
  const navigate = useNavigate();
  const orgCtx = useOptionalOrgContext();
  const createDm = useCreateConversation();
  const floatingDm = useFloatingDm();
  const isMobile = useIsMobile();

  const open = useCallback(
    async (identityRef: string) => {
      if (createDm.isPending) return false;
      // v2.10.2 [T159]: navigate via the mutateAsync PROMISE chain, NOT mutate()'s
      // per-call onSuccess. A caller (the agent activity sidebar's "Open DM" button)
      // closes — and thus UNMOUNTS — itself synchronously right after open(); React
      // Query then discards the per-call onSuccess (the navigate) when the mutation
      // resolves on an unmounted observer, so the DM was created but never opened.
      // A plain promise is not tied to the observer, so it still runs after
      // unmount. The shared hook-level onSuccess (cache invalidation) is untouched.
      try {
        const res = await createDm.mutateAsync({ kind: 'dm', members: [identityRef] });
        if (floatingDm && !isMobile) {
          floatingDm.open(res.conversation_id);
        } else {
          const slug = orgCtx?.slug;
          navigate(
            slug ? `/organizations/${slug}/dms/${res.conversation_id}` : `/dms/${res.conversation_id}`,
          );
        }
        return true;
      } catch {
        // Failure surfaces via createDm.error; swallow so there's no unhandled
        // rejection (the open simply doesn't happen).
        return false;
      }
    },
    [createDm, floatingDm, isMobile, navigate, orgCtx?.slug],
  );

  return { open, pending: createDm.isPending, error: createDm.error };
}
