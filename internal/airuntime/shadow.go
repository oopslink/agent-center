package airuntime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"
)

type ShadowCompareRequest struct {
	ObjectType string   `json:"object_type,omitempty"`
	ObjectIDs  []string `json:"object_ids,omitempty"`
}

type SnapshotEvidence struct {
	OK                   bool     `json:"ok"`
	Reason               Reason   `json:"reason,omitempty"`
	CLIKey               string   `json:"cli_key,omitempty"`
	CLIExecutable        string   `json:"cli_executable,omitempty"`
	CLIVersionConstraint string   `json:"cli_version_constraint,omitempty"`
	RequiredFeatures     []string `json:"required_features,omitempty"`
	ModelKey             string   `json:"model_key,omitempty"`
	ParametersSHA256     string   `json:"parameters_sha256,omitempty"`
}

type ShadowDifference struct {
	ObjectType string           `json:"object_type"`
	ObjectID   string           `json:"object_id"`
	Matched    bool             `json:"matched"`
	DiffType   string           `json:"diff_type"`
	Legacy     SnapshotEvidence `json:"legacy"`
	New        SnapshotEvidence `json:"new"`
	Details    map[string]any   `json:"details,omitempty"`
}

type ShadowCompareReport struct {
	RunID       string             `json:"run_id"`
	OrgID       string             `json:"org_id"`
	Compared    int                `json:"compared"`
	Matched     int                `json:"matched"`
	DiffCount   int                `json:"diff_count"`
	Differences []ShadowDifference `json:"differences"`
}

type ShadowCompareRecord struct {
	ID         string
	RunID      string
	OrgID      string
	Difference ShadowDifference
	ComparedAt time.Time
}

type ShadowCompareMutation struct {
	OrgID   string
	RunID   string
	Records []ShadowCompareRecord
}

func (s *Service) ShadowCompare(ctx context.Context, orgID string, req ShadowCompareRequest) (ShadowCompareReport, error) {
	repo, ok := s.repo.(MigrationRepository)
	if !ok {
		return ShadowCompareReport{}, runtimeError(ReasonMigrationUnavailable, "AI Runtime migration repository is not configured", nil)
	}
	objects, err := repo.ListLegacyRuntimeObjects(ctx, orgID)
	if err != nil {
		return ShadowCompareReport{}, err
	}
	objectType := strings.TrimSpace(req.ObjectType)
	if objectType == "" {
		objectType = "agent"
	}
	allow := map[string]bool{}
	for _, id := range req.ObjectIDs {
		id = strings.TrimSpace(id)
		if id != "" {
			allow[id] = true
		}
	}
	selections, err := repo.ListObjectSelections(ctx, orgID, objectType)
	if err != nil {
		return ShadowCompareReport{}, err
	}
	selectionByObject := map[string]RuntimeSelection{}
	for _, selection := range selections {
		selectionByObject[selection.ObjectID] = selection.Selection
	}
	resolver := NewRuntimeResolver(s.repo)
	resolver.now = s.now
	runID := s.id()
	report := ShadowCompareReport{RunID: runID, OrgID: orgID, Differences: []ShadowDifference{}}
	mutation := ShadowCompareMutation{OrgID: orgID, RunID: runID}
	for _, object := range objects {
		if object.ObjectType != objectType {
			continue
		}
		if len(allow) > 0 && !allow[object.ObjectID] {
			continue
		}
		report.Compared++
		legacySnapshot, legacyErr := resolveLegacyObject(ctx, resolver, orgID, object)
		newSelection, ok := selectionByObject[object.ObjectID]
		if !ok {
			newSelection = RuntimeSelection{Mode: SelectionInherit}
		}
		newSnapshot, newErr := resolver.Resolve(ctx, orgID, newSelection)
		diff := shadowDiff(object, legacySnapshot, legacyErr, newSnapshot, newErr)
		if diff.Matched {
			report.Matched++
		} else {
			report.Differences = append(report.Differences, diff)
		}
		mutation.Records = append(mutation.Records, ShadowCompareRecord{
			ID: s.id(), RunID: runID, OrgID: orgID, Difference: diff, ComparedAt: s.now(),
		})
	}
	report.DiffCount = len(report.Differences)
	sort.Slice(report.Differences, func(i, j int) bool {
		return mappingLess(MigrationMapping{ObjectType: report.Differences[i].ObjectType, ObjectID: report.Differences[i].ObjectID},
			MigrationMapping{ObjectType: report.Differences[j].ObjectType, ObjectID: report.Differences[j].ObjectID})
	})
	if err := repo.RecordShadowComparisons(ctx, mutation); err != nil {
		return report, err
	}
	return report, nil
}

func shadowDiff(object MigrationObject, legacySnapshot RuntimeSnapshot, legacyErr error, newSnapshot RuntimeSnapshot, newErr error) ShadowDifference {
	legacyEvidence := snapshotEvidence(legacySnapshot, legacyErr)
	newEvidence := snapshotEvidence(newSnapshot, newErr)
	diffType := "matched"
	matched := effectiveEvidenceEqual(legacyEvidence, newEvidence)
	if !matched {
		switch {
		case legacyErr != nil && newErr != nil:
			diffType = "error_mismatch"
		case legacyErr != nil:
			diffType = "legacy_error"
		case newErr != nil:
			diffType = "new_error"
		default:
			diffType = "snapshot_mismatch"
		}
	}
	return ShadowDifference{
		ObjectType: object.ObjectType, ObjectID: object.ObjectID,
		Matched: matched, DiffType: diffType, Legacy: legacyEvidence, New: newEvidence,
		Details: map[string]any{"legacy_cli": object.Legacy.CLI, "legacy_model": object.Legacy.Model},
	}
}

func snapshotEvidence(snapshot RuntimeSnapshot, err error) SnapshotEvidence {
	if err != nil {
		var runtimeErr *Error
		if errors.As(err, &runtimeErr) {
			return SnapshotEvidence{OK: false, Reason: runtimeErr.Reason}
		}
		return SnapshotEvidence{OK: false, Reason: ReasonSelectionInvalid}
	}
	return SnapshotEvidence{
		OK: true, CLIKey: snapshot.CLIKey, CLIExecutable: snapshot.CLIExecutable,
		CLIVersionConstraint: snapshot.CLIVersionConstraint,
		RequiredFeatures:     append([]string(nil), snapshot.RequiredFeatures...),
		ModelKey:             snapshot.ModelKey,
		ParametersSHA256:     mapDigest(snapshot.Parameters),
	}
}

func effectiveEvidenceEqual(a, b SnapshotEvidence) bool {
	if a.OK != b.OK || a.Reason != b.Reason || a.CLIKey != b.CLIKey ||
		a.CLIExecutable != b.CLIExecutable || a.CLIVersionConstraint != b.CLIVersionConstraint ||
		a.ModelKey != b.ModelKey || a.ParametersSHA256 != b.ParametersSHA256 {
		return false
	}
	if len(a.RequiredFeatures) != len(b.RequiredFeatures) {
		return false
	}
	aa, bb := append([]string(nil), a.RequiredFeatures...), append([]string(nil), b.RequiredFeatures...)
	sort.Strings(aa)
	sort.Strings(bb)
	for i := range aa {
		if aa[i] != bb[i] {
			return false
		}
	}
	return true
}

func mapDigest(m map[string]any) string {
	data, _ := json.Marshal(cloneMap(m))
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
