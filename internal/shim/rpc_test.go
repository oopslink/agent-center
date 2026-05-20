package shim

import (
	"encoding/json"
	"testing"
	"time"
)

func TestEnvelopeEncodingRoundtrip(t *testing.T) {
	hello := ShimHello{
		ProtocolVersion: ProtocolVersion,
		ExecutionID:     "E-1",
		ShimToken:       "tok",
		ShimPID:         100,
		ShimStartTime:   time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC),
		AgentPID:        200,
		AgentStartTime:  time.Date(2026, 5, 21, 12, 0, 1, 0, time.UTC),
		LastAckedSeq:    42,
	}
	data, err := EncodeEnvelope(MsgShimHello, hello)
	if err != nil {
		t.Fatal(err)
	}
	env, err := DecodeEnvelope(data)
	if err != nil {
		t.Fatal(err)
	}
	if env.Type != MsgShimHello {
		t.Fatalf("type: %s", env.Type)
	}
	var got ShimHello
	if err := json.Unmarshal(env.Payload, &got); err != nil {
		t.Fatal(err)
	}
	if got.ShimToken != "tok" || got.LastAckedSeq != 42 {
		t.Fatalf("roundtrip: %+v", got)
	}
}

func TestDecodeEnvelope_BadJSON(t *testing.T) {
	if _, err := DecodeEnvelope([]byte("not-json")); err == nil {
		t.Fatal("expected error")
	}
}

func TestProtocolVersionConstant(t *testing.T) {
	if ProtocolVersion < 1 {
		t.Fatal("must be >= 1")
	}
}
