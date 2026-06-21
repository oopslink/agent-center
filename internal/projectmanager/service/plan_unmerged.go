package service

import (
	"context"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// UnmergedBoard is the F4 read model (v2.13.0 / I18): a plan together with the
// list of its `Integrate(T)` nodes that have NOT yet merged back into the
// integration trunk (un-done Integrate nodes). It bundles the full PlanDetail so
// the HTTP layer can resolve each row's title / org_ref / assignee from the same
// load (no second query), mirroring how the plan DTO is rendered. AllMerged is the
// PD's ship-gate signal: true iff there are zero unmerged Integrate nodes (the
// §2.5 集成完成 Gate is structurally open).
type UnmergedBoard struct {
	Detail   *PlanDetail
	Unmerged []pm.UnmergedIntegration
}

// AllMerged reports the ship-gate-clear condition: no Integrate node is still
// open (F1 spec §2.5/§8). The board carries it explicitly so callers don't
// re-derive `len()==0`.
func (b *UnmergedBoard) AllMerged() bool { return len(b.Unmerged) == 0 }

// ListUnmergedIntegrations derives the F4 unmerged-branch board for a plan: it
// loads the plan's DERIVED view (§9.2) and joins it with the per-node cycle
// metadata (CycleNodeMetaPort), then projects the un-done `Integrate(T)` nodes
// (pm.UnmergedIntegrations). It is the PD's ship-gate reconciliation query —
// "which feature branches are still not merged back into the trunk?".
//
// nil-safe metadata: when no CycleNodeMetaPort is wired (F2 not composed, or a
// non-scaffolded plan), the metadata map is empty and the board is empty rather
// than wrong — the projection only ever lists nodes explicitly marked
// role==integrate. A metadata-port error is PROPAGATED (fail loud) rather than
// silently yielding an empty board, so a broken join can't read as "all merged".
func (s *Service) ListUnmergedIntegrations(ctx context.Context, planID pm.PlanID) (*UnmergedBoard, error) {
	if s.plans == nil {
		return nil, ErrPlansUnavailable
	}
	detail, err := s.GetPlanDetail(ctx, planID)
	if err != nil {
		return nil, err
	}
	var meta map[pm.TaskID]pm.CycleNodeMeta
	if s.cycleMeta != nil {
		meta, err = s.cycleMeta.CycleNodeMeta(ctx, planID)
		if err != nil {
			return nil, err
		}
	}
	return &UnmergedBoard{
		Detail:   detail,
		Unmerged: pm.UnmergedIntegrations(detail.View, meta),
	}, nil
}
