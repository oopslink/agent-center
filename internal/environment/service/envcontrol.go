// Package service hosts the Environment bounded-context AppServices (v2.7 D1,
// ADR-0050). EnvControl is the worker-initiated control-channel facade riding
// the existing admin API: it owns the Worker connection lifecycle (connect /
// heartbeat / cumulative ack) and serves the replayable command stream the
// reconnecting Worker pulls from its last acked offset.
//
// This is the D1 #102 LOG layer. The real command SOURCE — the
// agent.lifecycle→command projector that turns Agent-BC intents into stream
// entries — is D2; D1 ships only the synthetic EnqueueCommand path (used by
// tests) so the log/replay/idempotency invariants can be proved end-to-end
// before the projector exists. Process control (executing commands on a host)
// is also D2.
package service

import (
	"context"
	"errors"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/environment"
	"github.com/oopslink/agent-center/internal/idgen"
)

// EnvControl is the Environment-BC control-channel AppService. It wraps the
// Worker repository (connection state + ack cursor) and the ControlLog command
// stream (offset assignment + center-side idempotency + replay).
type EnvControl struct {
	db      DBExecutor
	workers environment.WorkerRepository
	log     *environment.ControlLog
	idgen   idgen.Generator
	clock   clock.Clock
}

// DBExecutor is the slim handle EnvControl keeps for parity with the other
// AppServices (composite-tx capable callers can hand the same *sql.DB). D1's
// methods are each single-statement against one repo so they don't open a tx
// here, but we keep the handle for D2 (projector + multi-write paths).
type DBExecutor interface{}

// Deps bundles EnvControl dependencies. Mirrors the pm/agent service shape.
type Deps struct {
	DB      DBExecutor
	Workers environment.WorkerRepository
	Events  environment.ControlEventRepository
	IDGen   idgen.Generator
	Clock   clock.Clock
}

// New constructs the EnvControl service.
func New(d Deps) *EnvControl {
	clk := d.Clock
	if clk == nil {
		clk = clock.SystemClock{}
	}
	return &EnvControl{
		db:      d.DB,
		workers: d.Workers,
		log:     environment.NewControlLog(d.Events, d.IDGen, clk),
		idgen:   d.IDGen,
		clock:   clk,
	}
}

// ConnectWorker opens the control stream for a Worker: it ensures the Worker AR
// exists (creating it in the offline state on first connect, stamped with the
// provided org), marks it online, and persists. The caller reads
// LastAckedOffset() off the returned Worker to know where to replay from.
//
// orgID is org PROVENANCE stamped from the workforce.Worker the daemon enrolled
// under — it is NOT a tight Agent↔Worker map (the handler resolves it).
func (c *EnvControl) ConnectWorker(ctx context.Context, workerID environment.WorkerID, orgID string) (*environment.Worker, error) {
	now := c.clock.Now()
	w, err := c.workers.FindByID(ctx, workerID)
	if errors.Is(err, environment.ErrWorkerNotFound) {
		w, err = environment.NewWorker(environment.NewWorkerInput{
			ID:             workerID,
			OrganizationID: orgID,
			CreatedAt:      now,
		})
		if err != nil {
			return nil, err
		}
		w.Connect(now)
		if err := c.workers.Save(ctx, w); err != nil {
			return nil, err
		}
		return w, nil
	}
	if err != nil {
		return nil, err
	}
	w.Connect(now)
	if err := c.workers.Update(ctx, w); err != nil {
		return nil, err
	}
	return w, nil
}

// Heartbeat refreshes the Worker's liveness (and brings an offline Worker back
// online — the stream is evidently live).
func (c *EnvControl) Heartbeat(ctx context.Context, workerID environment.WorkerID) error {
	w, err := c.workers.FindByID(ctx, workerID)
	if err != nil {
		return err
	}
	w.Heartbeat(c.clock.Now())
	return c.workers.Update(ctx, w)
}

// AckWorker advances the Worker's cumulative ack cursor to offset (monotonic;
// stale/duplicate acks are a tolerated no-op in the AR). Returns the Worker so
// the caller can echo the resulting last_acked_offset.
func (c *EnvControl) AckWorker(ctx context.Context, workerID environment.WorkerID, offset int64) (*environment.Worker, error) {
	w, err := c.workers.FindByID(ctx, workerID)
	if err != nil {
		return nil, err
	}
	w.AckOffset(offset, c.clock.Now())
	if err := c.workers.Update(ctx, w); err != nil {
		return nil, err
	}
	return w, nil
}

// CommandsAfter returns the replay set for a Worker: commands with offset
// strictly greater than offset, ascending. Pass the Worker's LastAckedOffset to
// get exactly what a reconnecting Worker still needs.
func (c *EnvControl) CommandsAfter(ctx context.Context, workerID environment.WorkerID, offset int64) ([]*environment.WorkerControlEvent, error) {
	return c.log.CommandsAfter(ctx, workerID, offset)
}

// EnqueueCommand appends a command to a Worker's stream (idempotent on
// idempotency key). This is the SYNTHETIC enqueue path for D1 tests; in
// production the command source is the D2 agent.lifecycle→command projector,
// not this method.
func (c *EnvControl) EnqueueCommand(ctx context.Context, in environment.AppendCommandInput) (*environment.WorkerControlEvent, error) {
	return c.log.AppendCommand(ctx, in)
}
