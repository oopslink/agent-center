package insight

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/oopslink/agent-center/internal/concurrency"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/persistence"
)

type ObservationRepo struct {
	db  *sql.DB
	gen idgen.Generator
}

func NewObservationRepo(db *sql.DB, gen idgen.Generator) *ObservationRepo {
	return &ObservationRepo{db: db, gen: gen}
}

func (r *ObservationRepo) Append(ctx context.Context, workerID, agentID string, snap concurrency.AgentSnapshot, observedAt time.Time) (string, error) {
	if r == nil || r.db == nil {
		return "", nil
	}
	if workerID == "" || agentID == "" {
		return "", fmt.Errorf("insight observation: worker_id and agent_id required")
	}
	id := r.gen.NewULID()
	b, err := json.Marshal(snap)
	if err != nil {
		return "", err
	}
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	_, err = exec.ExecContext(ctx, `INSERT INTO agent_concurrency_observations
		(id, worker_id, agent_id, snapshot, observed_at, created_at)
		VALUES (?,?,?,?,?,?)`,
		id, workerID, agentID, string(b), ts(observedAt), ts(time.Now().UTC()))
	if err != nil {
		return "", err
	}
	return id, nil
}

func ts(t time.Time) string {
	if t.IsZero() {
		t = time.Now().UTC()
	}
	return t.UTC().Format(time.RFC3339Nano)
}
