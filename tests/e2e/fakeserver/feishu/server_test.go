package feishu

import (
	"testing"

	"github.com/oopslink/agent-center/internal/bridge/feishu/inbound"
)

func TestServer_InjectAndDrain(t *testing.T) {
	s := New()
	defer s.Close()
	ev := inbound.VendorEvent{Kind: inbound.VendorEventMessageReceive,
		VendorMsgRef: "r-1", VendorThreadKey: "t-1", VendorUserID: "u-1",
		Context: inbound.MessageContextDM, Text: "hi"}
	s.Inject(ev)
	got := <-s.Inbox()
	if got.VendorMsgRef != "r-1" {
		t.Errorf("ref: %s", got.VendorMsgRef)
	}
}

func TestServer_OutboundRecord(t *testing.T) {
	s := New()
	defer s.Close()
	s.RecordOutbound(OutboundRecord{Content: "hi"})
	s.RecordOutbound(OutboundRecord{Content: "hi 2"})
	if len(s.Outbound()) != 2 {
		t.Errorf("outbound count: %d", len(s.Outbound()))
	}
}

func TestServer_CloseIsIdempotent(t *testing.T) {
	s := New()
	s.Close()
	s.Close()
	// Inject after close is a no-op (no panic).
	s.Inject(inbound.VendorEvent{})
}

func TestServer_InboxChannel(t *testing.T) {
	s := New()
	defer s.Close()
	if s.Inbox() == nil {
		t.Fatal("inbox nil")
	}
}
