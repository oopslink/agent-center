package projectmanager

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// ModelCatalogEntry is one org-level, user-managed model catalog entry
// (issue-93dd8daa phase ①). The catalog is the single source of truth for the
// models an org's agents may run — mirroring templates: NO built-in entries, all
// user/agent managed. `tier` is FREE TEXT (a capability/fit description the phase-②
// LLM difficulty judge matches against semantically), not an enum.
type ModelCatalogEntry struct {
	id            ModelCatalogEntryID
	orgID         string
	modelID       string // provider model id, unique within an org
	displayName   string
	inputCost     float64 // per-MTok input price (>= 0)
	outputCost    float64 // per-MTok output price (>= 0)
	contextWindow int     // max context tokens (>= 0)
	tier          string  // free-text capability/fit description
	createdBy     IdentityRef
	createdAt     time.Time
	updatedAt     time.Time
	version       int
}

// ModelCatalogEntryID is the surrogate primary key (the stable row id); model_id is
// the org-unique business key.
type ModelCatalogEntryID string

// ModelCatalogFields carries the mutable, user-supplied fields of an entry — shared
// by create/update/import so validation lives in one place.
type ModelCatalogFields struct {
	ModelID       string
	DisplayName   string
	InputCost     float64
	OutputCost    float64
	ContextWindow int
	Tier          string
}

type NewModelCatalogEntryInput struct {
	ID        ModelCatalogEntryID
	OrgID     string
	Fields    ModelCatalogFields
	CreatedBy IdentityRef
	CreatedAt time.Time
}

const (
	maxModelIDLen      = 256
	maxModelDisplayLen = 256
	maxModelTierLen    = 2048
)

var (
	ErrModelCatalogEntryExists   = errors.New("projectmanager: model catalog entry already exists (model_id must be unique in the org)")
	ErrModelCatalogEntryNotFound = errors.New("projectmanager: model catalog entry not found")
)

// NewModelCatalogEntry validates + constructs an entry (version 1).
func NewModelCatalogEntry(in NewModelCatalogEntryInput) (*ModelCatalogEntry, error) {
	if strings.TrimSpace(string(in.ID)) == "" {
		return nil, errors.New("projectmanager: model catalog entry id required")
	}
	f, err := validateModelCatalogFields(in.Fields)
	if err != nil {
		return nil, err
	}
	at := in.CreatedAt
	if at.IsZero() {
		at = time.Now()
	}
	return &ModelCatalogEntry{
		id:            in.ID,
		orgID:         in.OrgID,
		modelID:       f.ModelID,
		displayName:   f.DisplayName,
		inputCost:     f.InputCost,
		outputCost:    f.OutputCost,
		contextWindow: f.ContextWindow,
		tier:          f.Tier,
		createdBy:     in.CreatedBy,
		createdAt:     at.UTC(),
		updatedAt:     at.UTC(),
		version:       1,
	}, nil
}

type RehydrateModelCatalogEntryInput struct {
	ID            ModelCatalogEntryID
	OrgID         string
	ModelID       string
	DisplayName   string
	InputCost     float64
	OutputCost    float64
	ContextWindow int
	Tier          string
	CreatedBy     IdentityRef
	CreatedAt     time.Time
	UpdatedAt     time.Time
	Version       int
}

func RehydrateModelCatalogEntry(in RehydrateModelCatalogEntryInput) (*ModelCatalogEntry, error) {
	if in.Version < 1 {
		return nil, errors.New("projectmanager: model catalog entry version must be >= 1")
	}
	return &ModelCatalogEntry{
		id:            in.ID,
		orgID:         in.OrgID,
		modelID:       in.ModelID,
		displayName:   in.DisplayName,
		inputCost:     in.InputCost,
		outputCost:    in.OutputCost,
		contextWindow: in.ContextWindow,
		tier:          in.Tier,
		createdBy:     in.CreatedBy,
		createdAt:     in.CreatedAt.UTC(),
		updatedAt:     in.UpdatedAt.UTC(),
		version:       in.Version,
	}, nil
}

// Getters
func (m *ModelCatalogEntry) ID() ModelCatalogEntryID { return m.id }
func (m *ModelCatalogEntry) OrgID() string           { return m.orgID }
func (m *ModelCatalogEntry) ModelID() string         { return m.modelID }
func (m *ModelCatalogEntry) DisplayName() string     { return m.displayName }
func (m *ModelCatalogEntry) InputCost() float64      { return m.inputCost }
func (m *ModelCatalogEntry) OutputCost() float64     { return m.outputCost }
func (m *ModelCatalogEntry) ContextWindow() int      { return m.contextWindow }
func (m *ModelCatalogEntry) Tier() string            { return m.tier }
func (m *ModelCatalogEntry) CreatedBy() IdentityRef  { return m.createdBy }
func (m *ModelCatalogEntry) CreatedAt() time.Time    { return m.createdAt }
func (m *ModelCatalogEntry) UpdatedAt() time.Time    { return m.updatedAt }
func (m *ModelCatalogEntry) Version() int            { return m.version }

// Update mutates the entry's fields (model_id may be changed too — uniqueness is
// re-checked at the repo layer).
func (m *ModelCatalogEntry) Update(fields ModelCatalogFields, at time.Time) error {
	f, err := validateModelCatalogFields(fields)
	if err != nil {
		return err
	}
	m.modelID = f.ModelID
	m.displayName = f.DisplayName
	m.inputCost = f.InputCost
	m.outputCost = f.OutputCost
	m.contextWindow = f.ContextWindow
	m.tier = f.Tier
	m.touch(at)
	return nil
}

// validateModelCatalogFields trims + checks the user-supplied fields. Returns the
// normalized (trimmed) fields. Costs must be >= 0, context_window >= 0.
func validateModelCatalogFields(f ModelCatalogFields) (ModelCatalogFields, error) {
	out := ModelCatalogFields{
		ModelID:       strings.TrimSpace(f.ModelID),
		DisplayName:   strings.TrimSpace(f.DisplayName),
		InputCost:     f.InputCost,
		OutputCost:    f.OutputCost,
		ContextWindow: f.ContextWindow,
		Tier:          strings.TrimSpace(f.Tier),
	}
	if out.ModelID == "" {
		return out, errors.New("projectmanager: model_id required")
	}
	if len(out.ModelID) > maxModelIDLen {
		return out, fmt.Errorf("projectmanager: model_id too long (max %d bytes)", maxModelIDLen)
	}
	if out.DisplayName == "" {
		out.DisplayName = out.ModelID // sensible default
	}
	if len(out.DisplayName) > maxModelDisplayLen {
		return out, fmt.Errorf("projectmanager: display_name too long (max %d bytes)", maxModelDisplayLen)
	}
	if len(out.Tier) > maxModelTierLen {
		return out, fmt.Errorf("projectmanager: tier too long (max %d bytes)", maxModelTierLen)
	}
	if out.InputCost < 0 || out.OutputCost < 0 {
		return out, errors.New("projectmanager: input_cost/output_cost must be >= 0")
	}
	if out.ContextWindow < 0 {
		return out, errors.New("projectmanager: context_window must be >= 0")
	}
	return out, nil
}

func (m *ModelCatalogEntry) touch(at time.Time) {
	if at.IsZero() {
		at = time.Now()
	}
	m.updatedAt = at.UTC()
	m.version++
}
