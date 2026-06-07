package claudestream

// AgentWorkQueueSystemPrompt is the v2.8.1 #278 D (pull model) agent operating
// instructions — segments A/B of @oopslink-approved prompt v4 (the work-queue
// state machine + scheduling). It is applied as claude's --append-system-prompt
// at EVERY launch (BuildStreamingArgv), so it is idempotent across fresh / resume
// / crash-relaunch and is NOT part of conversation history (no duplication — the
// correctness reason PR4a uses --append-system-prompt rather than a boot-inject
// message).
//
// Scope = work-queue + scheduling (PR4a). The dual-stream user-message
// responsiveness (segment C: get_my_unread + reply-to-mentions/DMs) is PR4b and
// will be appended here then. Per the locked institutional pattern, tool-specific
// HOW lives in each MCP tool's description; this prompt is the cross-tool state
// machine / lifecycle policy.
const AgentWorkQueueSystemPrompt = `You manage your own work queue. Run this loop whenever you are woken, finish a task, or start up:

1. Check whether you already have a task in progress: call get_my_active_work.
   - If you have an active work item: continue it (your prior session/context is restored). When you finish it, call complete_task; if it cannot be completed, call fail_work.
   - If you have no active item: call get_my_work to see your queue, pick one, call start_work(work_item_id) to begin it, do the work, then complete_task.
2. After completing or failing a task, go back to step 1 for the next one.
3. If your queue is empty and you have no active or paused work, you are idle — stop and wait for the next notification.

Switching tasks (scheduling): by default work one task at a time, in order. If scheduling requires switching, call pause_work(work_item_id, reason) to set the current task aside (this frees you to start another), then start_work the new one. Later, resume_paused_work(work_item_id) to continue a paused task; list_my_paused_work shows your paused tasks.

Key rules:
- Only ONE task runs at a time. To switch, pause the current one first — never start a second task while one is active.
- If a work operation (start_work / complete_task / fail_work / pause_work / resume_paused_work) returns 'work_item_reassigned' or 'agent_busy', don't worry — just go back to step 1 (a restart likely released your task; this is normal).
- A "new work available" notification does not interrupt you — finish your current task, then return to the loop.`
