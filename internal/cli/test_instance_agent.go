// test_instance_agent.go — `install test-instance --with-agent` (v2.8 #261).
//
// Phase 2 of the test-instance tenant layer. Builds on --with-seed (#257):
// after seeding a usable tenant, it org-enrolls the workers INTO the seeded org
// (via the org-scoped mint-enroll flow, using the owner's session) so they
// control-connect — resolving the #255 finding where admin-endpoint-enrolled
// workers stay workforce-registered but control-disconnect with 409
// worker_not_org_enrolled. It then creates a real agent bound to a connected
// worker and dispatches a simple task so the agent runs and produces real
// tool_use/result events (closing the v2.7.1 #216 / §8 caveat source).
//
// REORDER vs #255: workers are installed HERE (after the seed), with org-bound
// enroll tokens — not during provisionTestInstance (which, for --with-agent,
// installs the center alone). All driving is via the REAL center HTTP API.
package cli

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"
)

func seedAndEnrollWithAgent(ctx context.Context, pack *accessPack, layout testInstanceLayout, workers int, readyTimeout time.Duration) error {
	if workers < 1 {
		return fmt.Errorf("--with-agent requires --workers >= 1 (the agent runs on a worker)")
	}
	version := installerVersion()
	base := strings.TrimRight(pack.WebURL, "/")

	// 1. Seed the tenant (signup owner+org → project → channel). Reuse the
	//    authenticated session (cookiejar) for the org-scoped calls that follow.
	client, slug, err := seedTestInstanceTenant(ctx, pack)
	if err != nil {
		return fmt.Errorf("seed tenant: %w", err)
	}

	// 2. Org-enroll each worker: org-scoped mint-enroll (binds the worker to the
	//    seeded org via the owner session) → install + start with the org-bound
	//    enroll token + pinned fingerprint → it control-connects (no 409).
	//    NOTE: mint-enroll assigns its OWN worker_id (worker-<hex>); we install at
	//    our namespaced prefix but with that server-assigned id, and cleanup reads
	//    the id back from each worker's config (see workerIDFromConfig).
	for n := 1; n <= workers; n++ {
		var mint struct {
			Token         string `json:"token"`
			Fingerprint   string `json:"fingerprint"`
			BootstrapHost string `json:"bootstrap_host"`
			WorkerID      string `json:"worker_id"`
		}
		if err := seedPostJSON(ctx, client, base+"/api/orgs/"+slug+"/admintoken/mint-enroll",
			map[string]any{"name": layout.workerID(n)}, &mint); err != nil {
			return fmt.Errorf("mint-enroll worker %d: %w", n, err)
		}
		if mint.Token == "" || mint.BootstrapHost == "" {
			return fmt.Errorf("mint-enroll worker %d: empty token/bootstrap_host in response", n)
		}
		wPrefix := layout.workerPrefix(n)
		var wsink bytes.Buffer
		if code := installWorkerFresh(&wsink, &wsink, installContext{
			Prefix:      wPrefix,
			UserMode:    isMacRuntime(),
			WorkerID:    mint.WorkerID,
			WorkerName:  layout.workerID(n),
			Bootstrap:   "tcp://" + mint.BootstrapHost,
			Token:       mint.Token,
			Fingerprint: mint.Fingerprint,
			Version:     version,
			Service:     true,
		}); code != ExitOK {
			return fmt.Errorf("install org-bound worker %s: %s", mint.WorkerID, strings.TrimSpace(wsink.String()))
		}
		pack.Workers = append(pack.Workers, accessPackWorker{ID: mint.WorkerID, Prefix: wPrefix})
	}
	pack.WorkersNote = fmt.Sprintf("%d worker(s) org-enrolled into the seeded org via org-scoped mint-enroll — control-connected (no worker_not_org_enrolled 409; resolves the #255 finding)", workers)

	// 3. Create a real agent bound to the first org-connected worker (cli=
	//    claude-code, model=claude-opus-4-8). The owner session (admin)
	//    authorizes creation.
	connectedWorker := pack.Workers[len(pack.Workers)-workers].ID
	agentName := "Sandbox Agent " + pack.ID
	if err := seedPostJSON(ctx, client, base+"/api/orgs/"+slug+"/members/agent", map[string]any{
		"display_name": agentName,
		"role":         "member",
		"cli":          "claude-code",
		"model":        "claude-opus-4-8",
		"worker_id":    connectedWorker,
	}, nil); err != nil {
		return fmt.Errorf("create agent: %w", err)
	}
	// Resolve the EXECUTION agent's real id + member-id (the members/agent create
	// response carries the org-member id, not the execution agent id) by reading
	// it back. The agent's id == its identity_member_id (the assignable ref id).
	var agentsList struct {
		Agents []struct {
			ID               string `json:"id"`
			IdentityMemberID string `json:"identity_member_id"`
			Name             string `json:"name"`
		} `json:"agents"`
	}
	if err := seedGetJSON(ctx, client, base+"/api/orgs/"+slug+"/agents", &agentsList); err != nil {
		return fmt.Errorf("list agents: %w", err)
	}
	var agentID, agentMemberID string
	for _, a := range agentsList.Agents {
		if a.Name == agentName {
			agentID, agentMemberID = a.ID, a.IdentityMemberID
			break
		}
	}
	if agentID == "" {
		return fmt.Errorf("created agent %q not found in list", agentName)
	}
	if agentMemberID == "" {
		agentMemberID = agentID
	}
	pack.Agent = &seedAgent{ID: agentID, CLI: "claude-code", Model: "claude-opus-4-8", WorkerID: connectedWorker}

	// 4. START the agent — a freshly-created agent is lifecycle=stopped; it must
	//    be started so the worker reconciles it and it can execute dispatched
	//    work (otherwise the task stays queued and no tool events are produced).
	if err := seedPostJSON(ctx, client, base+"/api/orgs/"+slug+"/agents/"+agentID+"/start", map[string]any{}, nil); err != nil {
		return fmt.Errorf("start agent: %w", err)
	}

	// 5. Dispatch a simple, explicit task to the agent so it runs and produces
	//    tool_use/result events. Concrete prompt (Tester #255 A5: agents are
	//    cautious with vague "acceptance test" wording → a clear task elicits
	//    real tool use). Assignee uses the agent's member-id (agent:<member-id>).
	assignee := "agent:" + agentMemberID
	projectID := ""
	if pack.Entities != nil {
		projectID = pack.Entities.ProjectID
	}
	if projectID == "" {
		return fmt.Errorf("no seeded project to dispatch the task into")
	}
	var taskResp struct {
		ID string `json:"id"`
	}
	if err := seedPostJSON(ctx, client, base+"/api/orgs/"+slug+"/projects/"+projectID+"/tasks", map[string]any{
		"title":       "List the files in the project root",
		"description": "Run `ls` in the project root directory and report the file names you see.",
	}, &taskResp); err != nil {
		return fmt.Errorf("create dispatch task: %w", err)
	}
	if err := seedPostJSON(ctx, client, base+"/api/orgs/"+slug+"/projects/"+projectID+"/tasks/"+taskResp.ID+"/assign", map[string]any{
		"assignee": assignee,
	}, nil); err != nil {
		return fmt.Errorf("assign task to agent: %w", err)
	}
	pack.Agent.DispatchedTaskID = taskResp.ID
	return nil
}
