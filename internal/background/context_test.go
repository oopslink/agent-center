package background

import (
	"context"
	"testing"
	"time"
)

func TestOperationContextRejectsAlreadyCanceledParent(t *testing.T) {
	parent, cancelParent := context.WithCancel(context.Background())
	cancelParent()

	_, cancel, ok := OperationContext(parent, time.Second)
	defer cancel()
	if ok {
		t.Fatal("OperationContext ok=true for already-canceled parent, want false")
	}
}

func TestOperationContextDetachesFromLaterParentCancellation(t *testing.T) {
	parent, cancelParent := context.WithCancel(context.Background())
	ctx, cancel, ok := OperationContext(parent, time.Second)
	defer cancel()
	if !ok {
		t.Fatal("OperationContext ok=false for live parent, want true")
	}

	cancelParent()
	select {
	case <-ctx.Done():
		t.Fatal("operation context was canceled by later parent cancellation")
	default:
	}
}

func TestOperationContextStillHasOwnDeadline(t *testing.T) {
	ctx, cancel, ok := OperationContext(context.Background(), time.Nanosecond)
	defer cancel()
	if !ok {
		t.Fatal("OperationContext ok=false for live parent, want true")
	}

	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("operation context did not expire")
	}
}
