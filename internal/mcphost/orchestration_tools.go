// orchestration_tools.go — MCP handler factories for the orchestration engine
// tools (P2-T2). Every handler follows the b3-i pattern EXACTLY: a typed args
// struct with NO agent_id field, a handler that injects the process-fixed
// agent_id from cfg.AgentID and forwards via callAdmin to the matching
// /admin/agent-tools/<tool> endpoint.
package mcphost

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// --- create_graph ------------------------------------------------------------

type createGraphArgs struct {
	PlanID string `json:"plan_id" jsonschema:"The plan ID to create a graph for"`
}

func makeCreateGraph(cfg Config) mcp.ToolHandlerFor[createGraphArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args createGraphArgs) (*mcp.CallToolResult, any, error) {
		body := map[string]any{
			"agent_id": cfg.AgentID,
			"plan_id":  args.PlanID,
		}
		return callAdmin(ctx, cfg, "create_graph", body)
	}
}

// --- get_graph ---------------------------------------------------------------

type getGraphArgs struct {
	GraphID string `json:"graph_id" jsonschema:"The graph ID to retrieve"`
}

func makeGetGraph(cfg Config) mcp.ToolHandlerFor[getGraphArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args getGraphArgs) (*mcp.CallToolResult, any, error) {
		body := map[string]any{
			"agent_id": cfg.AgentID,
			"graph_id": args.GraphID,
		}
		return callAdmin(ctx, cfg, "get_graph", body)
	}
}

// --- start_graph -------------------------------------------------------------

type startGraphArgs struct {
	GraphID string `json:"graph_id" jsonschema:"The graph ID to start"`
}

func makeStartGraph(cfg Config) mcp.ToolHandlerFor[startGraphArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args startGraphArgs) (*mcp.CallToolResult, any, error) {
		body := map[string]any{
			"agent_id": cfg.AgentID,
			"graph_id": args.GraphID,
		}
		return callAdmin(ctx, cfg, "start_graph", body)
	}
}

// --- finish_graph ------------------------------------------------------------

type finishGraphArgs struct {
	GraphID string `json:"graph_id" jsonschema:"The graph ID to finish"`
}

func makeFinishGraph(cfg Config) mcp.ToolHandlerFor[finishGraphArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args finishGraphArgs) (*mcp.CallToolResult, any, error) {
		body := map[string]any{
			"agent_id": cfg.AgentID,
			"graph_id": args.GraphID,
		}
		return callAdmin(ctx, cfg, "finish_graph", body)
	}
}

// --- add_graph_node ----------------------------------------------------------

type addGraphNodeArgs struct {
	GraphID     string         `json:"graph_id" jsonschema:"The graph to add the node to"`
	Category    string         `json:"category" jsonschema:"Node category: business or control"`
	ControlKind string         `json:"control_kind,omitempty" jsonschema:"Control node sub-type (start/end/condition); required for control nodes"`
	Title       string         `json:"title" jsonschema:"Node title"`
	Metadata    map[string]any `json:"metadata,omitempty" jsonschema:"Optional metadata key-value pairs"`
}

func makeAddGraphNode(cfg Config) mcp.ToolHandlerFor[addGraphNodeArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args addGraphNodeArgs) (*mcp.CallToolResult, any, error) {
		body := map[string]any{
			"agent_id": cfg.AgentID,
			"graph_id": args.GraphID,
			"category": args.Category,
			"title":    args.Title,
		}
		if args.ControlKind != "" {
			body["control_kind"] = args.ControlKind
		}
		if len(args.Metadata) > 0 {
			body["metadata"] = args.Metadata
		}
		return callAdmin(ctx, cfg, "add_graph_node", body)
	}
}

// --- remove_graph_node -------------------------------------------------------

type removeGraphNodeArgs struct {
	NodeID string `json:"node_id" jsonschema:"The node ID to remove"`
}

func makeRemoveGraphNode(cfg Config) mcp.ToolHandlerFor[removeGraphNodeArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args removeGraphNodeArgs) (*mcp.CallToolResult, any, error) {
		body := map[string]any{
			"agent_id": cfg.AgentID,
			"node_id":  args.NodeID,
		}
		return callAdmin(ctx, cfg, "remove_graph_node", body)
	}
}

// --- update_graph_node -------------------------------------------------------

type updateGraphNodeArgs struct {
	NodeID   string         `json:"node_id" jsonschema:"The node ID to update"`
	Title    string         `json:"title,omitempty" jsonschema:"New title (omit to keep current)"`
	Metadata map[string]any `json:"metadata,omitempty" jsonschema:"New metadata (omit to keep current)"`
}

func makeUpdateGraphNode(cfg Config) mcp.ToolHandlerFor[updateGraphNodeArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args updateGraphNodeArgs) (*mcp.CallToolResult, any, error) {
		body := map[string]any{
			"agent_id": cfg.AgentID,
			"node_id":  args.NodeID,
		}
		if args.Title != "" {
			body["title"] = args.Title
		}
		if len(args.Metadata) > 0 {
			body["metadata"] = args.Metadata
		}
		return callAdmin(ctx, cfg, "update_graph_node", body)
	}
}

// --- start_graph_node --------------------------------------------------------

type startGraphNodeArgs struct {
	NodeID string `json:"node_id" jsonschema:"The node ID to start"`
}

func makeStartGraphNode(cfg Config) mcp.ToolHandlerFor[startGraphNodeArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args startGraphNodeArgs) (*mcp.CallToolResult, any, error) {
		body := map[string]any{
			"agent_id": cfg.AgentID,
			"node_id":  args.NodeID,
		}
		return callAdmin(ctx, cfg, "start_graph_node", body)
	}
}

// --- complete_graph_node -----------------------------------------------------

type completeGraphNodeArgs struct {
	NodeID  string `json:"node_id" jsonschema:"The node ID to complete"`
	Outcome string `json:"outcome" jsonschema:"The outcome of the node (e.g. pass/reject/done)"`
}

func makeCompleteGraphNode(cfg Config) mcp.ToolHandlerFor[completeGraphNodeArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args completeGraphNodeArgs) (*mcp.CallToolResult, any, error) {
		body := map[string]any{
			"agent_id": cfg.AgentID,
			"node_id":  args.NodeID,
			"outcome":  args.Outcome,
		}
		return callAdmin(ctx, cfg, "complete_graph_node", body)
	}
}

// --- discard_graph_node ------------------------------------------------------

type discardGraphNodeArgs struct {
	NodeID string `json:"node_id" jsonschema:"The node ID to discard"`
}

func makeDiscardGraphNode(cfg Config) mcp.ToolHandlerFor[discardGraphNodeArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args discardGraphNodeArgs) (*mcp.CallToolResult, any, error) {
		body := map[string]any{
			"agent_id": cfg.AgentID,
			"node_id":  args.NodeID,
		}
		return callAdmin(ctx, cfg, "discard_graph_node", body)
	}
}

// --- resolve_condition -------------------------------------------------------

type resolveConditionArgs struct {
	NodeID string `json:"node_id" jsonschema:"The condition node ID to resolve"`
	Result string `json:"result" jsonschema:"Condition result: success or failure"`
}

func makeResolveCondition(cfg Config) mcp.ToolHandlerFor[resolveConditionArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args resolveConditionArgs) (*mcp.CallToolResult, any, error) {
		body := map[string]any{
			"agent_id": cfg.AgentID,
			"node_id":  args.NodeID,
			"result":   args.Result,
		}
		return callAdmin(ctx, cfg, "resolve_condition", body)
	}
}

// --- add_graph_edge ----------------------------------------------------------

type addGraphEdgeArgs struct {
	GraphID    string `json:"graph_id" jsonschema:"The graph containing the edge"`
	FromNodeID string `json:"from_node_id" jsonschema:"Source node ID (upstream dependency)"`
	ToNodeID   string `json:"to_node_id" jsonschema:"Target node ID (downstream dependent)"`
}

func makeAddGraphEdge(cfg Config) mcp.ToolHandlerFor[addGraphEdgeArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args addGraphEdgeArgs) (*mcp.CallToolResult, any, error) {
		body := map[string]any{
			"agent_id":     cfg.AgentID,
			"graph_id":     args.GraphID,
			"from_node_id": args.FromNodeID,
			"to_node_id":   args.ToNodeID,
		}
		return callAdmin(ctx, cfg, "add_graph_edge", body)
	}
}

// --- remove_graph_edge -------------------------------------------------------

type removeGraphEdgeArgs struct {
	GraphID    string `json:"graph_id" jsonschema:"The graph containing the edge"`
	FromNodeID string `json:"from_node_id" jsonschema:"Source node ID"`
	ToNodeID   string `json:"to_node_id" jsonschema:"Target node ID"`
}

func makeRemoveGraphEdge(cfg Config) mcp.ToolHandlerFor[removeGraphEdgeArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args removeGraphEdgeArgs) (*mcp.CallToolResult, any, error) {
		body := map[string]any{
			"agent_id":     cfg.AgentID,
			"graph_id":     args.GraphID,
			"from_node_id": args.FromNodeID,
			"to_node_id":   args.ToNodeID,
		}
		return callAdmin(ctx, cfg, "remove_graph_edge", body)
	}
}

// --- list_graph_nodes --------------------------------------------------------

type listGraphNodesArgs struct {
	GraphID string `json:"graph_id" jsonschema:"The graph whose nodes to list"`
}

func makeListGraphNodes(cfg Config) mcp.ToolHandlerFor[listGraphNodesArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args listGraphNodesArgs) (*mcp.CallToolResult, any, error) {
		body := map[string]any{
			"agent_id": cfg.AgentID,
			"graph_id": args.GraphID,
		}
		return callAdmin(ctx, cfg, "list_graph_nodes", body)
	}
}

// --- get_graph_node ----------------------------------------------------------

type getGraphNodeArgs struct {
	NodeID string `json:"node_id" jsonschema:"The node ID to retrieve"`
}

func makeGetGraphNode(cfg Config) mcp.ToolHandlerFor[getGraphNodeArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args getGraphNodeArgs) (*mcp.CallToolResult, any, error) {
		body := map[string]any{
			"agent_id": cfg.AgentID,
			"node_id":  args.NodeID,
		}
		return callAdmin(ctx, cfg, "get_graph_node", body)
	}
}

// --- get_ready_nodes ---------------------------------------------------------

type getReadyNodesArgs struct {
	GraphID string `json:"graph_id" jsonschema:"The graph whose ready nodes to return"`
}

func makeGetReadyNodes(cfg Config) mcp.ToolHandlerFor[getReadyNodesArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args getReadyNodesArgs) (*mcp.CallToolResult, any, error) {
		body := map[string]any{
			"agent_id": cfg.AgentID,
			"graph_id": args.GraphID,
		}
		return callAdmin(ctx, cfg, "get_ready_nodes", body)
	}
}

// --- bind_task_to_node -------------------------------------------------------

type bindTaskToNodeArgs struct {
	NodeID string `json:"node_id" jsonschema:"The node ID to bind the task to"`
	TaskID string `json:"task_id" jsonschema:"The task ID to bind"`
}

func makeBindTaskToNode(cfg Config) mcp.ToolHandlerFor[bindTaskToNodeArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args bindTaskToNodeArgs) (*mcp.CallToolResult, any, error) {
		body := map[string]any{
			"agent_id": cfg.AgentID,
			"node_id":  args.NodeID,
			"task_id":  args.TaskID,
		}
		return callAdmin(ctx, cfg, "bind_task_to_node", body)
	}
}

// --- unbind_task_from_node ---------------------------------------------------

type unbindTaskFromNodeArgs struct {
	NodeID string `json:"node_id" jsonschema:"The node ID to unbind the task from"`
}

func makeUnbindTaskFromNode(cfg Config) mcp.ToolHandlerFor[unbindTaskFromNodeArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args unbindTaskFromNodeArgs) (*mcp.CallToolResult, any, error) {
		body := map[string]any{
			"agent_id": cfg.AgentID,
			"node_id":  args.NodeID,
		}
		return callAdmin(ctx, cfg, "unbind_task_from_node", body)
	}
}

// --- list_templates ----------------------------------------------------------

type listTemplatesArgs struct{}

func makeListTemplates(cfg Config) mcp.ToolHandlerFor[listTemplatesArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args listTemplatesArgs) (*mcp.CallToolResult, any, error) {
		body := map[string]any{"agent_id": cfg.AgentID}
		return callAdmin(ctx, cfg, "list_templates", body)
	}
}

// --- get_template ------------------------------------------------------------

type getTemplateArgs struct {
	TemplateID string `json:"template_id" jsonschema:"The template ID to retrieve"`
}

func makeGetTemplate(cfg Config) mcp.ToolHandlerFor[getTemplateArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args getTemplateArgs) (*mcp.CallToolResult, any, error) {
		body := map[string]any{
			"agent_id":    cfg.AgentID,
			"template_id": args.TemplateID,
		}
		return callAdmin(ctx, cfg, "get_template", body)
	}
}

// --- create_template ---------------------------------------------------------

type createTemplateArgs struct {
	Name        string `json:"name" jsonschema:"The template name"`
	Description string `json:"description" jsonschema:"Short description of the template"`
	Content     string `json:"content" jsonschema:"The full markdown content describing the orchestration rules"`
}

func makeCreateTemplate(cfg Config) mcp.ToolHandlerFor[createTemplateArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args createTemplateArgs) (*mcp.CallToolResult, any, error) {
		body := map[string]any{
			"agent_id":    cfg.AgentID,
			"name":        args.Name,
			"description": args.Description,
			"content":     args.Content,
		}
		return callAdmin(ctx, cfg, "create_template", body)
	}
}

// --- update_template ---------------------------------------------------------

type updateTemplateArgs struct {
	TemplateID  string `json:"template_id" jsonschema:"The template ID to update"`
	Name        string `json:"name" jsonschema:"The new template name"`
	Description string `json:"description" jsonschema:"The new description"`
	Content     string `json:"content" jsonschema:"The new full markdown content"`
}

func makeUpdateTemplate(cfg Config) mcp.ToolHandlerFor[updateTemplateArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args updateTemplateArgs) (*mcp.CallToolResult, any, error) {
		body := map[string]any{
			"agent_id":    cfg.AgentID,
			"template_id": args.TemplateID,
			"name":        args.Name,
			"description": args.Description,
			"content":     args.Content,
		}
		return callAdmin(ctx, cfg, "update_template", body)
	}
}

// --- delete_template ---------------------------------------------------------

type deleteTemplateArgs struct {
	TemplateID string `json:"template_id" jsonschema:"The template ID to delete"`
}

func makeDeleteTemplate(cfg Config) mcp.ToolHandlerFor[deleteTemplateArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args deleteTemplateArgs) (*mcp.CallToolResult, any, error) {
		body := map[string]any{
			"agent_id":    cfg.AgentID,
			"template_id": args.TemplateID,
		}
		return callAdmin(ctx, cfg, "delete_template", body)
	}
}
