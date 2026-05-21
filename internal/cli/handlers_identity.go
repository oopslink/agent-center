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

// IdentityCommands returns the `identity` subcommand tree per
// 03-cli-subcommands § 8.6.
func (a *App) IdentityCommands() []*Command {
	return []*Command{
		{
			Name:    "add",
			Summary: "Register a center identity (user / supervisor / agent / bot)",
			Flags:   a.identityAddHandler,
		},
		{
			Name:    "list",
			Summary: "List identities (optional --kind filter)",
			Flags:   a.identityListHandler,
		},
		{
			Name:    "bind",
			Summary: "Bind a channel-side vendor_user_id to an identity",
			Flags:   a.identityBindHandler,
		},
		{
			Name:    "unbind",
			Summary: "Remove all bindings for an identity on a channel",
			Flags:   a.identityUnbindHandler,
		},
	}
}

func (a *App) identityAddHandler(fs *flag.FlagSet) Handler {
	kindStr := fs.String("kind", "", "kind: user|supervisor|agent|bot")
	displayName := fs.String("display-name", "", "human-readable display name")
	format := fs.String("format", "human", "")
	return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
		if len(args) < 1 {
			return PrintError(errw, *format, "usage_error",
				"identity add <identity_id> --kind=... --display-name=...", ExitUsage)
		}
		id := identity.IdentityID(args[0])
		if *kindStr == "" {
			// derive from id prefix
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
				"--kind must be one of user|supervisor|agent|bot", ExitUsage)
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
			fmt.Fprintf(out, "registered identity %s (%s)\n", res.Identity.ID(), res.Identity.Kind())
		}
		return ExitOK
	}
}

func (a *App) identityListHandler(fs *flag.FlagSet) Handler {
	kindStr := fs.String("kind", "", "filter by kind")
	format := fs.String("format", "human", "")
	return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
		filter := identity.IdentityFilter{}
		if *kindStr != "" {
			k := identity.Kind(*kindStr)
			if !k.IsValid() {
				return PrintError(errw, *format, "usage_error",
					"--kind must be one of user|supervisor|agent|bot", ExitUsage)
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

func (a *App) identityBindHandler(fs *flag.FlagSet) Handler {
	channel := fs.String("channel", "", "vendor channel (feishu / dingtalk / ...)")
	vendorUserID := fs.String("vendor-user-id", "", "vendor side user id")
	preferred := fs.Bool("preferred", false, "mark as the preferred binding for this (identity, channel)")
	format := fs.String("format", "human", "")
	return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
		if len(args) < 1 {
			return PrintError(errw, *format, "usage_error",
				"identity bind <identity_id> --channel=... --vendor-user-id=... [--preferred]", ExitUsage)
		}
		if *channel == "" {
			return PrintError(errw, *format, "usage_error", "--channel required", ExitUsage)
		}
		if *vendorUserID == "" {
			return PrintError(errw, *format, "usage_error", "--vendor-user-id required", ExitUsage)
		}
		if a.IdentityRegistration == nil {
			return PrintError(errw, *format, "internal_error",
				"identity registration service not wired", ExitNotImplemented)
		}
		res, err := a.IdentityRegistration.BindChannel(ctx, identity.BindChannelCommand{
			IdentityID:   identity.IdentityID(args[0]),
			Channel:      identity.Channel(*channel),
			VendorUserID: *vendorUserID,
			Preferred:    *preferred,
			Actor:        a.DefaultActor(),
		})
		if err != nil {
			return handleIdentityError(errw, *format, err)
		}
		if *format == "json" {
			b, _ := json.Marshal(bindingToMap(res.Binding))
			writeOut(out, string(b))
		} else {
			fmt.Fprintf(out, "bound %s to %s:%s%s\n",
				res.Binding.IdentityID(), res.Binding.Channel(), res.Binding.VendorUserID(),
				preferredSuffix(res.Binding.Preferred()))
		}
		return ExitOK
	}
}

func (a *App) identityUnbindHandler(fs *flag.FlagSet) Handler {
	channel := fs.String("channel", "", "vendor channel to unbind")
	format := fs.String("format", "human", "")
	return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
		if len(args) < 1 {
			return PrintError(errw, *format, "usage_error",
				"identity unbind <identity_id> --channel=...", ExitUsage)
		}
		if *channel == "" {
			return PrintError(errw, *format, "usage_error", "--channel required", ExitUsage)
		}
		if a.IdentityRegistration == nil {
			return PrintError(errw, *format, "internal_error",
				"identity registration service not wired", ExitNotImplemented)
		}
		if _, err := a.IdentityRegistration.UnbindChannel(ctx, identity.UnbindChannelCommand{
			IdentityID: identity.IdentityID(args[0]),
			Channel:    identity.Channel(*channel),
			Actor:      a.DefaultActor(),
		}); err != nil {
			return handleIdentityError(errw, *format, err)
		}
		if *format == "json" {
			writeOut(out, fmt.Sprintf(`{"identity_id":%q,"channel":%q}`, args[0], *channel))
		} else {
			fmt.Fprintf(out, "unbound %s from %s\n", args[0], *channel)
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

func bindingToMap(b *identity.ChannelBinding) map[string]any {
	return map[string]any{
		"binding_id":     b.ID(),
		"identity_id":    string(b.IdentityID()),
		"channel":        string(b.Channel()),
		"vendor_user_id": b.VendorUserID(),
		"preferred":      b.Preferred(),
		"bound_at":       b.BoundAt().Format("2006-01-02T15:04:05Z07:00"),
	}
}

func preferredSuffix(p bool) string {
	if p {
		return " (preferred)"
	}
	return ""
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
	case errors.Is(err, identity.ErrChannelBindingNotFound):
		return PrintError(w, format, "channel_binding_not_found", err.Error(), ExitNotFound)
	case errors.Is(err, identity.ErrChannelBindingAlreadyExists):
		return PrintError(w, format, "channel_binding_already_exists", err.Error(), ExitBusinessError)
	case errors.Is(err, identity.ErrChannelBindingPreferredConflict):
		return PrintError(w, format, "channel_binding_preferred_conflict", err.Error(), ExitInvariantViolation)
	}
	return HandleDomainError(w, format, err)
}
