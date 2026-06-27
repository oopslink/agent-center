# Message-Delivery Activity Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把"消息送达 agent / agent 主动确认已读"做成一等 activity 事件，让排障时能在 agent activity 时间线上分清「没收到 / 收到了卡在处理中 / 已处理」。

**Architecture:** 纯增量，零 schema 改动（`agent_activity_events` 已是通用 `event_type` + JSON payload）。① worker daemon 在 `converse()` 注入消息后埋一条 `message_delivered`；② 给 `MarkSeenCommand` 加 `Trigger` 来源标注，新增一个订阅 `conversation.read_state.changed` 的 outbox projector，仅对 `trigger=agent_tool` 追加 `message_acknowledged`；③ 前端 `AgentActivityRow` 渲染两种新类型且不折叠进 "Checking messages" 组。

**Tech Stack:** Go (后端，`go test ./...`)，React + TypeScript + Vitest（前端，`cd web && pnpm test`）。

设计源文档：`docs/design/features/agent-message-consumption-activity.md`。

## Global Constraints

- **所有提交必须通过测试**：每个 commit 前跑 `go test ./...`（后端）或 `cd web && pnpm test`（前端），0 failures 才能 commit。禁止 `--no-verify`。
- **单元行覆盖率 ≥ 90%**（testing 规约）。
- **append-only 不变量**（ADR-0049）：activity 事件只经 `ActivityEventRepository.Append`，不可变。
- **best-effort 不阻断**：`message_delivered` 埋点失败仅 `c.log(...)`，绝不影响消息注入主流程。
- **事件类型字符串字面量统一走常量**：producer/consumer 用同一常量，禁止散落字符串。
- **no-emoji UX 红线**：前端图标用内联 SVG，禁用 emoji。
- **DDD 分层**：projector 写 agent 域 activity 仓储、读 conversation 域事件，与既有 `WakeProjector` 同构。

---

## File Structure

| 文件 | 职责 | 动作 |
|---|---|---|
| `internal/agent/activity_event.go` | activity 事件类型常量 | 加 2 个常量 |
| `internal/conversation/service/read_state.go` | `MarkSeenCommand` + `MarkSeen` emit | 加 `Trigger` 字段/类型 + payload 透传 |
| `internal/admin/api/environment_agent.go` | PUSH 路径 mark-seen handler | 设 `Trigger=delivery` |
| `internal/admin/api/agent_tools_inbox.go` | PULL 路径 agent 工具 mark_seen handler | 设 `Trigger=agent_tool` |
| `internal/webconsole/api/handlers_unread_conversations.go` | 人类 web mark-seen | 设 `Trigger=human` |
| `internal/workerdaemon/agent_controller.go` | `converse()` 注入后埋 `message_delivered` | 加埋点 + payload helper |
| `internal/environment/service/message_ack_projector.go` | ack projector（订阅 read_state.changed） | **新建** |
| `internal/cli/webconsole_wiring.go` | projector 注册（单一来源） | 注册新 projector |
| `web/src/components/AgentActivityRow.tsx` | 类目/preview/图标 | 加 2 类目 + preview + 信封图标 |

---

## Task 1: `Trigger` 来源标注贯穿 MarkSeen + 两个事件类型常量

**Files:**
- Modify: `internal/agent/activity_event.go:28-38`
- Modify: `internal/conversation/service/read_state.go:52-58`（`MarkSeenCommand`）, `:130-143`（emit payload）
- Modify: `internal/admin/api/environment_agent.go:181-186`
- Modify: `internal/admin/api/agent_tools_inbox.go:148-153`
- Modify: `internal/webconsole/api/handlers_unread_conversations.go:221-226`
- Test: `internal/conversation/service/read_state_trigger_test.go`（新建）

**Interfaces:**
- Produces:
  - `agent.EventTypeMessageDelivered = "message_delivered"`, `agent.EventTypeMessageAcknowledged = "message_acknowledged"`
  - `convservice.MarkSeenTrigger`（string 类型）+ 常量 `MarkSeenTriggerHuman="human"` / `MarkSeenTriggerDelivery="delivery"` / `MarkSeenTriggerAgentTool="agent_tool"`
  - `MarkSeenCommand.Trigger MarkSeenTrigger`（可空；emit payload 写 `"trigger"` 键，空值落 `"human"`）

- [ ] **Step 1: 加两个 activity 事件类型常量**

在 `internal/agent/activity_event.go` 的 const 块（`:28-38`）末尾、`EventTypeUnknown` 之后加：

```go
	EventTypeUnknown       = "unknown"
	// v2.15 message-consumption activity (docs/design/features/agent-message-consumption-activity.md):
	// EventTypeMessageDelivered — worker daemon 在 sess.Inject() 后埋点，表示一条入站消息
	// 真正进入 agent 上下文（payload: {conversation_id, message_id, sender_ref, sender_display,
	// content_preview, attachments_count}）。
	// EventTypeMessageAcknowledged — agent 主动调 mark_seen 工具（PULL 路径）后由 ack projector
	// 追加，表示 agent 确认已读（payload: {conversation_id, message_id, previous_last_seen_message_id}）。
	EventTypeMessageDelivered    = "message_delivered"
	EventTypeMessageAcknowledged = "message_acknowledged"
```

- [ ] **Step 2: 给 MarkSeenCommand 加 Trigger 类型与字段**

在 `internal/conversation/service/read_state.go`，`MarkSeenCommand` 定义（`:52-58`）上方加类型，并给 struct 加字段：

```go
// MarkSeenTrigger 标注一次 mark-seen 的来源，使下游（ack projector）能区分
// PUSH 路径系统自动盖章（delivery）、PULL 路径 agent 主动确认（agent_tool）、
// 人类已读（human）。仅用于事件标注，不改变 only-forward / 乐观锁语义。
type MarkSeenTrigger string

const (
	MarkSeenTriggerHuman     MarkSeenTrigger = "human"
	MarkSeenTriggerDelivery  MarkSeenTrigger = "delivery"
	MarkSeenTriggerAgentTool MarkSeenTrigger = "agent_tool"
)

// MarkSeenCommand asks the service to advance the cursor.
type MarkSeenCommand struct {
	UserID            conversation.IdentityRef
	ConversationID    conversation.ConversationID
	LastSeenMessageID conversation.MessageID
	Actor             observability.Actor
	// Trigger 标注来源（空 → 视为 human）。透传进 conversation.read_state.changed payload。
	Trigger MarkSeenTrigger
}
```

- [ ] **Step 3: emit payload 写入 trigger 键**

在 `MarkSeen` 的 `s.sink.Emit(...)` 的 `Payload` map（`:137-142`）加一行：

```go
			Payload: map[string]any{
				"conversation_id":               string(cmd.ConversationID),
				"user_id":                       string(cmd.UserID),
				"last_seen_message_id":          string(cmd.LastSeenMessageID),
				"previous_last_seen_message_id": string(previousMsgID),
				"trigger":                       string(cmd.triggerOrDefault()),
			},
```

并在文件内 `MarkSeenCommand` 之后加 helper：

```go
// triggerOrDefault 返回标注来源，空值落 human（保守：ack projector 只白名单 agent_tool）。
func (c MarkSeenCommand) triggerOrDefault() MarkSeenTrigger {
	if c.Trigger == "" {
		return MarkSeenTriggerHuman
	}
	return c.Trigger
}
```

- [ ] **Step 4: 三个调用点各自标注来源**

`internal/admin/api/environment_agent.go:181`（PUSH/delivery 路径，worker daemon `ReportMarkSeen` 落点）的 `MarkSeenCommand{...}` 加 `Trigger: convservice.MarkSeenTriggerDelivery,`。

`internal/admin/api/agent_tools_inbox.go:148`（PULL，agent 工具）的 `MarkSeenCommand{...}` 加 `Trigger: convservice.MarkSeenTriggerAgentTool,`。

`internal/webconsole/api/handlers_unread_conversations.go:221`（人类 web）的 `MarkSeenCommand{...}` 加 `Trigger: convservice.MarkSeenTriggerHuman,`。

> 注意：`agent_tools_inbox.go` 已 import `convservice "...conversation/service"`（它已用 `convservice.MarkSeenCommand`）。其余两文件同理已有该 import（都用了 `convservice.MarkSeenCommand`）。

- [ ] **Step 5: 写失败测试 — emit payload 带正确 trigger**

新建 `internal/conversation/service/read_state_trigger_test.go`。参照同目录现有 read_state 测试的 fixture 搭建方式（in-memory sqlite + repos + `observability.EventSink`）。断言 `MarkSeen` 成功 bump 后，最近一条 outbox 事件 payload 的 `trigger` == 入参；空 Trigger → `"human"`：

```go
func TestMarkSeen_EmitsTrigger(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   convservice.MarkSeenTrigger
		want string
	}{
		{"agent_tool", convservice.MarkSeenTriggerAgentTool, "agent_tool"},
		{"delivery", convservice.MarkSeenTriggerDelivery, "delivery"},
		{"empty_defaults_human", "", "human"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newReadStateHarness(t) // builds svc + seeds one message msgID in convID for userRef
			_, err := h.svc.MarkSeen(context.Background(), convservice.MarkSeenCommand{
				UserID: h.userRef, ConversationID: h.convID, LastSeenMessageID: h.msgID,
				Actor: observability.Actor(h.userRef), Trigger: tc.in,
			})
			if err != nil {
				t.Fatalf("MarkSeen: %v", err)
			}
			got := h.lastEventTriggerField(t) // unmarshal newest outbox event payload, read ["trigger"]
			if got != tc.want {
				t.Fatalf("trigger = %q, want %q", got, tc.want)
			}
		})
	}
}
```

> `newReadStateHarness` / `lastEventTriggerField`：复刻同目录现有 read_state 测试里已有的 sqlite + sink 搭建（读 `read_state` 相关 `*_test.go` 的 setup helper，按相同方式构造；若已有等价 helper 直接复用）。

- [ ] **Step 6: 运行测试确认失败（trigger 键尚未写入前应失败 / 未编译）**

Run: `go test ./internal/conversation/service/ -run TestMarkSeen_EmitsTrigger -v`
Expected: FAIL（trigger 字段未实现时编译失败或断言不符）。

- [ ] **Step 7: 跑全量后端测试**

Run: `go test ./...`
Expected: PASS（注意 Step 4 的三处调用点改完后全绿；已有 read_state 测试不应回归）。

- [ ] **Step 8: Commit**

```bash
git add internal/agent/activity_event.go internal/conversation/service/read_state.go \
  internal/admin/api/environment_agent.go internal/admin/api/agent_tools_inbox.go \
  internal/webconsole/api/handlers_unread_conversations.go \
  internal/conversation/service/read_state_trigger_test.go
git commit -m "feat(conversation): tag MarkSeen with Trigger + add message activity event types"
```

---

## Task 2: worker daemon 在 converse() 注入后埋 `message_delivered`

**Files:**
- Modify: `internal/workerdaemon/agent_controller.go:927-950`（`converse()`）
- Test: `internal/workerdaemon/agent_controller_test.go`

**Interfaces:**
- Consumes: `feedbackReporter.ReportAgentActivity(ctx, agentID, eventType, payloadJSON, taskRef, interactionRef string, at time.Time) error`（adminclient.go:489），`agent.EventTypeMessageDelivered`（Task 1）。
- Produces: 每次成功 `converse()` 注入后 best-effort 发一条 `message_delivered` activity。

- [ ] **Step 1: 写失败测试 — converse 注入后发 message_delivered**

`recordingReporter`（`agent_controller_test.go:58`）已记录 `ReportAgentActivity` 调用。参照该文件现有 converse 测试，新增：注入成功后，recorder 收到一条 `eventType == "message_delivered"` 且 payload 含 `conversation_id`/`message_id`/`content_preview` 的调用。

```go
func TestConverse_EmitsMessageDelivered(t *testing.T) {
	c, rec := newConverseHarness(t) // running agent session + recordingReporter (mirror existing converse tests)
	err := c.converse(context.Background(), conversePayload{
		AgentID: "ag1", ConversationID: "conv1", MessageID: "msg1",
		SenderRef: "user:u1", SenderDisplay: "Alice", MessageText: "hello there",
	})
	if err != nil {
		t.Fatalf("converse: %v", err)
	}
	ev, ok := rec.findActivity("message_delivered") // helper: scan recorded ReportAgentActivity calls
	if !ok {
		t.Fatal("expected a message_delivered activity event")
	}
	if !strings.Contains(ev.payload, `"message_id":"msg1"`) ||
		!strings.Contains(ev.payload, `"content_preview":"hello there"`) {
		t.Fatalf("payload missing fields: %s", ev.payload)
	}
}
```

> 若 `recordingReporter` 尚无 `findActivity` 风格的访问器，按该 struct 现有字段（它已存储 activity 调用）加一个最小 helper，或直接遍历其记录切片。

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/workerdaemon/ -run TestConverse_EmitsMessageDelivered -v`
Expected: FAIL（"expected a message_delivered activity event"）。

- [ ] **Step 3: 加 payload helper**

在 `agent_controller.go`（`buildConverseBrief` 附近）加：

```go
// messageDeliveredPayload 渲染 message_delivered activity 的 JSON payload（消息真正
// 注入 agent 上下文那一刻的快照）。content_preview 截断到 200 字符，避免 activity 膨胀。
func messageDeliveredPayload(pl conversePayload) string {
	preview := pl.MessageText
	if len(preview) > 200 {
		preview = preview[:200]
	}
	b, err := json.Marshal(map[string]any{
		"conversation_id":   pl.ConversationID,
		"message_id":        pl.MessageID,
		"sender_ref":        pl.SenderRef,
		"sender_display":    pl.SenderDisplay,
		"content_preview":   preview,
		"attachments_count": pl.AttachmentCount,
	})
	if err != nil {
		return "{}"
	}
	return string(b)
}
```

> `json` 已在 `agent_controller.go` import（onEvent 用 `json.Marshal`）。

- [ ] **Step 4: 在 converse() 注入成功后埋点**

在 `converse()` 的 `c.recordWake(pl.AgentID, pl.MessageID)`（`:930`）之后插入（best-effort，用 `context.Background()` 与 onEvent 一致，避免 converse ctx 取消导致漏记）：

```go
	c.recordWake(pl.AgentID, pl.MessageID)

	// message_delivered (docs/design/features/agent-message-consumption-activity.md):
	// 消息已真正注入 agent 上下文 → 埋一条一等 activity，让排障时能分清「没收到」与
	// 「收到了卡在处理中」。best-effort：失败仅记日志，绝不影响注入。
	if err := c.cfg.Reporter.ReportAgentActivity(
		context.Background(), pl.AgentID, agent.EventTypeMessageDelivered,
		messageDeliveredPayload(pl), "" /*taskRef*/, "" /*interactionRef*/, time.Now(),
	); err != nil {
		c.log("converse agent=%s message_delivered report: %v", pl.AgentID, err)
	}
```

> 确认 `agent_controller.go` 已 import agent 包：onEvent 用了 `activityEventType`/类型常量则已有；若未 import，加 `"github.com/oopslink/agent-center/internal/agent"`（或沿用文件内已用的别名）。检查文件顶部 import 块后再决定。

- [ ] **Step 5: 运行测试确认通过**

Run: `go test ./internal/workerdaemon/ -run TestConverse_EmitsMessageDelivered -v`
Expected: PASS

- [ ] **Step 6: 全量后端测试**

Run: `go test ./...`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/workerdaemon/agent_controller.go internal/workerdaemon/agent_controller_test.go
git commit -m "feat(workerdaemon): emit message_delivered activity on converse inject"
```

---

## Task 3: ack projector — 订阅 read_state.changed，仅 agent_tool 出 message_acknowledged

**Files:**
- Create: `internal/environment/service/message_ack_projector.go`
- Test: `internal/environment/service/message_ack_projector_test.go`

**Interfaces:**
- Consumes: `outbox.Event`（`{ID, EventType, Refs, Payload}`，outbox.go:29），`agent.ActivityEventRepository.Append`（repository.go），`agent.NewActivityEvent(NewActivityEventInput)`，`outbox.AppliedStore`，`idgen.Generator.NewULID()`，`clock.Clock`，`persistence.RunInTx`。
- Produces: `MessageAckProjector`，`NewMessageAckProjector(db *sql.DB, activity agent.ActivityEventRepository, applied outbox.AppliedStore, gen idgen.Generator, clk clock.Clock) *MessageAckProjector`，`Name() string == "conv-agent-msg-ack"`。

- [ ] **Step 1: 写失败测试 — 仅 agent_tool 出 ack；delivery/human 跳过；非 agent 跳过**

新建 `internal/environment/service/message_ack_projector_test.go`。用 in-memory sqlite + `agentsql.NewActivityEventRepo(db)` + 真 `AppliedRepo`（参照 `participant_projector_test.go` / `wake_projector_test.go` 的 sqlite 搭建），构造 `read_state.changed` 事件并断言 Append 行为：

```go
func TestMessageAckProjector(t *testing.T) {
	cases := []struct {
		name    string
		trigger string
		userID  string
		wantApp bool // 是否应 Append 一条 message_acknowledged
	}{
		{"agent_tool_agent_user", "agent_tool", "agent:ag1", true},
		{"delivery_skipped", "delivery", "agent:ag1", false},
		{"human_skipped", "human", "user:u1", false},
		{"agent_tool_but_human_user", "agent_tool", "user:u1", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newAckHarness(t) // db + activityRepo + appliedRepo + idgen + fixed clock
			ev := outbox.Event{
				ID:        "evt-" + tc.name,
				EventType: "conversation.read_state.changed",
				Payload: mustJSON(map[string]any{
					"conversation_id":               "conv1",
					"user_id":                       tc.userID,
					"last_seen_message_id":          "msg9",
					"previous_last_seen_message_id": "msg5",
					"trigger":                       tc.trigger,
				}),
			}
			if err := h.proj.Project(context.Background(), ev); err != nil {
				t.Fatalf("Project: %v", err)
			}
			got := h.activityCount(t, "ag1", "message_acknowledged") // ListByAgent → count event_type
			if tc.wantApp && got != 1 {
				t.Fatalf("want 1 ack, got %d", got)
			}
			if !tc.wantApp && got != 0 {
				t.Fatalf("want 0 ack, got %d", got)
			}
		})
	}
}

func TestMessageAckProjector_Idempotent(t *testing.T) {
	h := newAckHarness(t)
	ev := outbox.Event{ID: "evt-1", EventType: "conversation.read_state.changed",
		Payload: mustJSON(map[string]any{"conversation_id": "c1", "user_id": "agent:ag1",
			"last_seen_message_id": "m9", "previous_last_seen_message_id": "m5", "trigger": "agent_tool"})}
	_ = h.proj.Project(context.Background(), ev)
	_ = h.proj.Project(context.Background(), ev) // replay
	if got := h.activityCount(t, "ag1", "message_acknowledged"); got != 1 {
		t.Fatalf("replay should be no-op; got %d acks", got)
	}
}

func TestMessageAckProjector_IgnoresUnrelatedEvent(t *testing.T) {
	h := newAckHarness(t)
	ev := outbox.Event{ID: "evt-x", EventType: "conversation.message_added", Payload: "{}"}
	if err := h.proj.Project(context.Background(), ev); err != nil {
		t.Fatalf("Project: %v", err)
	}
	if got := h.activityCount(t, "ag1", "message_acknowledged"); got != 0 {
		t.Fatalf("unrelated event must be no-op; got %d", got)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/environment/service/ -run TestMessageAckProjector -v`
Expected: FAIL（`MessageAckProjector` 未定义）。

- [ ] **Step 3: 实现 projector**

新建 `internal/environment/service/message_ack_projector.go`：

```go
package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"

	"github.com/oopslink/agent-center/internal/agent"
	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/outbox"
	"github.com/oopslink/agent-center/internal/persistence"
)

// evtReadStateChanged is the conversation BC event this projector subscribes to.
// (Mirror of the literal emitted in conversation/service/read_state.go MarkSeen.)
const evtReadStateChanged = "conversation.read_state.changed"

// markSeenTriggerAgentTool is the only trigger that yields a message_acknowledged
// activity — i.e. an agent DELIBERATELY confirmed a message it pulled via
// get_my_unread (PULL path). PUSH-path auto-advance (trigger=delivery) and human
// reads (trigger=human) are skipped: the delivery moment is already shown by the
// worker daemon's message_delivered event, so an auto-ack would be redundant noise.
const markSeenTriggerAgentTool = "agent_tool"

// MessageAckProjector turns an agent's deliberate mark_seen (conversation
// read_state.changed, trigger=agent_tool) into a message_acknowledged entry in
// that agent's append-only activity stream — closing the PULL-path "agent
// confirmed it read this" loop in the operator-facing timeline. It performs the
// Append AND records AppliedStore.MarkApplied in ONE tx so redelivery is a true
// no-op (the event id is the idempotency key).
type MessageAckProjector struct {
	db       *sql.DB
	activity agent.ActivityEventRepository
	applied  outbox.AppliedStore
	idgen    idgen.Generator
	clock    clock.Clock
}

// NewMessageAckProjector constructs the projector.
func NewMessageAckProjector(db *sql.DB, activity agent.ActivityEventRepository, applied outbox.AppliedStore, gen idgen.Generator, clk clock.Clock) *MessageAckProjector {
	if clk == nil {
		clk = clock.SystemClock{}
	}
	return &MessageAckProjector{db: db, activity: activity, applied: applied, idgen: gen, clock: clk}
}

// Name is the AppliedStore key.
func (p *MessageAckProjector) Name() string { return "conv-agent-msg-ack" }

type readStateChangedPayload struct {
	ConversationID  string `json:"conversation_id"`
	UserID          string `json:"user_id"`
	LastSeenMsgID   string `json:"last_seen_message_id"`
	PreviousMsgID   string `json:"previous_last_seen_message_id"`
	Trigger         string `json:"trigger"`
}

// Project appends a message_acknowledged activity for an agent's deliberate
// mark_seen. Any non-matching event/trigger/user is a no-op.
func (p *MessageAckProjector) Project(ctx context.Context, e outbox.Event) error {
	if e.EventType != evtReadStateChanged {
		return nil
	}
	var pl readStateChangedPayload
	if err := json.Unmarshal([]byte(e.Payload), &pl); err != nil {
		return err
	}
	// Whitelist: only an agent's DELIBERATE mark_seen produces an ack event.
	if pl.Trigger != markSeenTriggerAgentTool {
		return nil
	}
	if !strings.HasPrefix(pl.UserID, "agent:") {
		return nil
	}
	agentID := agent.AgentID(strings.TrimPrefix(pl.UserID, "agent:"))
	now := p.clock.Now()

	payload, err := json.Marshal(map[string]any{
		"conversation_id":               pl.ConversationID,
		"message_id":                    pl.LastSeenMsgID,
		"previous_last_seen_message_id": pl.PreviousMsgID,
	})
	if err != nil {
		return err
	}

	return persistence.RunInTx(ctx, p.db, func(txCtx context.Context) error {
		if done, err := p.applied.IsApplied(txCtx, p.Name(), e.ID); err != nil {
			return err
		} else if done {
			return nil
		}
		ev, err := agent.NewActivityEvent(agent.NewActivityEventInput{
			ID:         p.idgen.NewULID(),
			AgentID:    agentID,
			EventType:  agent.EventTypeMessageAcknowledged,
			Payload:    string(payload),
			OccurredAt: now,
		})
		if err != nil {
			return err
		}
		if err := p.activity.Append(txCtx, ev); err != nil {
			return err
		}
		return p.applied.MarkApplied(txCtx, p.Name(), e.ID, now)
	})
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `go test ./internal/environment/service/ -run TestMessageAckProjector -v`
Expected: PASS（4 个子用例 + 幂等 + 无关事件）。

- [ ] **Step 5: 全量后端测试**

Run: `go test ./...`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/environment/service/message_ack_projector.go internal/environment/service/message_ack_projector_test.go
git commit -m "feat(environment): ack projector emits message_acknowledged for agent mark_seen"
```

---

## Task 4: 注册 ack projector 进 outboxProjectors()

**Files:**
- Modify: `internal/cli/webconsole_wiring.go:381-389`（projector 返回列表）

**Interfaces:**
- Consumes: `envservice.NewMessageAckProjector`（Task 3），`a.DB`、`a.AgentActivityRepo`（app.go:119）、`appliedRepo`、`a.IDGen`、`a.Clock`。

- [ ] **Step 1: 构造并注册 projector**

在 `outboxProjectors()` 内（紧邻 `dispatchWakeProj` 等构造处之后、`return` 之前）加：

```go
	// message_acknowledged activity (docs/design/features/agent-message-consumption-activity.md):
	// agent 主动 mark_seen（PULL）→ 在其 activity 流追加一条 ack，闭合「agent 确认已读」回路。
	msgAckProj := envservice.NewMessageAckProjector(a.DB, a.AgentActivityRepo, appliedRepo, a.IDGen, a.Clock)
```

并把它加入返回切片（`:381-389`）：

```go
	return []outbox.Projector{
		participantProj,
		planParticipantProj,
		taskInputConvProj,
		agentControlProj,
		wakeProj,
		planOrchestratorProj,
		dispatchWakeProj,
		msgAckProj,
	}, wakeProj
```

- [ ] **Step 2: 编译 + 全量后端测试**

Run: `go build ./... && go test ./...`
Expected: PASS（`a.AgentActivityRepo` 在 app.go:489 已 wired，非 nil）。

- [ ] **Step 3: Commit**

```bash
git add internal/cli/webconsole_wiring.go
git commit -m "feat(cli): register message-ack projector in outbox relay"
```

---

## Task 5: 前端渲染 message_delivered / message_acknowledged

**Files:**
- Modify: `web/src/components/AgentActivityRow.tsx:11-25`（类目）, `:35-54`（categoryOf）, `:177-225`（preview）, 图标区（`:114-143`）
- Test: `web/src/components/AgentActivityRow.test.tsx`, `web/src/components/agentActivityGrouping.test.tsx`

**Interfaces:**
- Consumes: `AgentActivityEvent`（`event_type` + `payload` string，types.ts 已通用，无需改 DTO）。
- 既有 `isCheckingEvent` 依赖 `categoryOf(...) === CAT_CHECKING`；新类型映射到非-CHECKING 类目 → **自动不被折叠**，无需改 `agentActivityGrouping.ts`。

- [ ] **Step 1: 写失败测试 — 渲染 + 不折叠**

在 `AgentActivityRow.test.tsx` 加：渲染 `message_delivered` 行，断言出现 "Received" 标签与 `content_preview` 文本：

```tsx
it('renders message_delivered as a Received row with sender + preview', () => {
  render(
    <AgentActivityRow
      event={{
        id: 'a1', agent_id: 'ag1', event_type: 'message_delivered',
        occurred_at: '2026-06-27T00:00:00Z',
        payload: JSON.stringify({
          conversation_id: 'c1', message_id: 'm1',
          sender_display: 'Alice', content_preview: 'hello there',
        }),
      }}
    />,
  );
  expect(screen.getByText(/Received/i)).toBeInTheDocument();
  expect(screen.getByText(/hello there/)).toBeInTheDocument();
});

it('renders message_acknowledged as an Acknowledged row', () => {
  render(
    <AgentActivityRow
      event={{
        id: 'a2', agent_id: 'ag1', event_type: 'message_acknowledged',
        occurred_at: '2026-06-27T00:00:00Z',
        payload: JSON.stringify({ conversation_id: 'c1', message_id: 'm9' }),
      }}
    />,
  );
  expect(screen.getByText(/Acknowledged/i)).toBeInTheDocument();
});
```

在 `agentActivityGrouping.test.tsx` 加：`message_delivered` 夹在两个 checking 事件之间时不被并入 checking-group：

```tsx
it('does not fold message_delivered into a checking group', () => {
  const ev = (id: string, t: string): AgentActivityEvent => ({
    id, agent_id: 'ag1', event_type: t, occurred_at: '2026-06-27T00:00:00Z', payload: '{}',
  });
  const items = groupActivity([ev('1', 'system_init'), ev('2', 'message_delivered'), ev('3', 'rate_limit')]);
  // delivered must surface as its own 'event' item, not swallowed by a checking-group.
  expect(items.some((i) => i.kind === 'event' && i.event.event_type === 'message_delivered')).toBe(true);
});
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd web && pnpm test -- AgentActivityRow agentActivityGrouping`
Expected: FAIL（"Received"/"Acknowledged" 文本找不到）。

- [ ] **Step 3: 加两个类目**

在 `AgentActivityRow.tsx` 的类目定义区（`CAT_CONTROL` 之后，`:25`）加：

```tsx
// message-consumption activity (docs/design/features/agent-message-consumption-activity.md):
// Received = 入站消息真正进入 agent 上下文（message_delivered，排障主信号，绝不折叠进 Checking）；
// Acknowledged = agent 主动确认已读（message_acknowledged，PULL 路径点缀）。
const CAT_DELIVERED: Category = { key: 'delivered', label: 'Received', cls: 'text-status-teal-strong', dot: 'bg-status-teal-solid' };
const CAT_ACK: Category = { key: 'acknowledged', label: 'Acknowledged', cls: 'text-text-muted', dot: 'bg-text-muted' };
```

> 若 design tokens 无 `status-teal-*`，改用既有的 `text-status-blue-fg`/`bg-status-blue-solid` 之外、且与 CAT_CONTROL 区分的色（如 `text-status-purple-strong`/`bg-status-purple-solid`，见 `toolBadge` 已用 purple）。实现前 grep `bg-status-` 确认可用 token。

- [ ] **Step 4: categoryOf 加分支**

`categoryOf`（`:35-54`）在 `lifecycle` 之后、`default` 之前加：

```tsx
    case 'message_delivered':
      return CAT_DELIVERED;
    case 'message_acknowledged':
      return CAT_ACK;
```

- [ ] **Step 5: preview 加分支**

`preview`（`:177`）在 `switch` 内加：

```tsx
    case 'message_delivered': {
      const who = str(p.sender_display) || str(p.sender_ref);
      const body = str(p.content_preview);
      return [who, truncate(body, 100)].filter(Boolean).join(': ');
    }
    case 'message_acknowledged':
      return 'read confirmed';
```

- [ ] **Step 6: （可选）信封图标**

类目用彩色圆点 + 文字已足够区分（与既有 thinking/checking 一致，无图标）。tool 类才走 `toolBadge` 图标路径；`message_delivered`/`message_acknowledged` 不属于 tool，`toolBadge` 返回 null → 走默认文字标签。**无需新增图标**（保持与 Output/Thinking 行一致）。本步无代码改动，仅确认。

- [ ] **Step 7: 运行测试确认通过**

Run: `cd web && pnpm test -- AgentActivityRow agentActivityGrouping`
Expected: PASS

- [ ] **Step 8: 全量前端测试**

Run: `cd web && pnpm test`
Expected: PASS

- [ ] **Step 9: Commit**

```bash
git add web/src/components/AgentActivityRow.tsx web/src/components/AgentActivityRow.test.tsx web/src/components/agentActivityGrouping.test.tsx
git commit -m "feat(web): render message_delivered / message_acknowledged in activity timeline"
```

---

## Self-Review

**Spec coverage（对照 `docs/design/features/agent-message-consumption-activity.md`）：**
- §2① message_delivered（worker daemon 在 sess.Inject 后埋点）→ Task 2 ✓
- §2② message_acknowledged + Trigger 区分 + projector + 注册 → Task 1（Trigger）+ Task 3（projector）+ Task 4（注册）✓
- §2③ 前端两类目、不折叠进 Checking → Task 5 ✓
- §4 不变量：append-only（Task 3 经 Append）✓；best-effort 不阻断（Task 2 仅 log）✓；幂等（Task 3 same-tx applied + 幂等测试）✓；Trigger 默认白名单（Task 1 triggerOrDefault + Task 3 仅 agent_tool）✓
- §5 测试要点：delivered 必发 / Trigger 透传 / projector 仅 agent_tool / 前端 label+不折叠 → 各 Task 的测试步骤覆盖 ✓
- §6 出范围：无 schema/迁移、不动工具对外契约（仅内部 Trigger 标注）→ 计划未触碰 migrations、未改 tool args ✓

**Placeholder scan：** 无 TBD/TODO；每个代码步骤含完整代码。测试 harness（`newReadStateHarness`/`newConverseHarness`/`newAckHarness`）明确指向"复刻同目录现有 `*_test.go` setup"——这是对既有测试脚手架的复用指引，非占位。

**Type consistency：** `MarkSeenTrigger` 常量在 Task 1 定义、Task 3 projector 用字面量 `"agent_tool"`（经 `markSeenTriggerAgentTool` 常量，注释标注为 conversation 侧的镜像，避免跨包 import 环）；`agent.EventTypeMessageDelivered`/`EventTypeMessageAcknowledged` Task 1 定义、Task 2/3 消费；`Name()=="conv-agent-msg-ack"` 唯一；`AgentActivityRepo`/`IDGen`/`Clock` 字段名对齐 app.go。

> projector 用本地常量 `markSeenTriggerAgentTool="agent_tool"` 而非 import `convservice.MarkSeenTriggerAgentTool`：避免 environment/service → conversation/service 的额外耦合（值是稳定的事件 wire 契约，注释已标注镜像来源）。若团队偏好单一来源，可改为 import convservice 常量——二选一，实现者按现有 import 拓扑决定。
