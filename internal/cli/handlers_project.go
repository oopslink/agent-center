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
	format := fs.String("format", "human", "")
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
		if *format == "json" {
			b, _ := json.Marshal(projectToMap(res.Project))
			writeOut(out, string(b))
		} else {
			fmt.Fprintf(out, "added project %s\n", res.Project.ID())
		}
		return ExitOK
	}
}

func (a *App) projectListHandler(fs *flag.FlagSet) Handler {
	kindStr := fs.String("kind", "", "filter by kind")
	format := fs.String("format", "human", "")
	return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
		filter := workforce.ProjectFilter{}
		if *kindStr != "" {
			k := workforce.ProjectKind(*kindStr)
			if !k.IsValid() {
				return PrintError(errw, *format, "usage_error", "invalid --kind", ExitUsage)
			}
			filter.Kind = &k
		}
		projects, err := a.ProjectRepo.FindAll(ctx, filter)
		if err != nil {
			return HandleDomainError(errw, *format, err)
		}
		if *format == "json" {
			arr := make([]map[string]any, len(projects))
			for i, p := range projects {
				arr[i] = projectToMap(p)
			}
			b, _ := json.Marshal(arr)
			writeOut(out, string(b))
		} else {
			fmt.Fprintf(out, "%-32s %-12s %s\n", "ID", "KIND", "NAME")
			for _, p := range projects {
				fmt.Fprintf(out, "%-32s %-12s %s\n", p.ID(), p.Kind(), p.Name())
			}
		}
		return ExitOK
	}
}

func (a *App) projectShowHandler(fs *flag.FlagSet) Handler {
	format := fs.String("format", "human", "")
	return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
		if len(args) < 1 {
			return PrintError(errw, *format, "usage_error", "project show <id>", ExitUsage)
		}
		p, err := a.ProjectRepo.FindByID(ctx, workforce.ProjectID(args[0]))
		if err != nil {
			return HandleDomainError(errw, *format, err)
		}
		if *format == "json" {
			b, _ := json.Marshal(projectToMap(p))
			writeOut(out, string(b))
		} else {
			fmt.Fprintf(out, "Project %s\n  name: %s\n  kind: %s\n  version: %d\n",
				p.ID(), p.Name(), p.Kind(), p.Version())
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
	format := fs.String("format", "human", "")
	return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
		if len(args) < 1 {
			return PrintError(errw, *format, "usage_error", "project update <id>", ExitUsage)
		}
		if *versionFlag <= 0 {
			return PrintError(errw, *format, "usage_error", "--version required for CAS", ExitUsage)
		}
		fields := workforce.ProjectUpdateFields{}
		if isFlagSet(fs, "name") {
			fields.Name = name
		}
		if isFlagSet(fs, "kind") {
			k := workforce.ProjectKind(*kindStr)
			if !k.IsValid() {
				return PrintError(errw, *format, "usage_error", "invalid --kind", ExitUsage)
			}
			fields.Kind = &k
		}
		if isFlagSet(fs, "default-agent-cli") {
			fields.DefaultAgentCLI = cli
		}
		if isFlagSet(fs, "description") {
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
		if *format == "json" {
			b, _ := json.Marshal(projectToMap(res.Project))
			writeOut(out, string(b))
		} else {
			fmt.Fprintf(out, "updated project %s (version=%d)\n", res.Project.ID(), res.Project.Version())
		}
		return ExitOK
	}
}

func (a *App) projectRemoveHandler(fs *flag.FlagSet) Handler {
	format := fs.String("format", "human", "")
	return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
		if len(args) < 1 {
			return PrintError(errw, *format, "usage_error", "project remove <id>", ExitUsage)
		}
		_, err := a.ProjectSvc.Remove(ctx, wfservice.RemoveCommand{
			ID:    workforce.ProjectID(args[0]),
			Actor: a.DefaultActor(),
		})
		if err != nil {
			return HandleDomainError(errw, *format, err)
		}
		writeOut(out, fmt.Sprintf("removed project %s", args[0]))
		return ExitOK
	}
}

func projectToMap(p *workforce.Project) map[string]any {
	return map[string]any{
		"project_id":         string(p.ID()),
		"name":               p.Name(),
		"kind":               string(p.Kind()),
		"default_agent_cli":  p.DefaultAgentCLI(),
		"description":        p.Description(),
		"version":            p.Version(),
	}
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
