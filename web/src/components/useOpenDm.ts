import { useCallback } from 'react';
import { useNavigate } from 'react-router-dom';
import { useCreateConversation } from '@/api/conversations';
import { useOptionalOrgContext } from '@/OrgContext';

// useOpenDm (v2.10.1 M6) — open (or create) a 1:1 DM with an identity ref
// (`agent:…` / `user:…`) and navigate to it. The backend dedupes a DM by its
// member set, so a repeat call reuses the existing conversation. Shared by the
// mobile Members list (tap an avatar → DM, mockup `v2.10.1-mobile` Members frame)
// and any other surface that wants a one-tap "message this member".
export function useOpenDm(): {
  open: (identityRef: string) => void;
  pending: boolean;
  error: unknown;
} {
  const navigate = useNavigate();
  const orgCtx = useOptionalOrgContext();
  const createDm = useCreateConversation();

  const open = useCallback(
    (identityRef: string) => {
      if (createDm.isPending) return;
      createDm.mutate(
        { kind: 'dm', members: [identityRef] },
        {
          onSuccess: (res) => {
            const slug = orgCtx?.slug;
            navigate(
              slug ? `/organizations/${slug}/dms/${res.conversation_id}` : `/dms/${res.conversation_id}`,
            );
          },
        },
      );
    },
    [createDm, navigate, orgCtx],
  );

  return { open, pending: createDm.isPending, error: createDm.error };
}
