package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"

	"github.com/oopslink/agent-center/internal/workforce"
	wfservice "github.com/oopslink/agent-center/internal/workforce/service"
)

// ProjectCommands returns the `project` subcommand tree.
func (a *App) ProjectCommands() []*Command {
	return []*Command{
		{Name: "add", Summary: "Add a project", Flags: a.projectAddHandler},
		{Name: "list", Summary: "List projects", Flags: a.projectListHandler},
		{Name: "show", Summary: "Show a project", Flags: a.projectShowHandler},
		{Name: "update", Summary: "Update a project", Flags: a.projectUpdateHandler},
		{Name: "remove", Summary: "Remove a project", Flags: a.projectRemoveHandler},
	}
}

func (a *App) projectAddHandler(fs *flag.FlagSet) Handler {
	name := fs.String("name", "", "human-readable name")
	kindStr := fs.String("kind", "", "project kind (coding|writing|investing)")
	cli := fs.String("default-agent-cli", "", "default agent CLI")
	desc := fs.String("description", "", "description")
	format := fs.String("format", FormatTable, formatFlagHelp())
	return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
		if len(args) < 1 {
			return PrintError(errw, *format, "usage_error", "project add <slug> --name=...", ExitUsage)
		}
		kind := workforce.ProjectKind(*kindStr)
		if !kind.IsValid() {
			return PrintError(errw, *format, "usage_error", "invalid --kind", ExitUsage)
		}
		nameStr := *name
		if nameStr == "" {
			nameStr = args[0]
		}
		var dto ProjectDTO
		if a.Client != nil {
			res, cerr := a.Client.ProjectAdd(ctx, ProjectAddRequest{
				ID:              args[0],
				Name:            nameStr,
				Kind:            string(kind),
				DefaultAgentCLI: *cli,
				Description:     *desc,
			})
			if cerr != nil {
				return HandleClientError(errw, *format, cerr)
			}
			dto = res.Project
		} else {
			res, err := a.ProjectSvc.Add(ctx, wfservice.AddCommand{
				ID:              workforce.ProjectID(args[0]),
				Name:            nameStr,
				Kind:            kind,
				DefaultAgentCLI: *cli,
				Description:     *desc,
				Actor:           a.DefaultActor(),
			})
			if err != nil {
				return HandleDomainError(errw, *format, err)
			}
			dto = projectToDTO(res.Project)
		}
		if *format == "json" {
			b, _ := json.Marshal(projectDTOToMap(dto))
			writeOut(out, string(b))
		} else {
			fmt.Fprintf(out, "added project %s\n", dto.ID)
		}
		return ExitOK
	}
}

func (a *App) projectListHandler(fs *flag.FlagSet) Handler {
	kindStr := fs.String("kind", "", "filter by kind")
	format := fs.String("format", FormatTable, formatFlagHelp())
	return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
		if *kindStr != "" {
			k := workforce.ProjectKind(*kindStr)
			if !k.IsValid() {
				return PrintError(errw, *format, "usage_error", "invalid --kind", ExitUsage)
			}
		}
		var dtos []ProjectDTO
		if a.Client != nil {
			var err error
			dtos, err = a.Client.ProjectFindAll(ctx, *kindStr)
			if err != nil {
				return HandleClientError(errw, *format, err)
			}
		} else {
			filter := workforce.ProjectFilter{}
			if *kindStr != "" {
				k := workforce.ProjectKind(*kindStr)
				filter.Kind = &k
			}
			projects, err := a.ProjectRepo.FindAll(ctx, filter)
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
			fmt.Fprintf(out, "%-32s %-12s %s\n", "ID", "KIND", "NAME")
			for _, p := range dtos {
				fmt.Fprintf(out, "%-32s %-12s %s\n", p.ID, p.Kind, p.Name)
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
			fmt.Fprintf(out, "Project %s\n  name: %s\n  kind: %s\n  version: %d\n",
				dto.ID, dto.Name, dto.Kind, dto.Version)
		}
		return ExitOK
	}
}

func (a *App) projectUpdateHandler(fs *flag.FlagSet) Handler {
	name := fs.String("name", "", "new name")
	kindStr := fs.String("kind", "", "new kind")
	cli := fs.String("default-agent-cli", "", "new default agent CLI")
	desc := fs.String("description", "", "new description")
	versionFlag := fs.Int("version", 0, "expected version (CAS)")
	format := fs.String("format", FormatTable, formatFlagHelp())
	return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
		if len(args) < 1 {
			return PrintError(errw, *format, "usage_error", "project update <id>", ExitUsage)
		}
		if *versionFlag <= 0 {
			return PrintError(errw, *format, "usage_error", "--version required for CAS", ExitUsage)
		}
		// Build the per-field update set.
		hasName := isFlagSet(fs, "name")
		hasKind := isFlagSet(fs, "kind")
		hasCLI := isFlagSet(fs, "default-agent-cli")
		hasDesc := isFlagSet(fs, "description")
		if !hasName && !hasKind && !hasCLI && !hasDesc {
			return PrintError(errw, *format, "usage_error", "no fields to update", ExitUsage)
		}
		if hasKind {
			k := workforce.ProjectKind(*kindStr)
			if !k.IsValid() {
				return PrintError(errw, *format, "usage_error", "invalid --kind", ExitUsage)
			}
		}
		var dto ProjectDTO
		if a.Client != nil {
			req := ProjectUpdateRequest{
				ID:      args[0],
				Version: *versionFlag,
			}
			if hasName {
				req.Name = name
			}
			if hasKind {
				k := *kindStr
				req.Kind = &k
			}
			if hasCLI {
				req.DefaultAgentCLI = cli
			}
			if hasDesc {
				req.Description = desc
			}
			res, cerr := a.Client.ProjectUpdate(ctx, req)
			if cerr != nil {
				return HandleClientError(errw, *format, cerr)
			}
			dto = res.Project
		} else {
			fields := workforce.ProjectUpdateFields{}
			if hasName {
				fields.Name = name
			}
			if hasKind {
				k := workforce.ProjectKind(*kindStr)
				fields.Kind = &k
			}
			if hasCLI {
				fields.DefaultAgentCLI = cli
			}
			if hasDesc {
				fields.Description = desc
			}
			if fields.IsEmpty() {
				return PrintError(errw, *format, "usage_error", "no fields to update", ExitUsage)
			}
			res, err := a.ProjectSvc.Update(ctx, wfservice.UpdateCommand{
				ID:      workforce.ProjectID(args[0]),
				Version: *versionFlag,
				Fields:  fields,
				Actor:   a.DefaultActor(),
			})
			if err != nil {
				return HandleDomainError(errw, *format, err)
			}
			dto = projectToDTO(res.Project)
		}
		if *format == "json" {
			b, _ := json.Marshal(projectDTOToMap(dto))
			writeOut(out, string(b))
		} else {
			fmt.Fprintf(out, "updated project %s (version=%d)\n", dto.ID, dto.Version)
		}
		return ExitOK
	}
}

func (a *App) projectRemoveHandler(fs *flag.FlagSet) Handler {
	format := fs.String("format", FormatTable, formatFlagHelp())
	return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
		if len(args) < 1 {
			return PrintError(errw, *format, "usage_error", "project remove <id>", ExitUsage)
		}
		if a.Client != nil {
			if _, cerr := a.Client.ProjectRemove(ctx, ProjectRemoveRequest{ID: args[0]}); cerr != nil {
				return HandleClientError(errw, *format, cerr)
			}
		} else {
			_, err := a.ProjectSvc.Remove(ctx, wfservice.RemoveCommand{
				ID:    workforce.ProjectID(args[0]),
				Actor: a.DefaultActor(),
			})
			if err != nil {
				return HandleDomainError(errw, *format, err)
			}
		}
		writeOut(out, fmt.Sprintf("removed project %s", args[0]))
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
	return map[string]any{
		"project_id":        p.ID,
		"name":              p.Name,
		"kind":              p.Kind,
		"default_agent_cli": p.DefaultAgentCLI,
		"description":       p.Description,
		"version":           p.Version,
	}
}

func projectToDTO(p *workforce.Project) ProjectDTO {
	return ProjectDTO{
		ID:              string(p.ID()),
		Name:            p.Name(),
		Kind:            string(p.Kind()),
		DefaultAgentCLI: p.DefaultAgentCLI(),
		Description:     p.Description(),
		Version:         p.Version(),
	}
}

func projectsToDTOs(ps []*workforce.Project) []ProjectDTO {
	out := make([]ProjectDTO, len(ps))
	for i, p := range ps {
		out[i] = projectToDTO(p)
	}
	return out
}

func isFlagSet(fs *flag.FlagSet, name string) bool {
	set := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == name {
			set = true
		}
	})
	return set
}
