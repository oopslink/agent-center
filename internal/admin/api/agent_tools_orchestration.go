package api

import (
	"errors"
	"net/http"
	"strings"
	"time"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
	orch "github.com/oopslink/agent-center/internal/projectmanager/orchestration"
)

// =============================================================================
// Agent MCP orchestration tools (P2-T2). Thin wrappers over the orchestration
// Service so an agent can build and run orchestration graphs via MCP tools.
//
// Auth: every tool goes through requireAgentOnWorker (the b1 guardrail). The
// OrchService nil-check gates the feature (501 when not wired).
// =============================================================================

// mapOrchError translates orchestration-specific sentinel errors to the
// tool-error envelope, then defers to mapDomainError for everything else.
func mapOrchError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, orch.ErrGraphNotFound), errors.Is(err, orch.ErrNodeNotFound):
		writeError(w, http.StatusNotFound, "not_found", err.Error())
	case errors.Is(err, orch.ErrGraphExists), errors.Is(err, orch.ErrNodeExists),
		errors.Is(err, orch.ErrEdgeExists):
		writeError(w, http.StatusConflict, "already_exists", err.Error())
	case errors.Is(err, orch.ErrIllegalTransition):
		writeError(w, http.StatusConflict, "illegal_transition", err.Error())
	case errors.Is(err, orch.ErrNodeNotRemovable):
		writeError(w, http.StatusConflict, "not_removable", err.Error())
	case errors.Is(err, orch.ErrSelfEdge), errors.Is(err, orch.ErrCycleDetected):
		writeError(w, http.StatusUnprocessableEntity, "invalid_graph", err.Error())
	case errors.Is(err, orch.ErrMissingRequiredField),
		errors.Is(err, orch.ErrInvalidCategory),
		errors.Is(err, orch.ErrMissingControlKind),
		errors.Is(err, orch.ErrInvalidControlKind):
		writeError(w, http.StatusBadRequest, "validation_error", err.Error())
	default:
		mapDomainError(w, err)
	}
}

// nodeMap serializes an orchestration Node to a JSON-friendly map.
func nodeMap(n *orch.Node) map[string]any {
	m := map[string]any{
		"node_id":    string(n.ID()),
		"graph_id":   string(n.GraphID()),
		"category":   string(n.Category()),
		"title":      n.Title(),
		"status":     string(n.Status()),
		"outcome":    n.Outcome(),
		"metadata":   n.Metadata(),
		"created_at": n.CreatedAt().Format(time.RFC3339Nano),
		"updated_at": n.UpdatedAt().Format(time.RFC3339Nano),
	}
	if n.ControlKind() != "" {
		m["control_kind"] = string(n.ControlKind())
	}
	return m
}

// --- create_graph ------------------------------------------------------------

type createGraphReq struct {
	AgentID string `json:"agent_id"`
	PlanID  string `json:"plan_id"`
}

func (s *Server) createGraphHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req createGraphReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if _, ok := s.requireAgentOnWorker(w, r, d, req.AgentID); !ok {
		return
	}
	if d.OrchService == nil {
		writeError(w, http.StatusNotImplemented, "orchestration_not_wired", "")
		return
	}
	if strings.TrimSpace(req.PlanID) == "" {
		writeError(w, http.StatusBadRequest, "missing_plan_id", "")
		return
	}
	graphID, err := d.OrchService.CreateGraph(r.Context(), req.PlanID)
	if err != nil {
		mapOrchError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"graph_id": string(graphID)})
}

// --- get_graph ---------------------------------------------------------------

type getGraphReq struct {
	AgentID string `json:"agent_id"`
	GraphID string `json:"graph_id"`
}

func (s *Server) getGraphHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req getGraphReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if _, ok := s.requireAgentOnWorker(w, r, d, req.AgentID); !ok {
		return
	}
	if d.OrchService == nil {
		writeError(w, http.StatusNotImplemented, "orchestration_not_wired", "")
		return
	}
	if strings.TrimSpace(req.GraphID) == "" {
		writeError(w, http.StatusBadRequest, "missing_graph_id", "")
		return
	}
	g, err := d.OrchService.GetGraph(r.Context(), orch.GraphID(req.GraphID))
	if err != nil {
		mapOrchError(w, err)
		return
	}
	nodes := make([]map[string]any, 0, len(g.Nodes()))
	for _, n := range g.Nodes() {
		nodes = append(nodes, nodeMap(n))
	}
	edges := make([]map[string]any, 0, len(g.Edges()))
	for _, e := range g.Edges() {
		edges = append(edges, map[string]any{
			"from_node_id": string(e.FromNodeID),
			"to_node_id":   string(e.ToNodeID),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"graph_id":      string(g.ID()),
		"plan_id":       g.PlanID(),
		"status":        string(g.Status()),
		"start_node_id": string(g.StartNodeID()),
		"end_node_id":   string(g.EndNodeID()),
		"nodes":         nodes,
		"edges":         edges,
		"created_at":    g.CreatedAt().Format(time.RFC3339Nano),
		"updated_at":    g.UpdatedAt().Format(time.RFC3339Nano),
	})
}

// --- start_graph -------------------------------------------------------------

type startGraphReq struct {
	AgentID string `json:"agent_id"`
	GraphID string `json:"graph_id"`
}

func (s *Server) startGraphHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req startGraphReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if _, ok := s.requireAgentOnWorker(w, r, d, req.AgentID); !ok {
		return
	}
	if d.OrchService == nil {
		writeError(w, http.StatusNotImplemented, "orchestration_not_wired", "")
		return
	}
	if strings.TrimSpace(req.GraphID) == "" {
		writeError(w, http.StatusBadRequest, "missing_graph_id", "")
		return
	}
	if err := d.OrchService.StartGraph(r.Context(), orch.GraphID(req.GraphID)); err != nil {
		mapOrchError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// --- finish_graph ------------------------------------------------------------

type finishGraphReq struct {
	AgentID string `json:"agent_id"`
	GraphID string `json:"graph_id"`
}

func (s *Server) finishGraphHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req finishGraphReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if _, ok := s.requireAgentOnWorker(w, r, d, req.AgentID); !ok {
		return
	}
	if d.OrchService == nil {
		writeError(w, http.StatusNotImplemented, "orchestration_not_wired", "")
		return
	}
	if strings.TrimSpace(req.GraphID) == "" {
		writeError(w, http.StatusBadRequest, "missing_graph_id", "")
		return
	}
	if err := d.OrchService.FinishGraph(r.Context(), orch.GraphID(req.GraphID)); err != nil {
		mapOrchError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// --- add_graph_node ----------------------------------------------------------

type addGraphNodeReq struct {
	AgentID     string         `json:"agent_id"`
	GraphID     string         `json:"graph_id"`
	Category    string         `json:"category"`
	ControlKind string         `json:"control_kind"`
	Title       string         `json:"title"`
	Metadata    map[string]any `json:"metadata"`
}

func (s *Server) addGraphNodeHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req addGraphNodeReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if _, ok := s.requireAgentOnWorker(w, r, d, req.AgentID); !ok {
		return
	}
	if d.OrchService == nil {
		writeError(w, http.StatusNotImplemented, "orchestration_not_wired", "")
		return
	}
	if strings.TrimSpace(req.GraphID) == "" {
		writeError(w, http.StatusBadRequest, "missing_graph_id", "")
		return
	}
	if strings.TrimSpace(req.Title) == "" {
		writeError(w, http.StatusBadRequest, "missing_title", "")
		return
	}
	nodeID, err := d.OrchService.AddNode(r.Context(), orch.GraphID(req.GraphID),
		req.Category, req.ControlKind, req.Title, req.Metadata)
	if err != nil {
		mapOrchError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"node_id": string(nodeID)})
}

// --- remove_graph_node -------------------------------------------------------

type removeGraphNodeReq struct {
	AgentID string `json:"agent_id"`
	NodeID  string `json:"node_id"`
}

func (s *Server) removeGraphNodeHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req removeGraphNodeReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if _, ok := s.requireAgentOnWorker(w, r, d, req.AgentID); !ok {
		return
	}
	if d.OrchService == nil {
		writeError(w, http.StatusNotImplemented, "orchestration_not_wired", "")
		return
	}
	if strings.TrimSpace(req.NodeID) == "" {
		writeError(w, http.StatusBadRequest, "missing_node_id", "")
		return
	}
	if err := d.OrchService.RemoveNode(r.Context(), orch.NodeID(req.NodeID)); err != nil {
		mapOrchError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// --- update_graph_node -------------------------------------------------------

type updateGraphNodeReq struct {
	AgentID  string         `json:"agent_id"`
	NodeID   string         `json:"node_id"`
	Title    string         `json:"title"`
	Metadata map[string]any `json:"metadata"`
}

func (s *Server) updateGraphNodeHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req updateGraphNodeReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if _, ok := s.requireAgentOnWorker(w, r, d, req.AgentID); !ok {
		return
	}
	if d.OrchService == nil {
		writeError(w, http.StatusNotImplemented, "orchestration_not_wired", "")
		return
	}
	if strings.TrimSpace(req.NodeID) == "" {
		writeError(w, http.StatusBadRequest, "missing_node_id", "")
		return
	}
	if err := d.OrchService.UpdateNode(r.Context(), orch.NodeID(req.NodeID), req.Title, req.Metadata); err != nil {
		mapOrchError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// --- start_graph_node --------------------------------------------------------

type startGraphNodeReq struct {
	AgentID string `json:"agent_id"`
	NodeID  string `json:"node_id"`
}

func (s *Server) startGraphNodeHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req startGraphNodeReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if _, ok := s.requireAgentOnWorker(w, r, d, req.AgentID); !ok {
		return
	}
	if d.OrchService == nil {
		writeError(w, http.StatusNotImplemented, "orchestration_not_wired", "")
		return
	}
	if strings.TrimSpace(req.NodeID) == "" {
		writeError(w, http.StatusBadRequest, "missing_node_id", "")
		return
	}
	if err := d.OrchService.StartNode(r.Context(), orch.NodeID(req.NodeID)); err != nil {
		mapOrchError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// --- complete_graph_node -----------------------------------------------------

type completeGraphNodeReq struct {
	AgentID string `json:"agent_id"`
	NodeID  string `json:"node_id"`
	Outcome string `json:"outcome"`
}

func (s *Server) completeGraphNodeHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req completeGraphNodeReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if _, ok := s.requireAgentOnWorker(w, r, d, req.AgentID); !ok {
		return
	}
	if d.OrchService == nil {
		writeError(w, http.StatusNotImplemented, "orchestration_not_wired", "")
		return
	}
	if strings.TrimSpace(req.NodeID) == "" {
		writeError(w, http.StatusBadRequest, "missing_node_id", "")
		return
	}
	if err := d.OrchService.CompleteNode(r.Context(), orch.NodeID(req.NodeID), req.Outcome); err != nil {
		mapOrchError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// --- discard_graph_node ------------------------------------------------------

type discardGraphNodeReq struct {
	AgentID string `json:"agent_id"`
	NodeID  string `json:"node_id"`
}

func (s *Server) discardGraphNodeHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req discardGraphNodeReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if _, ok := s.requireAgentOnWorker(w, r, d, req.AgentID); !ok {
		return
	}
	if d.OrchService == nil {
		writeError(w, http.StatusNotImplemented, "orchestration_not_wired", "")
		return
	}
	if strings.TrimSpace(req.NodeID) == "" {
		writeError(w, http.StatusBadRequest, "missing_node_id", "")
		return
	}
	if err := d.OrchService.DiscardNode(r.Context(), orch.NodeID(req.NodeID)); err != nil {
		mapOrchError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// --- resolve_condition -------------------------------------------------------

type resolveConditionReq struct {
	AgentID string `json:"agent_id"`
	NodeID  string `json:"node_id"`
	Result  string `json:"result"`
}

func (s *Server) resolveConditionHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req resolveConditionReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if _, ok := s.requireAgentOnWorker(w, r, d, req.AgentID); !ok {
		return
	}
	if d.OrchService == nil {
		writeError(w, http.StatusNotImplemented, "orchestration_not_wired", "")
		return
	}
	if strings.TrimSpace(req.NodeID) == "" {
		writeError(w, http.StatusBadRequest, "missing_node_id", "")
		return
	}
	if err := d.OrchService.ResolveCondition(r.Context(), orch.NodeID(req.NodeID), req.Result); err != nil {
		mapOrchError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// --- add_graph_edge ----------------------------------------------------------

type addGraphEdgeReq struct {
	AgentID    string `json:"agent_id"`
	GraphID    string `json:"graph_id"`
	FromNodeID string `json:"from_node_id"`
	ToNodeID   string `json:"to_node_id"`
}

func (s *Server) addGraphEdgeHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req addGraphEdgeReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if _, ok := s.requireAgentOnWorker(w, r, d, req.AgentID); !ok {
		return
	}
	if d.OrchService == nil {
		writeError(w, http.StatusNotImplemented, "orchestration_not_wired", "")
		return
	}
	if strings.TrimSpace(req.GraphID) == "" {
		writeError(w, http.StatusBadRequest, "missing_graph_id", "")
		return
	}
	if strings.TrimSpace(req.FromNodeID) == "" || strings.TrimSpace(req.ToNodeID) == "" {
		writeError(w, http.StatusBadRequest, "missing_node_id", "both from_node_id and to_node_id are required")
		return
	}
	if err := d.OrchService.AddEdge(r.Context(), orch.GraphID(req.GraphID),
		orch.NodeID(req.FromNodeID), orch.NodeID(req.ToNodeID)); err != nil {
		mapOrchError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// --- remove_graph_edge -------------------------------------------------------

type removeGraphEdgeReq struct {
	AgentID    string `json:"agent_id"`
	GraphID    string `json:"graph_id"`
	FromNodeID string `json:"from_node_id"`
	ToNodeID   string `json:"to_node_id"`
}

func (s *Server) removeGraphEdgeHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req removeGraphEdgeReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if _, ok := s.requireAgentOnWorker(w, r, d, req.AgentID); !ok {
		return
	}
	if d.OrchService == nil {
		writeError(w, http.StatusNotImplemented, "orchestration_not_wired", "")
		return
	}
	if strings.TrimSpace(req.GraphID) == "" {
		writeError(w, http.StatusBadRequest, "missing_graph_id", "")
		return
	}
	if err := d.OrchService.RemoveEdge(r.Context(), orch.GraphID(req.GraphID),
		orch.NodeID(req.FromNodeID), orch.NodeID(req.ToNodeID)); err != nil {
		mapOrchError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// --- list_graph_nodes --------------------------------------------------------

type listGraphNodesReq struct {
	AgentID string `json:"agent_id"`
	GraphID string `json:"graph_id"`
}

func (s *Server) listGraphNodesHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req listGraphNodesReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if _, ok := s.requireAgentOnWorker(w, r, d, req.AgentID); !ok {
		return
	}
	if d.OrchService == nil {
		writeError(w, http.StatusNotImplemented, "orchestration_not_wired", "")
		return
	}
	if strings.TrimSpace(req.GraphID) == "" {
		writeError(w, http.StatusBadRequest, "missing_graph_id", "")
		return
	}
	nodes, err := d.OrchService.ListNodes(r.Context(), orch.GraphID(req.GraphID))
	if err != nil {
		mapOrchError(w, err)
		return
	}
	out := make([]map[string]any, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, nodeMap(n))
	}
	writeJSON(w, http.StatusOK, map[string]any{"nodes": out})
}

// --- get_graph_node ----------------------------------------------------------

type getGraphNodeReq struct {
	AgentID string `json:"agent_id"`
	NodeID  string `json:"node_id"`
}

func (s *Server) getGraphNodeHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req getGraphNodeReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if _, ok := s.requireAgentOnWorker(w, r, d, req.AgentID); !ok {
		return
	}
	if d.OrchService == nil {
		writeError(w, http.StatusNotImplemented, "orchestration_not_wired", "")
		return
	}
	if strings.TrimSpace(req.NodeID) == "" {
		writeError(w, http.StatusBadRequest, "missing_node_id", "")
		return
	}
	n, err := d.OrchService.GetNode(r.Context(), orch.NodeID(req.NodeID))
	if err != nil {
		mapOrchError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, nodeMap(n))
}

// --- get_ready_nodes ---------------------------------------------------------

type getReadyNodesReq struct {
	AgentID string `json:"agent_id"`
	GraphID string `json:"graph_id"`
}

func (s *Server) getReadyNodesHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req getReadyNodesReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if _, ok := s.requireAgentOnWorker(w, r, d, req.AgentID); !ok {
		return
	}
	if d.OrchService == nil {
		writeError(w, http.StatusNotImplemented, "orchestration_not_wired", "")
		return
	}
	if strings.TrimSpace(req.GraphID) == "" {
		writeError(w, http.StatusBadRequest, "missing_graph_id", "")
		return
	}
	nodes, err := d.OrchService.GetReadyNodes(r.Context(), orch.GraphID(req.GraphID))
	if err != nil {
		mapOrchError(w, err)
		return
	}
	out := make([]map[string]any, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, nodeMap(n))
	}
	writeJSON(w, http.StatusOK, map[string]any{"nodes": out})
}

// --- bind_task_to_node -------------------------------------------------------

type bindTaskToNodeReq struct {
	AgentID string `json:"agent_id"`
	NodeID  string `json:"node_id"`
	TaskID  string `json:"task_id"`
}

func (s *Server) bindTaskToNodeHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req bindTaskToNodeReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if _, ok := s.requireAgentOnWorker(w, r, d, req.AgentID); !ok {
		return
	}
	if d.OrchService == nil {
		writeError(w, http.StatusNotImplemented, "orchestration_not_wired", "")
		return
	}
	if strings.TrimSpace(req.NodeID) == "" {
		writeError(w, http.StatusBadRequest, "missing_node_id", "")
		return
	}
	if strings.TrimSpace(req.TaskID) == "" {
		writeError(w, http.StatusBadRequest, "missing_task_id", "")
		return
	}
	if err := d.OrchService.BindTask(r.Context(), orch.NodeID(req.NodeID), req.TaskID); err != nil {
		mapOrchError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// --- unbind_task_from_node ---------------------------------------------------

type unbindTaskFromNodeReq struct {
	AgentID string `json:"agent_id"`
	NodeID  string `json:"node_id"`
}

func (s *Server) unbindTaskFromNodeHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req unbindTaskFromNodeReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if _, ok := s.requireAgentOnWorker(w, r, d, req.AgentID); !ok {
		return
	}
	if d.OrchService == nil {
		writeError(w, http.StatusNotImplemented, "orchestration_not_wired", "")
		return
	}
	if strings.TrimSpace(req.NodeID) == "" {
		writeError(w, http.StatusBadRequest, "missing_node_id", "")
		return
	}
	if err := d.OrchService.UnbindTask(r.Context(), orch.NodeID(req.NodeID)); err != nil {
		mapOrchError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// =============================================================================
// Template tools: list_templates / get_template
// =============================================================================

type listTemplatesReq struct {
	AgentID string `json:"agent_id"`
}

func (s *Server) listTemplatesHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req listTemplatesReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	a, ok := s.requireAgentOnWorker(w, r, d, req.AgentID)
	if !ok {
		return
	}
	if d.TemplateRepo == nil {
		writeError(w, http.StatusNotImplemented, "templates_not_wired", "")
		return
	}
	// Resolve org from agent
	orgID := string(a.OrganizationID())
	templates, err := d.TemplateRepo.ListByOrg(r.Context(), orgID)
	if err != nil {
		mapDomainError(w, err)
		return
	}
	items := make([]map[string]any, 0, len(templates))
	for _, t := range templates {
		items = append(items, map[string]any{
			"id":          string(t.ID()),
			"name":        t.Name(),
			"description": t.Description(),
			"builtin":     t.IsBuiltin(),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"templates": items})
}

type getTemplateReq struct {
	AgentID    string `json:"agent_id"`
	TemplateID string `json:"template_id"`
}

func (s *Server) getTemplateHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req getTemplateReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if _, ok := s.requireAgentOnWorker(w, r, d, req.AgentID); !ok {
		return
	}
	if d.TemplateRepo == nil {
		writeError(w, http.StatusNotImplemented, "templates_not_wired", "")
		return
	}
	t, err := d.TemplateRepo.FindByID(r.Context(), pm.TemplateID(req.TemplateID))
	if err != nil {
		if errors.Is(err, pm.ErrTemplateNotFound) {
			writeError(w, http.StatusNotFound, "template_not_found", err.Error())
			return
		}
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":          string(t.ID()),
		"name":        t.Name(),
		"description": t.Description(),
		"content":     t.Content(),
		"builtin":     t.IsBuiltin(),
	})
}
