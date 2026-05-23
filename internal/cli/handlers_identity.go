package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/oopslink/agent-center/internal/conversation/identity"
)

// IdentityCommands returns the `identity` subcommand tree (v2 per ADR-0033;
// bind/unbind dropped along with ChannelBinding in P10 § 3.9).
func (a *App) IdentityCommands() []*Command {
	return []*Command{
		{
			Name:    "add",
			Summary: "Register a center identity (user / agent / system)",
			Flags:   a.identityAddHandler,
		},
		{
			Name:    "list",
			Summary: "List identities (optional --kind filter)",
			Flags:   a.identityListHandler,
		},
	}
}

func (a *App) identityAddHandler(fs *flag.FlagSet) Handler {
	kindStr := fs.String("kind", "", "kind: user|agent|system (derived from id prefix when omitted)")
	displayName := fs.String("display-name", "", "human-readable display name")
	format := fs.String("format", FormatTable, formatFlagHelp())
	return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
		if len(args) < 1 {
			return PrintError(errw, *format, "usage_error",
				"identity add <identity_id> [--kind=...] --display-name=...", ExitUsage)
		}
		id := identity.IdentityID(args[0])
		if *kindStr == "" {
			derived, err := identity.KindFromID(id)
			if err != nil {
				return PrintError(errw, *format, "usage_error",
					"--kind required (cannot derive from identity id): "+err.Error(), ExitUsage)
			}
			s := string(derived)
			kindStr = &s
		}
		kind := identity.Kind(*kindStr)
		if !kind.IsValid() {
			return PrintError(errw, *format, "usage_error",
				"--kind must be one of user|agent|system", ExitUsage)
		}
		if *displayName == "" {
			return PrintError(errw, *format, "usage_error", "--display-name required", ExitUsage)
		}
		if a.IdentityRegistration == nil {
			return PrintError(errw, *format, "internal_error",
				"identity registration service not wired", ExitNotImplemented)
		}
		res, err := a.IdentityRegistration.RegisterIdentity(ctx, identity.RegisterIdentityCommand{
			ID:          id,
			Kind:        kind,
			DisplayName: *displayName,
			Actor:       a.DefaultActor(),
		})
		if err != nil {
			return handleIdentityError(errw, *format, err)
		}
		if *format == "json" {
			b, _ := json.Marshal(identityToMap(res.Identity))
			writeOut(out, string(b))
		} else {
			fmt.Fprintf(out, "registered %s (%s) %q\n", res.Identity.ID(), res.Identity.Kind(), res.Identity.DisplayName())
		}
		return ExitOK
	}
}

func (a *App) identityListHandler(fs *flag.FlagSet) Handler {
	kindFlag := fs.String("kind", "", "filter by kind")
	format := fs.String("format", FormatTable, formatFlagHelp())
	return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
		filter := identity.IdentityFilter{}
		if *kindFlag != "" {
			k := identity.Kind(*kindFlag)
			if !k.IsValid() {
				return PrintError(errw, *format, "usage_error",
					"--kind must be one of user|agent|system", ExitUsage)
			}
			filter.Kind = &k
		}
		ids, err := a.IdentityRepo.Find(ctx, filter)
		if err != nil {
			return handleIdentityError(errw, *format, err)
		}
		if *format == "json" {
			arr := make([]map[string]any, len(ids))
			for i, x := range ids {
				arr[i] = identityToMap(x)
			}
			b, _ := json.Marshal(arr)
			writeOut(out, string(b))
		} else {
			fmt.Fprintf(out, "%-32s %-12s %s\n", "ID", "KIND", "DISPLAY NAME")
			for _, x := range ids {
				fmt.Fprintf(out, "%-32s %-12s %s\n", x.ID(), x.Kind(), x.DisplayName())
			}
		}
		return ExitOK
	}
}

func identityToMap(i *identity.Identity) map[string]any {
	return map[string]any{
		"identity_id":  string(i.ID()),
		"kind":         string(i.Kind()),
		"display_name": i.DisplayName(),
		"version":      i.Version(),
		"created_at":   i.CreatedAt().Format("2006-01-02T15:04:05Z07:00"),
	}
}

func handleIdentityError(w io.Writer, format string, err error) ExitCode {
	switch {
	case errors.Is(err, identity.ErrIdentityNotFound):
		return PrintError(w, format, "identity_not_found", err.Error(), ExitNotFound)
	case errors.Is(err, identity.ErrIdentityAlreadyExists):
		return PrintError(w, format, "identity_already_exists", err.Error(), ExitBusinessError)
	case errors.Is(err, identity.ErrIdentityVersionConflict):
		return PrintError(w, format, "identity_version_conflict", err.Error(), ExitVersionConflict)
	case errors.Is(err, identity.ErrIdentityInvalidKind):
		return PrintError(w, format, "identity_invalid_kind", err.Error(), ExitUsage)
	case errors.Is(err, identity.ErrIdentityKindImmutable):
		return PrintError(w, format, "identity_kind_immutable", err.Error(), ExitInvalidTransition)
	}
	return HandleDomainError(w, format, err)
}
