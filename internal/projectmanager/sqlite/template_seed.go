package sqlite

import (
	"context"
	"database/sql"
	_ "embed"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

//go:embed cycle_template.md
var cycleTemplateContent string

// SeedBuiltinTemplates inserts the system's built-in templates if they don't
// already exist. Called at boot. Idempotent.
func SeedBuiltinTemplates(ctx context.Context, db *sql.DB, repo *TemplateRepo) error {
	builtins := []struct {
		id, name, desc string
		content        string
	}{
		{
			id:      "template-builtin-cycle",
			name:    "cycle",
			desc:    "Development cycle workflow: S0 → Dev → Review → Integrate → Gate → Accept → Ship",
			content: cycleTemplateContent,
		},
	}
	for _, b := range builtins {
		_, err := repo.FindByID(ctx, pm.TemplateID(b.id))
		if err == nil {
			continue // already exists
		}
		t, err := pm.NewTemplate(pm.NewTemplateInput{
			ID:          pm.TemplateID(b.id),
			OrgID:       "",   // builtin templates are org-agnostic
			Name:        b.name,
			Description: b.desc,
			Content:     b.content,
			Builtin:     true,
			CreatedBy:   "system",
		})
		if err != nil {
			return err
		}
		if err := repo.Save(ctx, t); err != nil {
			return err
		}
	}
	return nil
}
