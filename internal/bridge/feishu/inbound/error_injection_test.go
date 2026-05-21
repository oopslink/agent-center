package inbound_test

import (
	"context"
	"errors"
	"testing"

	"github.com/oopslink/agent-center/internal/bridge/feishu/inbound"
	"github.com/oopslink/agent-center/internal/conversation/identity"
)

// fakeBindings injects ErrFindByVendorUser failure mode.
type fakeBindings struct {
	identity.ChannelBindingRepository
	findErr error
}

func (f *fakeBindings) FindByVendorUserID(ctx context.Context, ch identity.Channel, vu string) (*identity.ChannelBinding, error) {
	if f.findErr != nil {
		return nil, f.findErr
	}
	return f.ChannelBindingRepository.FindByVendorUserID(ctx, ch, vu)
}

// TestResolver_LookupReturnsUnexpectedError covers the path where the
// underlying binding repo returns a non-NotFound error.
func TestResolver_LookupReturnsUnexpectedError(t *testing.T) {
	f := newFixture(t)
	fb := &fakeBindings{
		ChannelBindingRepository: f.bindings,
		findErr:                  errors.New("db transient"),
	}
	r, err := inbound.NewIdentityResolver(inbound.IdentityResolverDeps{
		Bindings:     fb,
		Identities:   f.identities,
		Registration: f.identReg,
		Sink:         f.sink,
		Clock:        f.clock,
		Channel:      identity.Channel("feishu"),
		Actor:        "system",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = r.Resolve(context.Background(), "ou-x")
	if err == nil {
		t.Fatal("want error from lookup")
	}
}
