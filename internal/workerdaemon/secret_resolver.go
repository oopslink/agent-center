// Package workerdaemon — secret_resolver.go: adminClientSecretResolver
// adapts the AdminClient.ResolveSecret RPC to the local agentruntime.SecretResolver
// interface that MCPInjector consumes (ADR-0027 § 7).
//
// v2.3-3b (task #29). Worker daemons construct one of these at boot
// and hand it to NewMCPInjector — the injector calls Resolve once per
// `secret:<name>` reference found in mcp_config.json before writing
// the runtime file.
package workerdaemon

import (
	"context"

	"github.com/oopslink/agent-center/internal/agentruntime"
)

// adminClientSecretResolver delegates agentruntime.SecretResolver.Resolve to the
// transport-level AdminClient. The thin wrapper lets us keep
// MCPInjector + agentruntime.SecretResolver decoupled from the HTTP shape (any
// future cross-host worker daemon transport plugs in here without
// touching the injector).
type adminClientSecretResolver struct {
	client *AdminClient
}

// NewAdminClientSecretResolver constructs the resolver. Returns nil
// when client is nil so callers can fall back to a no-op injector
// path in tests / dry-runs.
func NewAdminClientSecretResolver(c *AdminClient) agentruntime.SecretResolver {
	if c == nil {
		return nil
	}
	return adminClientSecretResolver{client: c}
}

// Resolve forwards to AdminClient.ResolveSecret. Returns the plaintext
// bytes verbatim — the caller (MCPInjector.resolveRefs) embeds them in
// the runtime JSON and writes the file at mode 0600.
func (r adminClientSecretResolver) Resolve(ctx context.Context, name string) ([]byte, error) {
	return r.client.ResolveSecret(ctx, name)
}
