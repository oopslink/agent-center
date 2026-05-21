package renderer

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Renderer is the pure-function vendor payload renderer. Stateless; safe
// to share across goroutines.
type Renderer struct{}

// New constructs a Renderer.
func New() *Renderer { return &Renderer{} }

// RenderMessage maps (MessageInput, optional InputRequestInput) → RenderedCard.
//
// Routing rules per bridge/01 § 6 + plan-5 § 3.6:
//
//	text                              → MessageKindText (markdown)
//	system                            → small interactive card
//	agent_finding + no input_request  → text with agent label
//	agent_finding + input_request_ref → interactive card with buttons
//	supervisor_summary                → rich interactive card + action buttons
//	conclusion_draft                  → rich interactive card + [Confirm][Edit][Drop] buttons
//	task_proposal                     → small interactive card
//
// Unknown content_kind → ErrUnknownContentKind (conventions § 17 — no
// silent fallback).
func (r *Renderer) RenderMessage(msg MessageInput, ir *InputRequestInput) (RenderedCard, error) {
	if strings.TrimSpace(msg.MessageID) == "" {
		return RenderedCard{}, errors.New("renderer: message_id required")
	}
	switch msg.ContentKind {
	case ContentKindText:
		return r.renderText(msg)
	case ContentKindSystem:
		return r.renderSystem(msg)
	case ContentKindAgentFinding:
		if msg.InputRequestRef == "" {
			return r.renderAgentFindingText(msg)
		}
		if ir == nil {
			return RenderedCard{}, ErrMissingInputRequest
		}
		return r.renderAgentFindingCard(msg, *ir)
	case ContentKindSupervisorSummary:
		return r.renderSupervisorSummary(msg)
	case ContentKindConclusionDraft:
		return r.renderConclusionDraft(msg)
	case ContentKindTaskProposal:
		return r.renderTaskProposal(msg)
	}
	return RenderedCard{}, fmt.Errorf("%w: %q", ErrUnknownContentKind, msg.ContentKind)
}

// RenderRootCard renders the kind=task / kind=issue root card.
func (r *Renderer) RenderRootCard(in RootCardInput) (RenderedCard, error) {
	switch in.Conversation.Kind {
	case "task", "issue":
		return r.renderRootCard(in)
	}
	return RenderedCard{}, fmt.Errorf("%w: kind=%q not a root-card kind",
		ErrUnknownContentKind, in.Conversation.Kind)
}

// ---- individual renderers ----

func (r *Renderer) renderText(msg MessageInput) (RenderedCard, error) {
	if msg.Content == "" {
		return RenderedCard{}, ErrEmptyContent
	}
	envelope, err := json.Marshal(map[string]string{"text": msg.Content})
	if err != nil {
		return RenderedCard{}, err
	}
	return RenderedCard{
		MessageKind:    MessageKindText,
		CardJSON:       string(envelope),
		IdempotencyKey: msg.MessageID,
	}, nil
}

func (r *Renderer) renderAgentFindingText(msg MessageInput) (RenderedCard, error) {
	if msg.Content == "" {
		return RenderedCard{}, ErrEmptyContent
	}
	label := "[agent]"
	if msg.Sender != "" {
		label = "[" + msg.Sender + "]"
	}
	envelope, _ := json.Marshal(map[string]string{
		"text": label + " " + msg.Content,
	})
	return RenderedCard{
		MessageKind:    MessageKindText,
		CardJSON:       string(envelope),
		IdempotencyKey: msg.MessageID,
	}, nil
}

func (r *Renderer) renderSystem(msg MessageInput) (RenderedCard, error) {
	card := smallCard("[system]", msg.Content)
	return jsonResult(card, MessageKindInteractive, msg.MessageID)
}

func (r *Renderer) renderAgentFindingCard(msg MessageInput, ir InputRequestInput) (RenderedCard, error) {
	// Buttons: each option + [自己写] + [取消]
	actions := []map[string]any{}
	for i, opt := range ir.Options {
		actions = append(actions, button(opt, "primary", map[string]any{
			"action":           "input_request_respond",
			"input_request_id": ir.ID,
			"option_id":        i,
			"option_text":      opt,
		}))
	}
	// fallback "自己写"
	actions = append(actions, button("自己写", "default", map[string]any{
		"action":           "input_request_respond_custom",
		"input_request_id": ir.ID,
	}))
	// fallback "取消"
	actions = append(actions, button("取消", "danger", map[string]any{
		"action":           "input_request_cancel",
		"input_request_id": ir.ID,
	}))

	headerText := "[agent] 需要您拍板"
	if ir.Question != "" {
		headerText = "[agent] " + ir.Question
	}
	card := map[string]any{
		"config": map[string]any{"wide_screen_mode": true},
		"header": map[string]any{
			"template": "blue",
			"title":    map[string]any{"tag": "plain_text", "content": headerText},
		},
		"elements": []any{
			markdownEl(msg.Content),
			actionsEl(actions),
		},
	}
	return jsonResult(card, MessageKindInteractive, msg.MessageID)
}

func (r *Renderer) renderSupervisorSummary(msg MessageInput) (RenderedCard, error) {
	card := map[string]any{
		"config": map[string]any{"wide_screen_mode": true},
		"header": map[string]any{
			"template": "indigo",
			"title":    map[string]any{"tag": "plain_text", "content": "[supervisor] 总结"},
		},
		"elements": []any{
			markdownEl(msg.Content),
			actionsEl([]map[string]any{
				button("确认", "primary", map[string]any{"action": "supervisor_summary_confirm", "message_id": msg.MessageID}),
				button("改", "default", map[string]any{"action": "supervisor_summary_change", "message_id": msg.MessageID}),
				button("不做", "danger", map[string]any{"action": "supervisor_summary_abandon", "message_id": msg.MessageID}),
			}),
		},
	}
	return jsonResult(card, MessageKindInteractive, msg.MessageID)
}

func (r *Renderer) renderConclusionDraft(msg MessageInput) (RenderedCard, error) {
	card := map[string]any{
		"config": map[string]any{"wide_screen_mode": true},
		"header": map[string]any{
			"template": "purple",
			"title":    map[string]any{"tag": "plain_text", "content": "[issue] 结论草案"},
		},
		"elements": []any{
			markdownEl(msg.Content),
			actionsEl([]map[string]any{
				button("确认结论", "primary", map[string]any{"action": "conclusion_confirm", "message_id": msg.MessageID}),
				button("改后确认", "default", map[string]any{"action": "conclusion_edit", "message_id": msg.MessageID}),
				button("不做", "danger", map[string]any{"action": "conclusion_drop", "message_id": msg.MessageID}),
			}),
		},
	}
	return jsonResult(card, MessageKindInteractive, msg.MessageID)
}

func (r *Renderer) renderTaskProposal(msg MessageInput) (RenderedCard, error) {
	card := smallCard("[task_proposal] 草案", msg.Content)
	return jsonResult(card, MessageKindInteractive, msg.MessageID)
}

func (r *Renderer) renderRootCard(in RootCardInput) (RenderedCard, error) {
	subject := in.SubjectRef
	if subject == "" {
		subject = in.Conversation.Kind + " " + in.Conversation.ConversationID
	}
	title := in.Conversation.Title
	if title == "" {
		title = subject
	}
	template := "green"
	if in.Conversation.Kind == "issue" {
		template = "purple"
	}
	card := map[string]any{
		"config": map[string]any{"wide_screen_mode": true},
		"header": map[string]any{
			"template": template,
			"title":    map[string]any{"tag": "plain_text", "content": subject},
		},
		"elements": []any{
			markdownEl("**" + title + "**"),
			noteEl("agent-center · " + subject),
		},
	}
	return jsonResult(card, MessageKindInteractive, in.Conversation.ConversationID)
}

// ---- small helpers ----

func smallCard(headerText, body string) map[string]any {
	return map[string]any{
		"config": map[string]any{"wide_screen_mode": true},
		"header": map[string]any{
			"template": "grey",
			"title":    map[string]any{"tag": "plain_text", "content": headerText},
		},
		"elements": []any{
			markdownEl(body),
		},
	}
}

func markdownEl(content string) map[string]any {
	return map[string]any{
		"tag": "div",
		"text": map[string]any{
			"tag":     "lark_md",
			"content": content,
		},
	}
}

func noteEl(content string) map[string]any {
	return map[string]any{
		"tag": "note",
		"elements": []any{
			map[string]any{"tag": "plain_text", "content": content},
		},
	}
}

func actionsEl(actions []map[string]any) map[string]any {
	return map[string]any{
		"tag":     "action",
		"actions": actions,
	}
}

func button(text, style string, value map[string]any) map[string]any {
	return map[string]any{
		"tag":   "button",
		"type":  style,
		"text":  map[string]any{"tag": "plain_text", "content": text},
		"value": value,
	}
}

func jsonResult(card any, kind MessageKind, key string) (RenderedCard, error) {
	b, err := json.Marshal(card)
	if err != nil {
		return RenderedCard{}, err
	}
	return RenderedCard{
		MessageKind:    kind,
		CardJSON:       string(b),
		IdempotencyKey: key,
	}, nil
}
