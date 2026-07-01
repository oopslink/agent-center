package cli

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	agentservice "github.com/oopslink/agent-center/internal/agent/service"
	agentsql "github.com/oopslink/agent-center/internal/agent/sqlite"
	"github.com/oopslink/agent-center/internal/blobstore"
	"github.com/oopslink/agent-center/internal/conversation"
	convservice "github.com/oopslink/agent-center/internal/conversation/service"
	"github.com/oopslink/agent-center/internal/environment"
	envservice "github.com/oopslink/agent-center/internal/environment/service"
	envsql "github.com/oopslink/agent-center/internal/environment/sqlite"
	"github.com/oopslink/agent-center/internal/files"
	filesservice "github.com/oopslink/agent-center/internal/files/service"
	filessql "github.com/oopslink/agent-center/internal/files/sqlite"
	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/outbox"
	outboxsql "github.com/oopslink/agent-center/internal/outbox/sqlite"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
	pmservice "github.com/oopslink/agent-center/internal/projectmanager/service"
	pmsql "github.com/oopslink/agent-center/internal/projectmanager/sqlite"
	settingssql "github.com/oopslink/agent-center/internal/settings/sqlite"
	usagesql "github.com/oopslink/agent-center/internal/usage/sqlite"
	"github.com/oopslink/agent-center/internal/webconsole/api"
	"github.com/oopslink/agent-center/internal/webconsole/spa"
	"github.com/oopslink/agent-center/internal/webconsole/sse"
)

// buildFilesService constructs the files transfer Service from the App's DB +
// the configured blobstore root (mirrors the GC-loop construction in
// runWebConsole). Returns nil when the blobstore root is unset or the local
// blobstore fails to initialize — callers leave FilesSvc nil in that case so
// the /api/files surface degrades to 501 rather than panicking.
func buildFilesService(a *App) *filesservice.Service {
	if a == nil {
		return nil
	}
	blobRoot := a.Config.BlobStore.Root
	if blobRoot == "" {
		return nil
	}
	store, err := blobstore.NewLocalDir(blobRoot)
	if err != nil {
		return nil
	}
	return filesservice.New(filesservice.Deps{
		DB:         a.DB,
		Sessions:   filessql.NewFileTransferSessionRepo(a.DB),
		References: filessql.NewFileReferenceRepo(a.DB),
		Resolver:   files.NewLocalResolver(""),
		BlobStore:  store,
		IDGen:      a.IDGen,
		Clock:      a.Clock,
	}).SetGCRepo(filessql.NewBlobMetadataRepo(a.DB))
}

// buildWebConsoleHandler stitches the App's wired services into the
// HandlerDeps the api package expects + installs WithDeps middleware.
// Returns nil http.Handler when Web Console is disabled.
func buildWebConsoleHandler(a *App, bus *sse.Bus) http.Handler {
	if a == nil {
		return nil
	}
	deps := api.HandlerDeps{
		DB:                  a.DB,
		Actor:               a.operatorActor(),
		EventSink:           a.Sink,
		ConvRepo:            a.ConvRepo,
		MsgRepo:             a.MsgRepo,
		MessageWriter:       a.MessageWriter,
		ChannelMgmtSvc:      a.ChannelMgmtSvc,
		ParticipantMgmtSvc:  a.ParticipantMgmtSvc,
		CarryOverSvc:        a.CarryOverSvc,
		AgentInstanceRepo:   a.AgentInstanceRepo,
		UserSecretRepo:      a.UserSecretRepo,
		UserSecretSvc:       a.UserSecretSvc,
		PM:                  a.PMService,
		CodeRepoSvc:         a.CodeRepoService,
		Reminder:            buildReminderService(a),
		AgentSvc:            a.AgentService,
		LiveState:           a.LiveState, // v2.19.0 concurrency snapshot reader
		EnvControl:          a.EnvControlSvc,
		RuntimeFsDispatcher: a.RuntimeFsDispatcher,
		FilesSvc:            buildFilesService(a),
		ReadStateRepo:       a.ReadStateRepo,
		ReadStateSvc:        a.ReadStateSvc,
		FollowStateSvc:      a.FollowStateSvc,
		AdminTokenSvc:       a.AdminTokenSvc,
		SignupSvc:           a.IdentitySignupSvc,
		SigninSvc:           a.IdentitySigninSvc,
		SignoutSvc:          a.IdentitySignoutSvc,
		AuthSvc:             a.IdentityAuthSvc,
		PasscodeChangeSvc:   a.IdentityPasscodeChangeSvc,
		IdentityRepo:        a.IdentityRepo,
		OrgRepo:             a.IdentityOrgRepo,
		OrgCreateSvc:        a.IdentityOrgCreateSvc,
		OrgLifecycleSvc:     a.IdentityOrgLifecycleSvc,
		MemberRepo:          a.IdentityMemberRepo,
		MemberAddSvc:        a.IdentityMemberAddSvc,
		MemberCreateUserSvc: a.IdentityMemberCreateUserSvc,
		MemberRoleChangeSvc: a.IdentityMemberRoleChangeSvc,
		MemberDisableSvc:    a.IdentityMemberDisableSvc,
		MemberRemoveSvc:     a.IdentityMemberRemoveSvc,
		AgentProvisionSvc:   a.IdentityAgentProvisionSvc,
		OrgUpdateSvc:        a.IdentityOrgUpdateSvc,
		InvitationRepo:      a.IdentityInvitationRepo,
		// I7-D1 (T216): center settings store backing GET/PUT /api/system/wake-guardrail
		// (the I7-D3 Settings panel reads/writes the wake-guardrail thresholds here).
		SettingsStore: settingssql.NewStore(a.DB, a.Clock),
		// I28/F4: per-agent analytics read service (heatmap/cards/trends/Top-task).
		// Wired in BOTH builders so the test handler and the live server agree.
		Analytics: usagesql.NewAnalytics(a.DB),
	}
	srv := api.NewServer(":0", api.Deps{SSE: bus, SPA: spa.Handler()})
	return api.WithDeps(deps)(srv.Handler())
}

// WebConsoleEnrollWiring carries the values the AddWorkerModal needs
// to render a working install command for the worker box. Both are
// known by ServerCommand after the admin TCP listener boots: the
// fingerprint comes from AdminTransportInfo, the bootstrap host is
// derived from the admin_tcp_listen config + the operator-facing
// hostname (or 127.0.0.1 when the listener is loopback-only).
type WebConsoleEnrollWiring struct {
	BootstrapHost string // e.g. "192.168.1.10:7300" or "127.0.0.1:7300"
	Fingerprint   string // SSH-style sha256:HH:HH:...
}

// outboxProjectors is the SINGLE source of the production outbox projector set.
// Both runWebConsole (production) and the wiring test build their projector list
// from this one function, so a future "emit-an-event-but-no-consumer-registered"
// drift (e.g. dropping planParticipantProj — the v2.9 #284/#285 headline fix) is
// caught by the test rather than silently shipping. The returned slice is the
// EXACT variadic set previously passed to outbox.NewRelay, in the SAME order.
//
// wakeProj is ALSO returned individually because it is reused after the relay is
// built (the D2-e-iii wake reconcile loop must drive the SAME instance the relay
// drains — see runWebConsole). All other projectors are relay-only.
//
// Deps: appliedRepo (every projector's applied-store), outboxRepo (the work-item
// transition sink), and controlLog (the agent control / wake command log, built
// from a.ControlStreamBus + envsql by the caller). Everything else comes off a.
func (a *App) outboxProjectors(
	outboxRepo *outboxsql.OutboxRepo,
	appliedRepo *outboxsql.AppliedRepo,
	controlLog *environment.ControlLog,
) ([]outbox.Projector, *envservice.WakeProjector) {
	participantProj := pmservice.NewParticipantProjector(a.DB, a.ConvRepo, appliedRepo, a.IDGen, a.Clock)
	// v2.9 #284/#285 headline fix: the Plan↔Conversation projector consumes
	// EvtPlanCreated → auto-creates the Plan's dedicated conversation + binds
	// conversation_id, and EvtPlanParticipantsChanged → additive participant sync
	// (§9.5). WITHOUT registering it in the relay, a Plan created via the HTTP API
	// emits the event but nothing creates its conversation → conversation_id stays
	// "" → advance has no conversation to @mention into → headline dies (the
	// service-level test wired it, but the real app did not — integration seam).
	planParticipantProj := pmservice.NewPlanParticipantProjector(a.DB, a.ConvRepo, pmsql.NewPlanRepo(a.DB), appliedRepo, a.IDGen, a.Clock).
		// v2.9 P3: wire the optional message + read-state repos so EvtPlanDeleted fully
		// hard-deletes the plan conversation ("删会话"), and EvtPlanArchived archives it.
		WithConversationCascade(a.MsgRepo, a.ReadStateRepo)
	// v2.14.0 I14/F6: the task-input↔Conversation projector. It consumes
	// EvtTaskInputRequested (an input_required block) → posts an input_request
	// message into the task's bound Conversation (sender=assignee), and
	// EvtTaskInputReplied (the user's unblock reply) → posts an input_reply threaded
	// under the request. It is the SOLE Conversation writer for these events
	// (ADR-0052 outbox purity — BlockTask/UnblockTask only emit the event). The
	// Conversation-side write is the TaskInputDispatchAdapter over MessageWriter.
	taskInputConvProj := pmservice.NewTaskInputConversationProjector(
		a.DB, a.ConvRepo, convservice.NewTaskInputDispatchAdapter(a.MessageWriter), appliedRepo, a.Clock)
	// v2.14.0 F7 (issue I14): the pm WorkItemProjector (+ its transition sink) and
	// the agent-control work re-emit on →running were removed — AgentWorkItem
	// retired. Work delivery is now the Task model's concern; the agent-control
	// projector keeps only the reconcile loop.
	agentControlProj := envservice.NewAgentControlProjector(a.DB, controlLog, appliedRepo, a.Clock)
	// v2.7 D2-e-i (OQ5): ADDITIVE wakeup. A message posted into a TASK conversation
	// (MessageWriter emits conversation.message_added) becomes an agent.wake command
	// for every agent whose AgentWorkItem on that task is waiting_input (sender
	// self-excluded), same tx. Like D2-a/c-i this only enqueues — the daemon
	// controller (D2-c-ii) is wired DORMANT (ControlClient nil), so no real effect.
	// I7-D1/T227: the wake-chain circuit breaker — ONE process singleton (it holds
	// the rate/cycle runtime state shared across all wake deliveries). I7-D1/T216:
	// the config is now resolved LIVE from the center settings store on every
	// evaluation, so an I7-D3 Settings-panel PUT to /api/system/wake-guardrail takes
	// effect WITHOUT a restart (T224 "参数可配生效"). A read error or absent/blank
	// keys fall back to the conservative DefaultConfig — the guard is never disabled
	// by missing settings (§3.5 "阈值缺省即生效").
	// I7-D1/T341: the ONE wake-chain circuit breaker is now constructed in NewApp
	// and shared with the reply-guardrail (same rate/cycle/depth budget across wake
	// delivery + reply nudges). Reuse that instance here rather than building a
	// second guard with independent state.
	wakeGuard := a.WakeGuard
	sweepAgentRepo := agentsql.NewAgentRepo(a.DB)
	wakeProj := envservice.NewWakeProjector(envservice.WakeProjectorDeps{
		DB:         a.DB,
		Agents:     sweepAgentRepo,
		ControlLog: controlLog,
		Applied:    appliedRepo,
		Clock:      a.Clock,
		WakeGuard:  wakeGuard,
		// T335 follow-up — server-side session-heal sweep (the second net). The
		// WakeReconcileLoop drives ReconcileOnce on a 60s tick; it re-emits
		// agent.work_available for desired-running agents that have queued runnable work
		// but no running session (≈ a dropped wake), which routes through the T335
		// relaunch path. Grace=60s (one tick) debounces a normally-booting session; a
		// stuck-forever agent is bounded by backoff + a give-up cap that escalates once.
		SweepGrace:      60 * time.Second,
		SweepCandidates: buildSweepCandidates(a.PMService, sweepAgentRepo),
		SweepGiveUp: func(_ context.Context, c envservice.SweepCandidate) {
			// The sweep nudged this desired-running agent the cap's worth of times and it
			// still has no running session — surface it for a human instead of slow-
			// retrying silently. (control_events has no global GC today — issue-b71ee81f.)
			slog.Warn("session-heal sweep gave up: desired-running agent never came back up",
				"agent_id", c.AgentID, "worker_id", c.WorkerID, "task_id", c.TaskID)
		},
		// v2.7 D2-e-ii (OQ5 method 甲): batch-flush deps. The projector would read
		// an agent's read-state cursor + the task conversation messages and enqueue
		// ONE merged agent.wake with all unread. Still DORMANT (control loop off; the
		// former request_input → agent.awaiting_input trigger was retired in F7).
		ConvRepo:  a.ConvRepo,
		MsgRepo:   a.MsgRepo,
		ReadState: a.ReadStateRepo,
		// v2.7 #185 (FINDING-H): conversational wake. DisplayName resolves an
		// identity ref → display_name for channel @mention matching; SystemNotify
		// posts a "not running" system message when a DM/channel targets a stopped
		// agent (visible signal, no silent black hole). Both gate the DM/channel→
		// agent path on (control loop off → still DORMANT until cutover).
		DisplayName: func(ctx context.Context, identityRef string) (string, bool) {
			if a.IdentityRepo == nil {
				return "", false
			}
			id := identityRef
			if i := strings.IndexByte(id, ':'); i >= 0 {
				id = id[i+1:] // strip the agent:/user: scheme → bare identity id
			}
			idn, err := a.IdentityRepo.GetByID(ctx, id)
			if err != nil || idn == nil {
				return "", false
			}
			return idn.DisplayName(), true
		},
		// quick-fix (system-msg-activity): both ports return the POSTED message id so
		// the sanctioned nudge paths can align their converse idempotency key with the
		// generic system-message wake (dedup). SystemNotify stays content_kind=system
		// (notification chrome — NOT delivered as a message); SystemMessage stays
		// content_kind=text (a real message the generic wake delivers + records as
		// "Received").
		SystemNotify: func(ctx context.Context, convID, text string) (string, error) {
			if a.MessageWriter == nil {
				return "", nil
			}
			res, err := a.MessageWriter.AddMessage(ctx, convservice.AddMessageCommand{
				ConversationID:   conversation.ConversationID(convID),
				SenderIdentityID: conversation.IdentityRef("system"),
				ContentKind:      conversation.MessageContentSystem,
				Direction:        conversation.DirectionOutbound,
				Content:          text,
				Actor:            observability.Actor("system"),
			})
			if err != nil {
				return "", err
			}
			return string(res.MessageID), nil
		},
		SystemMessage: func(ctx context.Context, convID, text string) (string, error) {
			if a.MessageWriter == nil {
				return "", nil
			}
			res, err := a.MessageWriter.AddMessage(ctx, convservice.AddMessageCommand{
				ConversationID:   conversation.ConversationID(convID),
				SenderIdentityID: conversation.IdentityRef("system"),
				ContentKind:      conversation.MessageContentText,
				Direction:        conversation.DirectionInbound,
				Content:          text,
				Actor:            observability.Actor("system"),
			})
			if err != nil {
				return "", err
			}
			return string(res.MessageID), nil
		},
		// v2.7.1 #224: an issue/task conversation's @mention can target an agent that
		// is a MEMBER of the owning project (not only an explicit participant). Resolve
		// owner_ref (pm://issues|tasks/<id>) → owning project → its agent member-ids
		// (stripped of "agent:"). channel/dm owner_refs → no project → empty.
		ProjectAgentMembers: func(ctx context.Context, ownerRef string) ([]string, error) {
			var projectID pm.ProjectID
			switch {
			case strings.HasPrefix(ownerRef, "pm://tasks/"):
				tk, err := pmsql.NewTaskRepo(a.DB).FindByID(ctx, pm.TaskID(strings.TrimPrefix(ownerRef, "pm://tasks/")))
				if err != nil || tk == nil {
					return nil, err
				}
				projectID = tk.ProjectID()
			case strings.HasPrefix(ownerRef, "pm://issues/"):
				is, err := pmsql.NewIssueRepo(a.DB).FindByID(ctx, pm.IssueID(strings.TrimPrefix(ownerRef, "pm://issues/")))
				if err != nil || is == nil {
					return nil, err
				}
				projectID = is.ProjectID()
			case strings.HasPrefix(ownerRef, "pm://plans/"):
				// v2.9 ② (@oopslink): a PLAN conversation's @mention candidates broaden
				// to the plan's project agent-members too (symmetric with issue/task), so
				// a human can @ any project agent in a plan conversation, not just a
				// participant. plan → its project.
				pl, err := pmsql.NewPlanRepo(a.DB).FindByID(ctx, pm.PlanID(strings.TrimPrefix(ownerRef, "pm://plans/")))
				if err != nil || pl == nil {
					return nil, err
				}
				projectID = pl.ProjectID()
			default:
				return nil, nil // not a project-owned conversation
			}
			members, err := pmsql.NewProjectMemberRepo(a.DB).ListByProject(ctx, projectID)
			if err != nil {
				return nil, err
			}
			var out []string
			for _, m := range members {
				if ref := string(m.IdentityID()); strings.HasPrefix(ref, "agent:") {
					out = append(out, strings.TrimPrefix(ref, "agent:"))
				}
			}
			return out, nil
		},
		// T250: resolve a plan_id → its name so a plan-chat converse brief reads
		// "this conversation belongs to plan ⟨name⟩(plan_id)". Resolved live at wake
		// time (not denormalized onto the conversation) so a renamed plan stays
		// correct — mirroring the task/issue title convention. A miss falls back to
		// the plan_id alone in the brief.
		PlanName: func(ctx context.Context, planID string) (string, bool) {
			pl, err := pmsql.NewPlanRepo(a.DB).FindByID(ctx, pm.PlanID(planID))
			if err != nil || pl == nil {
				return "", false
			}
			name := strings.TrimSpace(pl.Name())
			if name == "" {
				return "", false
			}
			return name, true
		},
		// T255 (OQ2): live issue/task title resolvers, mirroring PlanName — an
		// issue/task chat's converse brief header shows the real title (resolved at
		// wake time so a rename reads correctly; a miss falls back to the id).
		IssueTitle: func(ctx context.Context, issueID string) (string, bool) {
			is, err := pmsql.NewIssueRepo(a.DB).FindByID(ctx, pm.IssueID(issueID))
			if err != nil || is == nil {
				return "", false
			}
			title := strings.TrimSpace(is.Title())
			if title == "" {
				return "", false
			}
			return title, true
		},
		TaskTitle: func(ctx context.Context, taskID string) (string, bool) {
			tk, err := pmsql.NewTaskRepo(a.DB).FindByID(ctx, pm.TaskID(taskID))
			if err != nil || tk == nil {
				return "", false
			}
			title := strings.TrimSpace(tk.Title())
			if title == "" {
				return "", false
			}
			return title, true
		},
	})
	// v2.14.0 F7 (issue I14): the agent-work-item projection projector, the
	// task-status-sync projector, and the work-item-event projector were removed —
	// AgentWorkItem retired (the agent_work_item_projections read model + the
	// pm.Task↔WorkItem status sync are gone).
	// v2.9 P2-1 AUTO-ADVANCE core (#266 LESSON): the orchestrator projector must be
	// in the PRODUCTION relay or auto-advance is silently dead. It consumes
	// pm.task.state_changed (a plan-task reaching a terminal state → re-dispatch the
	// plan's newly-ready downstream nodes) and pm.plan.started (→ dispatch the plan's
	// initial ready nodes). It reuses a.PMService's dispatch core (which has the
	// PlanDispatcher wired); idempotent via AppliedStore + INSERT-OR-IGNORE dispatch
	// records (§9.3). Registered in the returned slice + guarded by the #266 class-test.
	planOrchestratorProj := pmservice.NewPlanOrchestratorProjector(a.DB, a.PMService, appliedRepo, a.Clock)
	// T465 (issue I34): the IMMEDIATE dispatch-wake projector — the replacement for the
	// retired WorkItemProjector's wake side. It consumes pm.task.assigned / .reassigned /
	// .state_changed → emits a content-free agent.work_available onto the assignee's worker
	// stream the instant work lands (live inject / down relaunch), so dispatch/reassign/
	// completion-handoff no longer waits 60–120s for the WakeReconcileLoop sweep. The sweep
	// stays as the down-session fallback; DispatchRecord is untouched. Resolution closures keep
	// the pm/agent reads in composition (BC boundary), mirroring buildSweepCandidates.
	dispatchWakeProj := envservice.NewDispatchWakeProjector(envservice.DispatchWakeProjectorDeps{
		ControlLog:   controlLog,
		AssignTarget: buildAssignTarget(a.PMService, sweepAgentRepo),
		RepushTarget: buildRepushTarget(a.PMService, sweepAgentRepo),
	})
	// message_acknowledged activity (docs/design/features/agent-message-consumption-activity.md):
	// agent 主动 mark_seen（PULL）→ 在其 activity 流追加一条 ack，闭合「agent 确认已读」回路。
	msgAckProj := envservice.NewMessageAckProjector(a.DB, a.AgentActivityRepo, appliedRepo, a.IDGen, a.Clock)
	// v2.18.3 BE-2 (issue-577a7b0e): the EVENT fast path of the auto-assign dual-track
	// trigger. It consumes pm.task.state_changed / pm.task.created and fires a
	// project-scoped auto-assign sweep the instant a task enters the pool or an agent
	// frees a run slot — so an ownerless pool task is picked up in ~relay-latency, not at
	// the next periodic AutoAssignReconciler tick. Best-effort + idempotent (CAS); the
	// periodic loop is the completeness backstop. MUST be registered here or only the
	// periodic path runs (a defined-but-unregistered projector has no prod consumer).
	autoAssignTriggerProj := pmservice.NewAutoAssignTriggerProjector(a.PMService, nil)
	return []outbox.Projector{
		participantProj,
		planParticipantProj,
		taskInputConvProj,
		agentControlProj,
		wakeProj,
		planOrchestratorProj,
		dispatchWakeProj,
		msgAckProj,
		autoAssignTriggerProj,
	}, wakeProj
}

// runWebConsole binds + serves the Web Console HTTP API at addr,
// enforcing the loopback bind guard (per ADR-0037 / NF2 — no remote
// listen). Returns http.ErrServerClosed on graceful shutdown.
func runWebConsole(ctx context.Context, a *App, bus *sse.Bus, addr string, enroll WebConsoleEnrollWiring, logger func(string)) (cleanup func() error, err error) {
	if addr == "" {
		addr = "127.0.0.1:7100"
	}
	if a == nil {
		return func() error { return nil }, errors.New("webconsole: app nil")
	}
	// v2.6 production guarantee: webconsole requires Identity BC auth.
	// AuthSvc is wired only when secret_management.master_key_file is set
	// (master key doubles as the JWT HS256 signing key per ADR-0043 §6).
	// Refuse to start the webconsole when auth is unconfigured rather than
	// allowing the per-request middleware to fail-open.
	if a.IdentityAuthSvc == nil {
		return func() error { return nil }, errors.New(
			"webconsole: auth not configured — set secret_management.master_key_file " +
				"in the server config (the master key doubles as the JWT signing key)")
	}
	// Every background loop started below (server, fanout, pump, GC, wake,
	// plan-reconcile, resolved-issue-closer, control-event GC) fans its error
	// strings onto this single `logger`, concurrently. The fan-out makes the
	// logger contract "may be called from many goroutines at once", so a
	// caller's non-thread-safe sink (e.g. a test's shared []string / bytes.Buffer)
	// would data-race. Serialize every call through one mutex here so the
	// contract holds for ANY logger — ordering-only, no behavior change. (nil
	// logger is normalized to a no-op; callers historically always pass non-nil.)
	{
		var logMu sync.Mutex
		inner := logger
		if inner == nil {
			inner = func(string) {}
		}
		logger = func(s string) {
			logMu.Lock()
			defer logMu.Unlock()
			inner(s)
		}
	}
	// v2.7 D3-d/D3-c: a single files transfer Service instance backs BOTH the
	// /api/files HTTP surface (FilesSvc) and the refcount GC loop below — built
	// once from the configured blobstore root (nil when unconfigured).
	filesSvc := buildFilesService(a)
	deps := api.HandlerDeps{
		DB:                  a.DB,
		Actor:               a.operatorActor(),
		EventSink:           a.Sink,
		ConvRepo:            a.ConvRepo,
		MsgRepo:             a.MsgRepo,
		MessageWriter:       a.MessageWriter,
		ChannelMgmtSvc:      a.ChannelMgmtSvc,
		ParticipantMgmtSvc:  a.ParticipantMgmtSvc,
		CarryOverSvc:        a.CarryOverSvc,
		AgentInstanceRepo:   a.AgentInstanceRepo,
		UserSecretRepo:      a.UserSecretRepo,
		UserSecretSvc:       a.UserSecretSvc,
		PM:                  a.PMService,
		CodeRepoSvc:         a.CodeRepoService,
		Reminder:            buildReminderService(a),
		AgentSvc:            a.AgentService,
		LiveState:           a.LiveState, // v2.19.0 concurrency snapshot reader
		EnvControl:          a.EnvControlSvc,
		RuntimeFsDispatcher: a.RuntimeFsDispatcher,
		FilesSvc:            filesSvc,
		FleetSvc:            a.FleetSvc,
		ReadStateRepo:       a.ReadStateRepo,
		ReadStateSvc:        a.ReadStateSvc,
		FollowStateSvc:      a.FollowStateSvc,
		AdminTokenSvc:       a.AdminTokenSvc,
		EnrollBootstrapHost: enroll.BootstrapHost,
		EnrollFingerprint:   enroll.Fingerprint,
		WorkerRenameSvc:     a.EnrollSvc,
		WorkerAddSvc:        a.EnrollSvc,
		WorkerRemoveSvc:     a.EnrollSvc,
		WorkerRepo:          a.WorkerRepo,
		FileTransferRepo:    filessql.NewFileTransferSessionRepo(a.DB),
		SignupSvc:           a.IdentitySignupSvc,
		SigninSvc:           a.IdentitySigninSvc,
		SignoutSvc:          a.IdentitySignoutSvc,
		AuthSvc:             a.IdentityAuthSvc,
		PasscodeChangeSvc:   a.IdentityPasscodeChangeSvc,
		IdentityRepo:        a.IdentityRepo,
		OrgRepo:             a.IdentityOrgRepo,
		OrgCreateSvc:        a.IdentityOrgCreateSvc,
		OrgLifecycleSvc:     a.IdentityOrgLifecycleSvc,
		MemberRepo:          a.IdentityMemberRepo,
		MemberAddSvc:        a.IdentityMemberAddSvc,
		MemberCreateUserSvc: a.IdentityMemberCreateUserSvc,
		MemberRoleChangeSvc: a.IdentityMemberRoleChangeSvc,
		MemberDisableSvc:    a.IdentityMemberDisableSvc,
		MemberRemoveSvc:     a.IdentityMemberRemoveSvc,
		AgentProvisionSvc:   a.IdentityAgentProvisionSvc,
		OrgUpdateSvc:        a.IdentityOrgUpdateSvc,
		InvitationRepo:      a.IdentityInvitationRepo,
		// I7-D1 (T216) center settings store backing GET/PUT /api/system/wake-guardrail
		// (the I7-D3 Settings panel reads/writes the wake-guardrail thresholds). This is
		// the LIVE webconsole deps path — buildWebConsoleHandler is test-only, so without
		// this line PUT /system/wake-guardrail 501s "settings store not configured" on the
		// real server even though the wiring test is green.
		SettingsStore: settingssql.NewStore(a.DB, a.Clock),
		// I28/F4: per-agent analytics read service (heatmap/cards/trends/Top-task).
		// Wired in BOTH builders so the test handler and the live server agree.
		Analytics: usagesql.NewAnalytics(a.DB),
	}
	srv := api.NewServer(addr, api.Deps{
		SSE: bus, SPA: spa.Handler(),
		Version: ResolvedBuildVersion(),
		Branch:  ResolvedBuildBranch(),
		Commit:  ResolvedBuildCommit(),
		BuiltAt: ResolvedBuildBuiltAt(),
	})
	// Wrap the inner mux with deps middleware; install it as the
	// server's handler so the loopback guard in api.Server.ListenAndServe
	// still applies.
	wrapped := api.WithDeps(deps)(srv.Handler())
	srv.SetHandler(wrapped)
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger("webconsole: " + err.Error())
		}
	}()
	// Start the EventSink → SSE Bus fan-out tailer. It polls the
	// events table on a 250ms ticker and publishes each new event
	// onto the bus, where subscribed users receive it.
	fanoutCtx, fanoutCancel := context.WithCancel(ctx)
	fanout := sse.NewEventFanout(a.EventRepo, bus, 0).WithErrorHandler(func(err error) {
		logger("webconsole fanout: " + err.Error())
	})
	go fanout.Run(fanoutCtx)

	// v2.7 B3: bring the cross-BC outbox online. A single-goroutine Pump
	// drains the outbox (backlog on boot, then ~1s ticker) and applies the
	// ProjectManager→Conversation participant projection + the AssignTask→
	// AgentWorkItem projection. Without this loop the projectors are static
	// and no cross-BC effect ever happens (plan §10 OQ1). Mirrors the fanout
	// lifecycle (ctx-cancel + graceful shutdown).
	outboxRepo := outboxsql.NewOutboxRepo(a.DB)
	appliedRepo := outboxsql.NewAppliedRepo(a.DB)
	// v2.7 D2-a: ADDITIVE reconcile projector. Agent lifecycle intent changes
	// (C3 agent.lifecycle_changed) become declarative agent.reconcile commands on
	// the agent's Worker control stream (D1). D1's NoopHandler no-op-acks them →
	// zero real effect yet (no execution cutover; old taskruntime path untouched).
	// v2.7 D5 slice-1: inject the shared SSE down-push bus as the OPTIONAL
	// after-commit publisher. AppendCommand best-effort pushes each newly-appended
	// command (with its offset) to the bus so a subscribed worker gets it with low
	// latency; a publish failure cannot fail the append (poll + catch-up recover).
	// nil-safe (a.ControlStreamBus may be nil in non-webconsole boot paths).
	controlLog := environment.NewControlLog(envsql.NewControlEventRepo(a.DB), a.IDGen, a.Clock)
	if a.ControlStreamBus != nil {
		controlLog = controlLog.WithPublisher(a.ControlStreamBus)
	}
	// SINGLE source of the production projector set (incl planParticipantProj — the
	// v2.9 #284/#285 headline fix). outboxProjectors is shared with the wiring test
	// so a dropped projector (emit-an-event-but-no-consumer-registered) fails there
	// instead of silently shipping. wakeProj is also returned individually because
	// the D2-e-iii wake reconcile loop below must drive the SAME instance the relay
	// drains (push + poll dedup on the same batch key).
	projectors, wakeProj := a.outboxProjectors(outboxRepo, appliedRepo, controlLog)
	// T206: the Reminder delivery projector consumes cognition.reminder.fired and
	// wakes the remindee (design §3.4). Appended to the shared projector set.
	if rdp := buildReminderDeliveryProjector(a); rdp != nil {
		projectors = append(projectors, rdp)
	}
	relay := outbox.NewRelay(outboxRepo, appliedRepo, a.Clock, projectors...)
	pump := outbox.NewPump(relay, time.Second, 0).WithErrorHandler(func(err error) {
		logger("webconsole outbox pump: " + err.Error())
	})
	// T206: scan + fire due reminders on the same 1s tick (design §D4).
	if hook := buildReminderTickHook(a, logger); hook != nil {
		pump = pump.WithTickHook(hook)
	}
	pumpCtx, pumpCancel := context.WithCancel(ctx)
	go pump.Run(pumpCtx)

	// v2.7 D3-c: bring the file-blob refcount GC online. A single-goroutine
	// loop (initial pass on boot, then ~1h ticker) expires stale upload sessions
	// and reaps blobs whose live-reference count has been zero past the grace
	// period (default 7d, ADR-0048 §5). Mirrors the Pump/fanout lifecycle
	// (ctx-cancel + graceful shutdown); ADDITIVE — it does not touch the
	// existing pump/fanout. The files transfer Service was constructed above
	// (shared with the /api/files HTTP surface); the resolver yields
	// blobstore-relative paths so the blobstore owns the physical root.
	gcCtx, gcCancel := context.WithCancel(ctx)
	if filesSvc != nil {
		gcLoop := filesservice.NewGCLoop(filesSvc, filesservice.DefaultGCGrace, time.Hour).
			WithErrorHandler(func(err error) {
				logger("webconsole files gc: " + err.Error())
			})
		go gcLoop.Run(gcCtx)
	}

	// v2.7 D2-e-iii (OQ5 poll-fallback "push优先 + poll兜底"): a slow-cadence
	// (60s) sweep recomputes every waiting_input agent's UNREAD messages from its
	// read-state cursor and enqueues any pending agent.wake batch — INDEPENDENT of
	// whether a push wake signal ever fired (self-heals a never-enqueued silent
	// bug). It SHARES the same wakeProj instance as the relay (push), so push/poll
	// produce the same enqueue + the SAME batch key → ControlLog dedups, never
	// double-delivers. DORMANT until D2-f: the enqueued commands sit in the control
	// log until ControlClient is flipped (nobody pulls → zero real effect); it is
	// NOT gated on ControlClient. Mirrors the Pump/GC lifecycle (ctx-cancel +
	// graceful shutdown).
	wakeLoopCtx, wakeLoopCancel := context.WithCancel(ctx)
	wakeLoop := envservice.NewWakeReconcileLoop(wakeProj, 60*time.Second, func(msg string) {
		logger("webconsole wake reconcile: " + msg)
	})
	go wakeLoop.Run(wakeLoopCtx)

	// v2.9 P2-3 RECONCILIATION SWEEP: a slow-cadence (60s) sweep re-runs the
	// IDEMPOTENT plan dispatch core over every running Plan — the safety net that
	// re-dispatches any ready-but-undispatched node a missed pm.task.state_changed /
	// crash left stranded. It reuses a.PMService (with the PlanDispatcher wired);
	// already-dispatched nodes are skipped (INSERT-OR-IGNORE record), so it
	// dispatches nothing on the happy path. Mirrors the wakeLoop lifecycle
	// (ctx-cancel + graceful shutdown).
	planReconcileLoopCtx, planReconcileLoopCancel := context.WithCancel(ctx)
	planReconcileLoop := pmservice.NewPlanReconcileLoop(a.PMService, 60*time.Second, func(msg string) {
		logger("webconsole plan reconcile: " + msg)
	})
	go planReconcileLoop.Run(planReconcileLoopCtx)

	// Resolved issue lifecycle: after an issue remains resolved for the default
	// grace period, close it automatically. The service uses durable
	// status_changed_at, so restarts do not lose the countdown.
	resolvedIssueCloserCtx, resolvedIssueCloserCancel := context.WithCancel(ctx)
	resolvedIssueCloser := pmservice.NewResolvedIssueCloser(a.PMService, 0, 0, func(msg string, args ...any) {
		logger("webconsole resolved issue closer: " + fmt.Sprintf(msg, args...))
	})
	go func() {
		_ = resolvedIssueCloser.Run(resolvedIssueCloserCtx)
	}()

	// T340 (issue-b71ee81f): bring the control-event stream GC online. The
	// worker_control_events table is append-only for every command type and had no
	// GC → chronic growth. This slow-cadence (hourly) sweep prunes acked rows older
	// than the retention window (default 3 days, env-overridable). It runs once on
	// boot then on a ticker, mirroring the wakeLoop/planReconcileLoop lifecycle
	// (ctx-cancel + graceful shutdown). SAFETY: it never deletes an un-acked row, so
	// a worker offline past retention loses no undelivered command on reconnect.
	ceRetention, ceInterval := controlEventGCConfig(logger)
	controlEventGCCtx, controlEventGCCancel := context.WithCancel(ctx)
	controlEventGC := envservice.NewControlEventGC(
		envsql.NewControlEventRepo(a.DB), a.Clock, ceRetention, ceInterval,
		func(format string, args ...any) {
			logger("webconsole control-event gc: " + fmt.Sprintf(format, args...))
		},
	)
	go controlEventGC.Run(controlEventGCCtx)

	// incident 2026-06-30: agent_activity_events is an append-only telemetry log that —
	// unlike worker_control_events above — had NO GC, so it grew unbounded (~598MB / 354k
	// rows over 26 days), bloating the DB to 691MB and causing SQLITE_BUSY write-lock
	// contention that stalled the control-flow engine. This slow-cadence (hourly) sweep
	// prunes events older than the retention window (default 7 days, env-overridable),
	// same boot-then-ticker lifecycle. SAFETY: pure telemetry, no replay/ack contract, so
	// straight age-based deletion is safe (durable aggregates live in agent_activity_daily).
	aaeRetention, aaeInterval := activityEventGCConfig(logger)
	activityEventGCCtx, activityEventGCCancel := context.WithCancel(ctx)
	activityEventGC := agentservice.NewActivityEventGC(
		agentsql.NewActivityEventRepo(a.DB), a.Clock, aaeRetention, aaeInterval,
		func(format string, args ...any) {
			logger("webconsole activity-event gc: " + fmt.Sprintf(format, args...))
		},
	)
	go activityEventGC.Run(activityEventGCCtx)

	cleanup = func() error {
		fanoutCancel()
		pumpCancel()
		gcCancel()
		wakeLoopCancel()
		planReconcileLoopCancel()
		resolvedIssueCloserCancel()
		controlEventGCCancel()
		activityEventGCCancel()
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = bus.Shutdown(shutCtx)
		return srv.Shutdown(shutCtx)
	}
	return cleanup, nil
}

// controlEventGCConfig resolves the control-event GC retention + sweep interval from
// the environment (T340 — "别写死"), falling back to the service defaults (3-day
// retention / hourly sweep) when unset or unparseable. Both accept a Go duration
// string (e.g. "72h", "30m"):
//   - AC_CONTROL_EVENTS_RETENTION — age past which an acked control event is pruned.
//   - AC_CONTROL_EVENTS_GC_INTERVAL — sweep cadence.
//
// A non-positive or malformed value is logged once and ignored (default stands), so a
// fat-fingered env never silently disables or thrashes the sweep.
func controlEventGCConfig(logger func(string)) (retention, interval time.Duration) {
	retention = envservice.DefaultControlEventRetention
	interval = envservice.DefaultControlEventGCInterval
	if v := strings.TrimSpace(os.Getenv("AC_CONTROL_EVENTS_RETENTION")); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			retention = d
		} else {
			logger(fmt.Sprintf("webconsole control-event gc: ignoring invalid AC_CONTROL_EVENTS_RETENTION=%q (using %s)", v, retention))
		}
	}
	if v := strings.TrimSpace(os.Getenv("AC_CONTROL_EVENTS_GC_INTERVAL")); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			interval = d
		} else {
			logger(fmt.Sprintf("webconsole control-event gc: ignoring invalid AC_CONTROL_EVENTS_GC_INTERVAL=%q (using %s)", v, interval))
		}
	}
	return retention, interval
}

// activityEventGCConfig resolves the activity-event GC retention + sweep interval from
// the environment, falling back to the service defaults (7-day retention / hourly sweep)
// when unset or unparseable. Both accept a Go duration string (e.g. "168h", "30m"):
//   - AC_ACTIVITY_EVENTS_RETENTION — age past which an activity event is pruned.
//   - AC_ACTIVITY_EVENTS_GC_INTERVAL — sweep cadence.
//
// A non-positive or malformed value is logged once and ignored (default stands), so a
// fat-fingered env never silently disables or thrashes the sweep.
func activityEventGCConfig(logger func(string)) (retention, interval time.Duration) {
	retention = agentservice.DefaultActivityEventRetention
	interval = agentservice.DefaultActivityEventGCInterval
	if v := strings.TrimSpace(os.Getenv("AC_ACTIVITY_EVENTS_RETENTION")); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			retention = d
		} else {
			logger(fmt.Sprintf("webconsole activity-event gc: ignoring invalid AC_ACTIVITY_EVENTS_RETENTION=%q (using %s)", v, retention))
		}
	}
	if v := strings.TrimSpace(os.Getenv("AC_ACTIVITY_EVENTS_GC_INTERVAL")); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			interval = d
		} else {
			logger(fmt.Sprintf("webconsole activity-event gc: ignoring invalid AC_ACTIVITY_EVENTS_GC_INTERVAL=%q (using %s)", v, interval))
		}
	}
	return retention, interval
}

// _ keeps observability import alive (handler deps include Actor).
var _ = observability.Actor("")
