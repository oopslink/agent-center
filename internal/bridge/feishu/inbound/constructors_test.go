package inbound_test

import (
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/bridge/feishu/inbound"
	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/observability"
)

// TestNewRouter_MissingDepsVariants walks each individual missing-dep
// branch so the dep-validation switch in NewRouter is fully covered.
func TestNewRouter_MissingDepsVariants(t *testing.T) {
	f := newFixture(t)
	base := inbound.RouterDeps{
		Clock: f.clock, IDGen: f.idgen, Sink: f.sink,
		Dedupe: inbound.NewDedupe(0, 0, f.clock),
		Resolver: f.resolver, Parser: f.parser,
		Slash: f.slash, Card: f.card,
		DB: f.db, Convs: f.convs, MsgWriter: f.msgWriter,
		Actor: observability.Actor("system"),
	}
	cases := []func(*inbound.RouterDeps){
		func(d *inbound.RouterDeps) { d.Sink = nil },
		func(d *inbound.RouterDeps) { d.Dedupe = nil },
		func(d *inbound.RouterDeps) { d.Resolver = nil },
		func(d *inbound.RouterDeps) { d.Parser = nil },
		func(d *inbound.RouterDeps) { d.Slash = nil },
		func(d *inbound.RouterDeps) { d.Card = nil },
		func(d *inbound.RouterDeps) { d.DB = nil },
		func(d *inbound.RouterDeps) { d.IDGen = nil },
		func(d *inbound.RouterDeps) { d.Convs = nil },
		func(d *inbound.RouterDeps) { d.MsgWriter = nil },
		func(d *inbound.RouterDeps) { d.Actor = "" },
	}
	for i, mut := range cases {
		d := base
		mut(&d)
		if _, err := inbound.NewRouter(d); err == nil {
			t.Errorf("case %d: want error", i)
		}
	}
}

func TestNewSlashRouter_MissingDepsVariants(t *testing.T) {
	f := newFixture(t)
	base := inbound.SlashRouterDeps{
		DB: f.db, Clock: f.clock, IDGen: f.idgen, Sink: f.sink,
		Tasks: f.tasks, Execs: f.execs, Convs: f.convs,
		TaskSvc: f.taskSvc, IRSvc: f.irSvc, IRRepo: f.irs,
		MsgWriter: f.msgWriter, Actor: observability.Actor("system"),
	}
	cases := []func(*inbound.SlashRouterDeps){
		func(d *inbound.SlashRouterDeps) { d.DB = nil },
		func(d *inbound.SlashRouterDeps) { d.Sink = nil },
		func(d *inbound.SlashRouterDeps) { d.IDGen = nil },
		func(d *inbound.SlashRouterDeps) { d.Tasks = nil },
		func(d *inbound.SlashRouterDeps) { d.Execs = nil },
		func(d *inbound.SlashRouterDeps) { d.Convs = nil },
		func(d *inbound.SlashRouterDeps) { d.TaskSvc = nil },
		func(d *inbound.SlashRouterDeps) { d.IRSvc = nil },
		func(d *inbound.SlashRouterDeps) { d.IRRepo = nil },
		func(d *inbound.SlashRouterDeps) { d.MsgWriter = nil },
		func(d *inbound.SlashRouterDeps) { d.Actor = "" },
	}
	for i, mut := range cases {
		d := base
		mut(&d)
		if _, err := inbound.NewSlashRouter(d); err == nil {
			t.Errorf("case %d: want error", i)
		}
	}
}

func TestNewCardCallback_MissingDepsVariants(t *testing.T) {
	f := newFixture(t)
	base := inbound.CardCallbackDeps{
		Clock: f.clock, Sink: f.sink, IRRepo: f.irs, IRSvc: f.irSvc,
		Execs: f.execs, Tasks: f.tasks, MsgWriter: f.msgWriter,
		Actor: observability.Actor("system"),
	}
	cases := []func(*inbound.CardCallbackDeps){
		func(d *inbound.CardCallbackDeps) { d.Sink = nil },
		func(d *inbound.CardCallbackDeps) { d.IRRepo = nil },
		func(d *inbound.CardCallbackDeps) { d.IRSvc = nil },
		func(d *inbound.CardCallbackDeps) { d.Execs = nil },
		func(d *inbound.CardCallbackDeps) { d.Tasks = nil },
		func(d *inbound.CardCallbackDeps) { d.MsgWriter = nil },
		func(d *inbound.CardCallbackDeps) { d.Actor = "" },
	}
	for i, mut := range cases {
		d := base
		mut(&d)
		if _, err := inbound.NewCardCallback(d); err == nil {
			t.Errorf("case %d: want error", i)
		}
	}
}

func TestNewIdentityResolver_MissingDepsVariants(t *testing.T) {
	f := newFixture(t)
	base := inbound.IdentityResolverDeps{
		Bindings: f.bindings, Identities: f.identities, Registration: f.identReg,
		Sink: f.sink, Clock: f.clock,
		Channel: "feishu", Actor: observability.Actor("system"),
	}
	cases := []func(*inbound.IdentityResolverDeps){
		func(d *inbound.IdentityResolverDeps) { d.Bindings = nil },
		func(d *inbound.IdentityResolverDeps) { d.Identities = nil },
		func(d *inbound.IdentityResolverDeps) { d.Registration = nil },
		func(d *inbound.IdentityResolverDeps) { d.Sink = nil },
		func(d *inbound.IdentityResolverDeps) { d.Channel = "" },
		func(d *inbound.IdentityResolverDeps) { d.Actor = "" },
	}
	for i, mut := range cases {
		d := base
		mut(&d)
		if _, err := inbound.NewIdentityResolver(d); err == nil {
			t.Errorf("case %d: want error", i)
		}
	}
}

func TestNewIdentityResolver_DefaultClock(t *testing.T) {
	f := newFixture(t)
	r, err := inbound.NewIdentityResolver(inbound.IdentityResolverDeps{
		Bindings: f.bindings, Identities: f.identities, Registration: f.identReg,
		Sink: f.sink, Channel: "feishu", Actor: observability.Actor("system"),
		Clock: nil,
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = r
}

func TestNewDedupe_Defaults(t *testing.T) {
	d := inbound.NewDedupe(0, 0, nil)
	if d == nil {
		t.Fatal("dedupe nil")
	}
	if d.SeenBefore("foo") {
		t.Error("first SeenBefore should be false")
	}
	if !d.SeenBefore("foo") {
		t.Error("second SeenBefore should be true")
	}
}

func TestNewDedupe_FakeClock(t *testing.T) {
	c := clock.NewFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	d := inbound.NewDedupe(0, 0, c)
	_ = d.SeenBefore("a")
}
