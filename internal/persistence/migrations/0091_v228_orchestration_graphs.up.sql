-- 0091_v228_orchestration_graphs.up.sql — orchestration engine (design spec 2026-07-02)
-- Generic DAG engine: Graph aggregate + Node entity + Edge + ActionLog.

CREATE TABLE IF NOT EXISTS pm_graphs (
    id         TEXT PRIMARY KEY,
    plan_id    TEXT NOT NULL DEFAULT '',
    status     TEXT NOT NULL DEFAULT 'draft',
    start_node TEXT NOT NULL,
    end_node   TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    version    INTEGER NOT NULL DEFAULT 1
);

CREATE INDEX IF NOT EXISTS idx_pm_graphs_plan_id ON pm_graphs(plan_id);

CREATE TABLE IF NOT EXISTS pm_graph_nodes (
    id           TEXT PRIMARY KEY,
    graph_id     TEXT NOT NULL REFERENCES pm_graphs(id) ON DELETE CASCADE,
    category     TEXT NOT NULL,
    control_kind TEXT NOT NULL DEFAULT '',
    title        TEXT NOT NULL,
    status       TEXT NOT NULL DEFAULT 'open',
    outcome      TEXT NOT NULL DEFAULT '',
    metadata     TEXT NOT NULL DEFAULT '{}',
    action_logs  TEXT NOT NULL DEFAULT '[]',
    created_at   TEXT NOT NULL,
    updated_at   TEXT NOT NULL,
    version      INTEGER NOT NULL DEFAULT 1
);

CREATE INDEX IF NOT EXISTS idx_pm_graph_nodes_graph_id ON pm_graph_nodes(graph_id);
CREATE INDEX IF NOT EXISTS idx_pm_graph_nodes_status ON pm_graph_nodes(graph_id, status);

CREATE TABLE IF NOT EXISTS pm_graph_edges (
    graph_id      TEXT NOT NULL REFERENCES pm_graphs(id) ON DELETE CASCADE,
    from_node_id  TEXT NOT NULL REFERENCES pm_graph_nodes(id) ON DELETE CASCADE,
    to_node_id    TEXT NOT NULL REFERENCES pm_graph_nodes(id) ON DELETE CASCADE,
    PRIMARY KEY (graph_id, from_node_id, to_node_id)
);
