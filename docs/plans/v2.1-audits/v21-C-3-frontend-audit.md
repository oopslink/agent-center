# v2.1-C-3 — unread tracking frontend + e2e audit

> Run 2026-05-24 · v2.1-C ST 3 of 3 (per-ST P12 cadence per x9527
> rule for ≥3h work with schema/BC/wire scope). Audit log first;
> impl commit second.

## § 0. Scope

C-1 landed the schema, C-2 landed the backend (repo + service +
`GET /api/conversations/{id}/unread` + `POST .../seen` +
`conversation.read_state.changed` SSE event). This ST adds the
frontend wire + e2e:

1. **Typed API binding**: new `web/src/api/readState.ts` exposing
   `useUnread(conversationId)` (GET) + `useMarkSeen()` (mutation).
2. **Per-conversation badge**: `Channels` + `DMs` list pages render
   the unread count next to each row's name.
3. **Auto-mark-seen on conversation mount**: opening a `ChannelDetail`
   or `DMDetail` page with messages → POST `.../seen` with the latest
   message id. Backward / same-id is a no-op on the server, so this
   is safe to fire on every mount.
4. **SSE wiring**: `dispatchToQueryClient` invalidates the
   per-conversation unread query when `conversation.read_state.changed`
   fires AND the message-added handler also invalidates the per-conv
   unread for all listeners (so tabs that aren't focused stay current).
5. **Query-key + cache**: `qk.unread(convId)` + invalidations on the
   POST mutation success path.
6. **e2e**: Playwright spec — seed a channel + N messages with the
   sqlite helper (S9 codified rule: never CLI subprocess while server
   runs), open the channel list, assert the badge shows N, open the
   channel, hit back, assert the badge clears.

Out of scope (would be v2.2 if requested):
- Per-thread unread (subset of per-conversation unread; not part of
  v2.1 plan).
- Cross-tab "mark all read" toolbar action.
- Mention-only unread (we count all messages including system ones).

## § 1. API binding

File: `web/src/api/readState.ts`.

```ts
export interface UnreadSummary {
  conversation_id: string;
  user_id: string;
  last_seen_message_id: string;
  unread_count: number;
}

export function useUnread(conversationId: string | undefined) {
  return useQuery({
    queryKey: qk.unread(conversationId ?? ''),
    queryFn: () => api.get<UnreadSummary>(`/conversations/${conversationId}/unread`),
    enabled: !!conversationId,
    staleTime: 5_000,
  });
}

export function useMarkSeen() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ conversationId, lastSeenMessageId }: {
      conversationId: string; lastSeenMessageId: string;
    }) => api.post<MarkSeenResult>(`/conversations/${conversationId}/seen`, {
      last_seen_message_id: lastSeenMessageId,
    }),
    onSuccess: (_, vars) => {
      void qc.invalidateQueries({ queryKey: qk.unread(vars.conversationId) });
    },
  });
}
```

Key additions:
- `qk.unread(convId) = ['unread', convId]`.
- `staleTime: 5_000` so badge re-renders quickly when SSE invalidates,
  but rapid back-to-back list visits don't refetch needlessly.

## § 2. Badge rendering

Tactical placement choice:

- **`Channels.tsx`** and **`DMs.tsx`** wrap each `<li>` row with a
  small "UnreadCount" component that internally calls `useUnread`. We
  intentionally do NOT batch via `FindByUserBatch` for v2.1: per-row
  `useUnread` queries are de-duplicated by react-query, and the
  channels list is small (<50 in any realistic v2 single-user case).
  If volume becomes a problem, swap in a batch endpoint in v2.2.
- Badge appearance: a small rounded pill,
  `bg-blue-600 text-white text-xs px-1.5` next to the channel name.
  Hidden when count == 0. Renders `999+` when count == 999 (cap).

## § 3. Auto-mark-seen on mount

`ChannelDetail` and `DMDetail` already fetch messages via
`useMessages()`. On first success with a non-empty list:

```ts
const markSeen = useMarkSeen();
useEffect(() => {
  if (!messages.data?.length || !channel?.id) return;
  const latest = messages.data[messages.data.length - 1];
  markSeen.mutate({
    conversationId: channel.id,
    lastSeenMessageId: latest.id,
  });
}, [messages.data, channel?.id]);
```

Reasoning:
- Fires once per page mount + every time a fresh message list arrives
  (i.e. after SSE invalidation). The server-side only-forward guard
  makes redundant POSTs cheap (Bumped=false, no event, no row write
  beyond the conditional UPSERT path that early-returns).
- We use the LAST message in the array (messages are sorted ASC by
  `posted_at` per `MessageRepo.FindByConversationID`).
- The `markSeen` mutation handle is stable enough that we don't need
  to add it to the deps array (caller doesn't need to react to
  identity changes); but for lint cleanliness we still list it. The
  mutation is intentionally fire-and-forget — no spinner, no error
  toast (a failed POST simply leaves the badge stale; user can refresh).

## § 4. SSE wiring

Extend `dispatchToQueryClient`:

```ts
case 'conversation.read_state.changed':
  if (ev.conversation_id) {
    invalidate(qk.unread(ev.conversation_id));
  }
  return;
```

Also extend the existing `conversation.message_added` branch:

```ts
case 'conversation.message_added':
  if (ev.conversation_id) {
    invalidate(qk.messages(ev.conversation_id));
    invalidate(qk.unread(ev.conversation_id));  // NEW
  }
  return;
```

This is the critical bit: when a new message arrives, every tab
viewing the channel list sees the badge tick up via SSE. The
auto-mark-seen on the focused tab then clears the count again.

## § 5. Tests

### 5.1 Unit (`web/src/api/readState.test.tsx`)

- `useUnread → hits GET /conversations/:id/unread, returns summary`.
- `useMarkSeen → posts body, invalidates unread on success`.
- `useUnread disabled when conversationId undefined`.

### 5.2 Component (`web/src/components/UnreadBadge.test.tsx`)

- Renders nothing when count == 0.
- Renders pill with count when count > 0.
- Renders `999+` when count == 999.

### 5.3 Page (`web/src/pages/Channels.test.tsx` augment)

Add MSW handler so channels list returns 1 row, and the unread
endpoint returns count=3 → assert the badge text "3" is rendered.

### 5.4 Page (`web/src/pages/ChannelDetail.test.tsx` augment)

- Mount, wait for messages to render, assert the POST `.../seen`
  fired with the last message id (MSW spy / fetch mock).
- Mount with empty message list → no POST.

### 5.5 Dispatch (`web/src/sse/useSSE.test.tsx` augment)

- `conversation.read_state.changed` → invalidates `qk.unread`.
- `conversation.message_added` → invalidates both `qk.messages` AND
  `qk.unread`.

### 5.6 e2e (`tests/e2e/v2/tests/unread-tracking.spec.ts`)

Cold-start journey:
1. Seed channel `unread-e2e` + 3 messages via direct sqlite INSERT
   (S9 rule).
2. Open `/channels` → wait for row.
3. Assert `[data-testid='unread-badge']` reads "3".
4. Click row → channel detail.
5. Wait for messages to render.
6. Click back to channels list.
7. Assert the badge is now absent (count == 0).

## § 6. Acceptance criteria

- Audit log committed first.
- Impl commit lands second: 1 new api file + 1 new badge component
  + 4 page edits (Channels / DMs / ChannelDetail / DMDetail) + 1 SSE
  dispatch edit + 1 queryKeys edit + unit tests + e2e spec.
- `pnpm test` — all unit tests green.
- `pnpm typecheck` — clean.
- `pnpm lint` — clean.
- `make build` — backend + frontend bundle.
- `pnpm test:e2e tests/e2e/v2/tests/unread-tracking.spec.ts` — green.

## § 7. Execution log

To be filled by the impl commit.
