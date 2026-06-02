package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/persistence"
	"github.com/oopslink/agent-center/internal/workforce"
)

// ReportCapabilitiesCommand carries a worker's freshly-probed capability list
// (v2.7 #147). The worker daemon runs ProbeAllAdapters on every online and
// uploads the result here so newly-installed CLIs are auto-discovered without
// a manual --capabilities flag.
type ReportCapabilitiesCommand struct {
	WorkerID      workforce.WorkerID
	Capabilities  []workforce.Capability
	ActorIdentity observability.Actor
}

// ReportCapabilitiesResult reports the post-merge worker version.
type ReportCapabilitiesResult struct {
	WorkerID   workforce.WorkerID
	NewVersion int
	EventID    observability.EventID
}

// ReportCapabilities applies the worker's probed capability list, MERGING onto
// the stored set (repo.UpdateCapabilities preserves the user-controlled Enabled
// flag for already-known CLIs — §-1: disabled→re-online→still disabled). Newly
// detected CLIs default Enabled=true; checked-but-not-installed CLIs are stored
// Detected=false / Enabled=false (complete表态, not auto-enabled). Emits
// workforce.worker.capabilities.reported.
func (s *WorkerEnrollService) ReportCapabilities(ctx context.Context, cmd ReportCapabilitiesCommand) (ReportCapabilitiesResult, error) {
	if err := cmd.ActorIdentity.Validate(); err != nil {
		return ReportCapabilitiesResult{}, fmt.Errorf("report_capabilities: %w", err)
	}
	if string(cmd.WorkerID) == "" {
		return ReportCapabilitiesResult{}, errors.New("workforce: report capabilities worker_id required")
	}
	var resp ReportCapabilitiesResult
	resp.WorkerID = cmd.WorkerID
	err := persistence.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		w, err := s.repo.FindByID(txCtx, cmd.WorkerID)
		if err != nil {
			return err
		}
		if len(cmd.Capabilities) > 0 {
			if err := s.repo.UpdateCapabilities(txCtx, w.ID(), cmd.Capabilities, w.Version()); err != nil {
				return err
			}
		}
		evID, err := s.sink.Emit(txCtx, observability.EmitCommand{
			EventType: "workforce.worker.capabilities.reported",
			Refs:      observability.EventRefs{WorkerID: string(w.ID())},
			Actor:     cmd.ActorIdentity,
			Payload: map[string]any{
				"worker_id":    string(w.ID()),
				"capabilities": cmd.Capabilities,
			},
		})
		if err != nil {
			return err
		}
		resp.EventID = evID
		// Re-read for the post-merge version (UpdateCapabilities bumps it).
		w2, err := s.repo.FindByID(txCtx, w.ID())
		if err != nil {
			return err
		}
		resp.NewVersion = w2.Version()
		return nil
	})
	if err != nil {
		return ReportCapabilitiesResult{}, err
	}
	return resp, nil
}
