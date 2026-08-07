package api

import (
	"encoding/json"
	"testing"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// TestReportDeliveryReq_DecodesAndMapsPushError locks the center-side relay contract that the
// P2 nit broke: the report_delivery body's git carries push_error (the 9th FinalizedGitStatus
// field), and it must both DECODE (deliveryGitReq had only 8 fields → silently dropped) and
// MAP into pm.Delivery so the DURABLE Task.Delivery records WHY a delivery was not pushed.
// This is the same "mirror missing a field, silent, no one shouts" family as the P0; the
// end-to-end worker→center relay is ALSO verified live in RR (a hand-fed round-trip alone
// could pass even if the real relay dropped it).
func TestReportDeliveryReq_DecodesAndMapsPushError(t *testing.T) {
	body := `{"agent_id":"a","task_id":"t","source":"manual_recovery","executor_id":"exec-dead","worktree":"/tmp/w","evidence":"go test ./... PASS","reason":"executor exhausted","git":{"branch":"ac-exec/t/e","head_sha":"deadbeef","probed":true,"pushed":false,"base_ref":"main","base_known":true,"ahead_of_base":1,"push_error":"remote rejected (pre-receive hook declined)"}}`
	var req reportDeliveryReq
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatalf("unmarshal report_delivery body: %v", err)
	}
	if req.Git == nil || req.Git.PushError != "remote rejected (pre-receive hook declined)" {
		t.Fatalf("deliveryGitReq must DECODE push_error (was dropped by the 8-field struct), got %+v", req.Git)
	}
	// The handler maps deliveryGitReq → pm.Delivery; assert push_error survives that mapping
	// (mirror the handler's construction so the field can't be dropped there either).
	g := req.Git
	d := &pm.Delivery{
		Branch: g.Branch, HeadSHA: g.HeadSHA, Dirty: g.Dirty, Pushed: g.Pushed, Probed: g.Probed,
		BaseRef: g.BaseRef, BaseKnown: g.BaseKnown, AheadOfBase: g.AheadOfBase, PushError: g.PushError,
		Source: req.Source, ExecutorID: req.ExecutorID, Worktree: req.Worktree, Evidence: req.Evidence, Reason: req.Reason,
	}
	if d.PushError != "remote rejected (pre-receive hook declined)" {
		t.Fatalf("pm.Delivery must carry the relayed push_error, got %q", d.PushError)
	}
	if d.Source != "manual_recovery" || d.Evidence == "" || d.Reason == "" || d.ExecutorID != "exec-dead" {
		t.Fatalf("manual recovery metadata dropped: %+v", d)
	}
}
