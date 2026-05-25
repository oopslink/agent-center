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
		var dto IdentityDTO
		if a.Client != nil {
			res, cerr := a.Client.IdentityRegister(ctx, IdentityRegisterRequest{
				ID:          string(id),
				Kind:        string(kind),
				DisplayName: *displayName,
			})
			if cerr != nil {
				return handleIdentityClientError(errw, *format, cerr)
			}
			dto = res.Identity
		} else {
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
			dto = identityToDTO(res.Identity)
		}
		if *format == "json" {
			b, _ := json.Marshal(identityDTOToMap(dto))
			writeOut(out, string(b))
		} else {
			fmt.Fprintf(out, "registered %s (%s) %q\n", dto.ID, dto.Kind, dto.DisplayName)
		}
		return ExitOK
	}
}

func (a *App) identityListHandler(fs *flag.FlagSet) Handler {
	kindFlag := fs.String("kind", "", "filter by kind")
	format := fs.String("format", FormatTable, formatFlagHelp())
	return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
		if *kindFlag != "" {
			k := identity.Kind(*kindFlag)
			if !k.IsValid() {
				return PrintError(errw, *format, "usage_error",
					"--kind must be one of user|agent|system", ExitUsage)
			}
		}
		var dtos []IdentityDTO
		if a.Client != nil {
			var err error
			dtos, err = a.Client.IdentityFind(ctx, *kindFlag)
			if err != nil {
				return handleIdentityClientError(errw, *format, err)
			}
		} else {
			filter := identity.IdentityFilter{}
			if *kindFlag != "" {
				k := identity.Kind(*kindFlag)
				filter.Kind = &k
			}
			ids, err := a.IdentityRepo.Find(ctx, filter)
			if err != nil {
				return handleIdentityError(errw, *format, err)
			}
			dtos = identitiesToDTOs(ids)
		}
		if *format == "json" {
			arr := make([]map[string]any, len(dtos))
			for i, x := range dtos {
				arr[i] = identityDTOToMap(x)
			}
			b, _ := json.Marshal(arr)
			writeOut(out, string(b))
		} else {
			fmt.Fprintf(out, "%-32s %-12s %s\n", "ID", "KIND", "DISPLAY NAME")
			for _, x := range dtos {
				fmt.Fprintf(out, "%-32s %-12s %s\n", x.ID, x.Kind, x.DisplayName)
			}
		}
		return ExitOK
	}
}

// identityToMap is the legacy projection helper preserved for any test
// callers that still receive a domain *identity.Identity.
func identityToMap(i *identity.Identity) map[string]any {
	return identityDTOToMap(identityToDTO(i))
}

// identityDTOToMap renders an IdentityDTO into the canonical JSON map
// shape preserved by the CLI's human/json formatting contract.
func identityDTOToMap(d IdentityDTO) map[string]any {
	createdAt := d.CreatedAt
	if createdAt != "" {
		createdAt = formatTS(createdAt)
	}
	return map[string]any{
		"identity_id":  d.ID,
		"kind":         d.Kind,
		"display_name": d.DisplayName,
		"version":      d.Version,
		"created_at":   createdAt,
	}
}

func identityToDTO(i *identity.Identity) IdentityDTO {
	return IdentityDTO{
		ID:          string(i.ID()),
		Kind:        string(i.Kind()),
		DisplayName: i.DisplayName(),
		Version:     i.Version(),
		CreatedAt:   i.CreatedAt().Format("2006-01-02T15:04:05Z07:00"),
	}
}

func identitiesToDTOs(ids []*identity.Identity) []IdentityDTO {
	out := make([]IdentityDTO, len(ids))
	for i, x := range ids {
		out[i] = identityToDTO(x)
	}
	return out
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

// handleIdentityClientError mirrors handleIdentityError for the
// Client-mode path. The admin endpoint envelope codes differ from the
// domain sentinels, so we re-map them here to the same legacy reason
// strings the v1 CLI tests assert on.
func handleIdentityClientError(w io.Writer, format string, err error) ExitCode {
	if err == nil {
		return ExitOK
	}
	if errors.Is(err, ErrClientNotConfigured) || errors.Is(err, ErrServerUnreachable) {
		return PrintError(w, format, "server_unreachable",
			err.Error()+" (start the server: agent-center server)", ExitBusinessError)
	}
	var ce *ClientError
	if errors.As(err, &ce) {
		switch ce.Code {
		case "not_found":
			return PrintError(w, format, "identity_not_found", ce.Message, ExitNotFound)
		case "already_exists":
			return PrintError(w, format, "identity_already_exists", ce.Message, ExitBusinessError)
		case "version_conflict":
			return PrintError(w, format, "identity_version_conflict", ce.Message, ExitVersionConflict)
		case "invalid_input", "invalid_id":
			return PrintError(w, format, "identity_invalid_kind", ce.Message, ExitUsage)
		case "invalid_transition":
			return PrintError(w, format, "identity_kind_immutable", ce.Message, ExitInvalidTransition)
		}
	}
	return HandleClientError(w, format, err)
}
