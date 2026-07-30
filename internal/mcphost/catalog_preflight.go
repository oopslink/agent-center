package mcphost

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ListToolNames enumerates the tool names NewServer(cfg) exposes over a real MCP
// tools/list exchange. Handler dependencies may be nil because this only lists
// metadata; it never invokes a tool.
func ListToolNames(ctx context.Context, cfg Config) ([]string, error) {
	srv := NewServer(cfg)
	serverT, clientT := mcp.NewInMemoryTransports()
	ss, err := srv.Connect(ctx, serverT, nil)
	if err != nil {
		return nil, fmt.Errorf("mcphost catalog: server connect: %w", err)
	}
	defer ss.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "agent-center-mcp-preflight", Version: "0"}, nil)
	cs, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		return nil, fmt.Errorf("mcphost catalog: client connect: %w", err)
	}
	defer cs.Close()
	res, err := cs.ListTools(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("mcphost catalog: list tools: %w", err)
	}
	names := make([]string, 0, len(res.Tools))
	for _, t := range res.Tools {
		if t != nil {
			names = append(names, t.Name)
		}
	}
	sort.Strings(names)
	return names, nil
}

// RequireTools fails loud when the agent-center MCP catalog does not expose every
// required default tool. This is a runtime preflight guard against a supervisor
// continuing without the center communication surface it is required to use.
func RequireTools(ctx context.Context, cfg Config, required ...string) error {
	names, err := ListToolNames(ctx, cfg)
	if err != nil {
		return err
	}
	have := make(map[string]struct{}, len(names))
	for _, name := range names {
		have[name] = struct{}{}
	}
	var missing []string
	for _, name := range required {
		if _, ok := have[name]; !ok {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("mcphost catalog: missing required tool(s): %s (available: %s)", strings.Join(missing, ", "), strings.Join(names, ", "))
	}
	return nil
}
