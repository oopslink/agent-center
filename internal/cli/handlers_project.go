package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/oopslink/agent-center/internal/workforce"
)

// ProjectCommands returns the `project` subcommand tree.
//
// v2.7 (task #132): the OLD-model write surface (`add` / `update` /
// `remove`) is gone — project management moves to webconsole/admin/MCP
// (new pm model). The CLI keeps only read-only observability commands.
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
			projects, err := a.ProjectRepo.FindAll(ctx, workforce.ProjectFilter{})
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
			fmt.Fprintf(out, "%-20s %-32s %s\n", "ID", "NAME", "TAGS")
			for _, p := range dtos {
				fmt.Fprintf(out, "%-20s %-32s %s\n", p.ID, p.Name, strings.Join(p.Tags, ","))
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
			p, err := a.ProjectRepo.FindByID(ctx, workforce.ProjectID(args[0]))
			if err != nil {
				return HandleDomainError(errw, *format, err)
			}
			dto = projectToDTO(p)
		}
		if *format == "json" {
			b, _ := json.Marshal(projectDTOToMap(dto))
			writeOut(out, string(b))
		} else {
			fmt.Fprintf(out, "Project %s\n  name: %s\n  tags: %s\n  version: %d\n",
				dto.ID, dto.Name, strings.Join(dto.Tags, ","), dto.Version)
		}
		return ExitOK
	}
}

// projectToMap is the legacy projection helper preserved for tests
// that still produce a domain *workforce.Project.
func projectToMap(p *workforce.Project) map[string]any {
	return projectDTOToMap(projectToDTO(p))
}

// projectDTOToMap renders a ProjectDTO into the canonical JSON map
// shape preserved by the CLI's human/json formatting contract. We keep
// the legacy `project_id` key (not `id`) for backward compatibility
// with existing CLI consumers.
func projectDTOToMap(p ProjectDTO) map[string]any {
	tags := p.Tags
	if tags == nil {
		tags = []string{}
	}
	return map[string]any{
		"project_id":  p.ID,
		"name":        p.Name,
		"description": p.Description,
		"tags":        tags,
		"version":     p.Version,
	}
}

func projectToDTO(p *workforce.Project) ProjectDTO {
	return ProjectDTO{
		ID:          string(p.ID()),
		Name:        p.Name(),
		Description: p.Description(),
		Tags:        p.Tags(),
		Version:     p.Version(),
	}
}

func projectsToDTOs(ps []*workforce.Project) []ProjectDTO {
	out := make([]ProjectDTO, len(ps))
	for i, p := range ps {
		out[i] = projectToDTO(p)
	}
	return out
}

