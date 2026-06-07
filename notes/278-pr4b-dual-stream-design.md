# PR4b (dual-stream) — complete build design (#278 D, off v2.8.1)

Branch: `fix/v281-278-pr4b-dual-stream` (off fd9992d; trunk now 84477a0 after #204 — rebase before PR, no overlap, #204=FE chip).

## Goal
Agent dual-stream responsiveness: besides its work queue (PR4a), the agent ALSO sees + replies to user messages directed at it (@mentions in channels + DMs). New `get_my_unread` MCP tool + prompt v4 segment C (必复) + send_message tool-desc file-uploads rule.

## get_my_unread — building blocks (ALL grounded + confirmed)
- **Agent → org + display name**: agent AR (`internal/agent/agent.go`) has `OrganizationID()` + display name; ref = `agent:<id>`. agent-tools endpoint resolves via AgentSvc (agent_id → agent).
- **List agent's conversations**: `conversation.ConversationRepo.Find(ctx, ConversationFilter{OrganizationID, Status:active})` (conversation_repo.go:88) → org conversations (channels+dms), capped (DefaultConversationLimit). Filter to participant IN GO: `conv.Participants` contains `agent:<id>` (participants = JSON column, ParticipantElement[]). No participant SQL filter exists.
- **Per-conv unread**: read-state `rsRepo.FindByUserAndConv(agentRef, convID)` → LastSeenMessageID (absent = everything unread). Messages scan: `SELECT ... FROM messages WHERE conversation_id=? AND id>? LIMIT cap` (model = read_state.go countMentions, message_repo.go messageSelect cols: id, conversation_id, sender_identity_id, content_kind, content, direction, ..., posted_at).
- **Directed-at filter**: kind='dm' → ALL unread msgs (direct); kind='channel' → only `mention.Present(content, displayName)` (internal/mention; SAME matcher as wake projector + UnreadWithMentions). Exclude the agent's OWN messages (sender == agentRef).
- **Cap**: MaxUnreadCount (999) overall + per-conv cap (mirror countMentions LIMIT cap+1).

## Architecture decision
- ReadStateService (read_state.go) has db/rsRepo/msgRepo but NOT convRepo. Options: (a) add convRepo to ReadStateService ctor (ripples to NewReadStateService callers in wiring) OR (b) NEW service AgentInboxService{convRepo, rsRepo, msgRepo, mention, db} owning ListUnreadForIdentity. LEAN (b) — clean separation, no ctor ripple; wire in the same place ReadStateService is wired (find NewReadStateService call site in app wiring).
- Result type: `UnreadItem{ConversationID, ConversationKind, ConversationName, MessageID, SenderRef, Content, ContentKind, PostedAt}`. Method: `ListUnreadForIdentity(ctx, identityRef, orgID, displayName) ([]UnreadItem, error)`.

## 6 build increments (institutional, each tested)
1. **conversation-service ListUnreadForIdentity** (new AgentInboxService or method): Find(org) → participant filter → per-conv read-state + msg scan → DM-all/channel-mention filter → exclude own → collect + cap. Unit test (seed convs/msgs/read-state, assert DM-all + channel-mention-only + own-excluded + cap).
2. **wire** the new service in app wiring (alongside ReadStateService).
3. **admin agent-tools endpoint** `get_my_unread` (agent_tools_*.go): requireAgentOnWorker + resolve agent (agent_id → org + displayName via AgentSvc) → ConversationSvc/InboxSvc.ListUnreadForIdentity → JSON. Route in server.go.
4. **MCP tool** get_my_unread (mcphost tools.go makeGetMyUnread + server.go AddTool, pull-aware desc) + server_test.go wantTools 26→27.
5. **prompt v4 SEGMENT C** appended to claudestream.AgentWorkQueueSystemPrompt (or a 2nd const): the agent has TWO streams — work queue (PR4a) AND messages. Periodically / when nudged, call get_my_unread; **每条 mention/DM 必复** = reply IS the decision: accept-defer (ack + will do after current) / accept-interrupt (pause_work current → handle → resume) / reject-explain. NOT hollow ack. (mention+DM only, not channel idle chatter, not cross-org — enforced by the query.)
6. **send_message tool-desc file-uploads rule** (per tool-rule-vs-prompt-policy lock): content=keep focused not file URIs; attachments=use for files (UI renders cards), don't repeat URI. (send_message MCP tool desc in mcphost.)
+ optional: unread WAKE (like work_available) so the agent is nudged on new mention/DM — Tester2 TOP risk = real claude deep-in-task misses periodic get_my_unread. Evaluate: emit on message-write to a mentioned/DM'd agent → daemon nudge. May be PR4b or a follow.

Then: full go test ./... → rebase onto latest v2.8.1 → PR4b → @AgentCenterTester verify A6 (dual-stream) + Tester2 §3.3 run-real (dual-stream responsiveness = top risk).
