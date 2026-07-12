package team

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// template_io.go implements the import / export path (design §6): JSON files for
// backup and cross-org sharing. Export of a template REQUIRES it to have passed
// manual curation (design §9 — curation is load-bearing, cross-org export must
// be clean). Import re-homes the artifact into a new org and mints a fresh id,
// so the same file can seed many orgs.

// templateExportFormat is the on-disk schema version of an exported template.
const templateExportFormat = "team-template/v1"

// exportEnvelope is the JSON wire shape. A struct (not the aggregate) decouples
// the file format from the in-memory type and pins field order/versioning.
type exportEnvelope struct {
	Format              string           `json:"format"`
	Name                string           `json:"name"`
	Description         string           `json:"description,omitempty"`
	Roles               []roleSlotWire   `json:"roles"`
	WorkflowTemplateRef string           `json:"workflow_template_ref,omitempty"`
	Experiences         []experienceWire `json:"experiences,omitempty"`
	SourceOrgID         string           `json:"source_org_id,omitempty"`
	SourceID            string           `json:"source_id,omitempty"`
	ExportedAt          time.Time        `json:"exported_at"`
}

type roleSlotWire struct {
	Role           string   `json:"role"`
	CLI            string   `json:"cli,omitempty"`
	Model          string   `json:"model,omitempty"`
	CapabilityTags []string `json:"capability_tags,omitempty"`
	MaxConcurrency int      `json:"max_concurrency,omitempty"`
	Count          int      `json:"count,omitempty"`
}

type experienceWire struct {
	Slug        string   `json:"slug"`
	Title       string   `json:"title,omitempty"`
	Description string   `json:"description,omitempty"`
	Body        string   `json:"body,omitempty"`
	Scope       string   `json:"scope"`
	Tags        []string `json:"tags,omitempty"`
}

// ExportTemplate serialises a template to a shareable JSON document. It refuses
// an un-curated template (design §9): cross-org export must be human-reviewed.
func ExportTemplate(t *TeamTemplate) ([]byte, error) {
	if t == nil {
		return nil, ErrInvalidTemplate
	}
	if !t.Curated {
		return nil, ErrTemplateNotCurated
	}
	env := exportEnvelope{
		Format:              templateExportFormat,
		Name:                t.Name,
		Description:         t.Description,
		WorkflowTemplateRef: t.WorkflowTemplateRef,
		SourceOrgID:         t.OrgID,
		SourceID:            t.ID,
		ExportedAt:          time.Now().UTC(),
	}
	for _, sl := range t.Roles {
		env.Roles = append(env.Roles, roleSlotWire{
			Role:           sl.Config.Role,
			CLI:            sl.Config.CLI,
			Model:          sl.Config.Model,
			CapabilityTags: sl.Config.CapabilityTags,
			MaxConcurrency: sl.Config.MaxConcurrency,
			Count:          sl.Count,
		})
	}
	for _, e := range t.Experiences {
		env.Experiences = append(env.Experiences, experienceWire{
			Slug:        e.Slug,
			Title:       e.Title,
			Description: e.Description,
			Body:        e.Body,
			Scope:       string(e.Scope),
			Tags:        e.Tags,
		})
	}
	return json.MarshalIndent(env, "", "  ")
}

// ImportTemplateInput re-homes an imported template into a destination org with
// a freshly minted id.
type ImportTemplateInput struct {
	OrgID string
	NewID string
	Now   time.Time
}

// ImportTemplate parses an exported JSON document and re-homes it into in.OrgID
// with a new id. It rejects an unknown format and drops any non-portable
// (project-scope) experience defensively — a cross-org import must never carry
// project-specific facts. The imported template lands Curated=false: the
// destination org re-reviews before re-export.
func ImportTemplate(data []byte, in ImportTemplateInput) (*TeamTemplate, error) {
	var env exportEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidTemplate, err)
	}
	if env.Format != templateExportFormat {
		return nil, fmt.Errorf("%w: unsupported format %q", ErrInvalidTemplate, env.Format)
	}
	roles := make([]RoleSlot, 0, len(env.Roles))
	for _, rw := range env.Roles {
		roles = append(roles, RoleSlot{
			Config: RoleConfig{
				Role:           rw.Role,
				CLI:            rw.CLI,
				Model:          rw.Model,
				CapabilityTags: rw.CapabilityTags,
				MaxConcurrency: rw.MaxConcurrency,
			},
			Count: rw.Count,
		})
	}
	exps := make([]Experience, 0, len(env.Experiences))
	for _, ew := range env.Experiences {
		scope := ExperienceScope(ew.Scope)
		if !scope.Portable() {
			continue // defensive: never import project-scope facts cross-org
		}
		exps = append(exps, Experience{
			Slug:        ew.Slug,
			Title:       ew.Title,
			Description: ew.Description,
			Body:        ew.Body,
			Scope:       scope,
			Tags:        ew.Tags,
		})
	}
	name := strings.TrimSpace(env.Name)
	return NewTemplate(NewTemplateInput{
		ID:                  in.NewID,
		OrgID:               in.OrgID,
		Name:                name,
		Description:         env.Description,
		Roles:               roles,
		WorkflowTemplateRef: env.WorkflowTemplateRef,
		Experiences:         exps,
		Curated:             false,
		CreatedAt:           in.Now,
	})
}
