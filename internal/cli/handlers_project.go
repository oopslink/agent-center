package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"time"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// ProjectCommands returns the `project` subcommand tree.
//
// v2.7 (task #132): the OLD-model write surface (`add` / `update` /
// `remove`) is gone — project management moves to webconsole/admin/MCP
// (new pm model). The CLI keeps only read-only observability commands.
//
// v2.7 (task #131 PR-3): the read surface is repointed from the retired
// workforce.Project model to the new pm.Project model. These are
// operator-scoped (A9-consistent) global-visible reads; the LOCAL path uses
// the operator-global pm.ProjectRepository.ListAll / FindByID.
func (a *App) ProjectCommands() []*Command {
	return []*Command{
		{Name: "list", Summary: "List projects", Flags: a.projectListHandler},
		{Name: "show", Summary: "Show a project", Flags: a.projectShowHandler},
	}
}

func (a *App) projectListHandler(fs *flag.FlagSet) Handler {
	format := fs.String("format", FormatTable, formatFlagHelp())
	return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
		var dtos []ProjectDTO
		if a.Client != nil {
			var err error
			dtos, err = a.Client.ProjectFindAll(ctx, "")
			if err != nil {
				return HandleClientError(errw, *format, err)
			}
		} else {
			projects, err := a.PMProjectRepo.ListAll(ctx)
			if err != nil {
				return HandleDomainError(errw, *format, err)
			}
			dtos = projectsToDTOs(projects)
		}
		if *format == "json" {
			arr := make([]map[string]any, len(dtos))
			for i, p := range dtos {
				arr[i] = projectDTOToMap(p)
			}
			b, _ := json.Marshal(arr)
			writeOut(out, string(b))
		} else {
			fmt.Fprintf(out, "%-20s %-32s %s\n", "ID", "NAME", "DESCRIPTION")
			for _, p := range dtos {
				fmt.Fprintf(out, "%-20s %-32s %s\n", p.ID, p.Name, p.Description)
			}
		}
		return ExitOK
	}
}

func (a *App) projectShowHandler(fs *flag.FlagSet) Handler {
	format := fs.String("format", FormatTable, formatFlagHelp())
	return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
		if len(args) < 1 {
			return PrintError(errw, *format, "usage_error", "project show <id>", ExitUsage)
		}
		var dto ProjectDTO
		if a.Client != nil {
			var err error
			dto, err = a.Client.ProjectFindByID(ctx, args[0])
			if err != nil {
				return HandleClientError(errw, *format, err)
			}
		} else {
			p, err := a.PMProjectRepo.FindByID(ctx, pm.ProjectID(args[0]))
			if err != nil {
				return HandleDomainError(errw, *format, err)
			}
			dto = projectToDTO(p)
		}
		if *format == "json" {
			b, _ := json.Marshal(projectDTOToMap(dto))
			writeOut(out, string(b))
		} else {
			fmt.Fprintf(out, "Project %s\n  name: %s\n  description: %s\n  version: %d\n",
				dto.ID, dto.Name, dto.Description, dto.Version)
		}
		return ExitOK
	}
}

// projectToMap is the projection helper preserved for tests that produce a
// domain *pm.Project.
func projectToMap(p *pm.Project) map[string]any {
	return projectDTOToMap(projectToDTO(p))
}

// projectDTOToMap renders a ProjectDTO into the canonical JSON map
// shape preserved by the CLI's human/json formatting contract. We keep
// the legacy `project_id` key (not `id`) for backward compatibility
// with existing CLI consumers.
//
// v2.7 #131 PR-3: tags dropped (pm.Project has no Tags). created_at +
// organization_id surfaced from the pm model.
func projectDTOToMap(p ProjectDTO) map[string]any {
	return map[string]any{
		"project_id":      p.ID,
		"name":            p.Name,
		"description":     p.Description,
		"organization_id": p.OrganizationID,
		"version":         p.Version,
		"created_at":      p.CreatedAt,
	}
}

func projectToDTO(p *pm.Project) ProjectDTO {
	return ProjectDTO{
		ID:             string(p.ID()),
		Name:           p.Name(),
		Description:    p.Description(),
		OrganizationID: p.OrganizationID(),
		Version:        p.Version(),
		CreatedAt:      p.CreatedAt().UTC().Format(time.RFC3339Nano),
	}
}

func projectsToDTOs(ps []*pm.Project) []ProjectDTO {
	out := make([]ProjectDTO, len(ps))
	for i, p := range ps {
		out[i] = projectToDTO(p)
	}
	return out
}
