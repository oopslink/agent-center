package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/observability/peek"
	"github.com/oopslink/agent-center/internal/observability/query"
)

// ObservabilityCommands returns the leaf commands for the 6 Observability
// CLI verbs: inspect / query / ps / stats / logs / peek-trace.
//
// Per plan-4 § 3.6 + 03-cli-subcommands § 8.7.
func (a *App) ObservabilityCommands() []*Command {
	return []*Command{
		a.inspectCommand(),
		a.queryCommand(),
		a.psCommand(),
		a.statsCommand(),
		a.logsCommand(),
		a.peekTraceCommand(),
	}
}

func (a *App) inspectCommand() *Command {
	return &Command{
		Name:     "inspect",
		Summary:  "Inspect a single resource (task / execution / worker / issue / conversation / input_request / project / worktree).",
		LongHelp: "agent-center inspect <kind> <id> [--format=human|json]\n",
		Flags: func(fs *flag.FlagSet) Handler {
			format := fs.String("format", FormatTable, formatFlagHelp())
			return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
				if len(args) < 2 {
					return PrintError(errw, *format, "usage_error",
						"inspect requires <kind> <id>", ExitUsage)
				}
				kind, id := args[0], args[1]
				if !query.ValidInspectKind(kind) {
					return PrintError(errw, *format, "unknown_kind",
						fmt.Sprintf("inspect kind must be one of %s", strings.Join(inspectKindNames(), "|")), ExitUsage)
				}
				var res query.InspectResult
				if a.Client != nil {
					if cerr := a.Client.InspectRaw(ctx, kind, id, &res); cerr != nil {
						return mapInspectClientErr(cerr, *format, errw)
					}
				} else {
					r, err := a.QuerySvc.Inspect(ctx, kind, id)
					if err != nil {
						return mapInspectErr(err, *format, errw)
					}
					res = r
				}
				return printInspect(out, *format, res)
			}
		},
	}
}

func (a *App) queryCommand() *Command {
	return &Command{
		Name:     "query",
		Summary:  "List resources (tasks / executions / workers / issues / input_requests / proposals / events).",
		LongHelp: "agent-center query <resource> [--filter] [--since] [--until] [--limit] [--cursor] [--format=human|json]\n",
		Flags: func(fs *flag.FlagSet) Handler {
			format := fs.String("format", FormatTable, formatFlagHelp())
			status := fs.String("status", "", "")
			project := fs.String("project", "", "")
			priority := fs.String("priority", "", "")
			blockedBy := fs.String("blocked-by", "", "")
			workerID := fs.String("worker", "", "")
			taskID := fs.String("task-id", "", "")
			execID := fs.String("execution-id", "", "")
			issueID := fs.String("issue-id", "", "")
			opener := fs.String("opener", "", "")
			failedReason := fs.String("failed-reason", "", "")
			actor := fs.String("actor", "", "")
			correlationID := fs.String("correlation-id", "", "")
			decisionID := fs.String("decision-id", "", "")
			eventType := fs.String("type", "", "Event type or prefix (suffix '.' for prefix match)")
			since := fs.String("since", "", "ISO-8601 or relative (e.g. 1h, 30m)")
			until := fs.String("until", "", "")
			limit := fs.Int("limit", 0, "")
			cursor := fs.String("cursor", "", "")
			return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
				if len(args) < 1 {
					return PrintError(errw, *format, "usage_error", "query requires <resource>", ExitUsage)
				}
				resource := args[0]
				if !query.ValidQueryResource(resource) {
					return PrintError(errw, *format, "unknown_resource",
						fmt.Sprintf("resource must be one of %s", strings.Join(queryResourceNames(), "|")), ExitUsage)
				}
				filter := query.QueryFilter{
					Status:        *status,
					ProjectID:     *project,
					Priority:      *priority,
					BlockedBy:     *blockedBy,
					WorkerID:      *workerID,
					TaskID:        *taskID,
					ExecutionID:   *execID,
					IssueID:       *issueID,
					Opener:        *opener,
					FailedReason:  *failedReason,
					Actor:         *actor,
					CorrelationID: *correlationID,
					DecisionID:    *decisionID,
					EventType:     *eventType,
					Limit:         *limit,
					Cursor:        *cursor,
				}
				if *since != "" {
					t, err := parseSince(*since)
					if err != nil {
						return PrintError(errw, *format, "usage_error", err.Error(), ExitUsage)
					}
					filter.Since = &t
				}
				if *until != "" {
					t, err := parseSince(*until)
					if err != nil {
						return PrintError(errw, *format, "usage_error", err.Error(), ExitUsage)
					}
					filter.Until = &t
				}
				var res query.QueryResult
				if a.Client != nil {
					req := QueryRequest{
						Resource:    resource,
						Status:      *status,
						ProjectID:   *project,
						WorkerID:    *workerID,
						TaskID:      *taskID,
						ExecutionID: *execID,
						IssueID:     *issueID,
						Opener:      *opener,
						EventType:   *eventType,
						Limit:       *limit,
						Cursor:      *cursor,
					}
					if cerr := a.Client.QueryRaw(ctx, req, &res); cerr != nil {
						return mapQueryClientErr(cerr, *format, errw)
					}
				} else {
					r, err := a.QuerySvc.Query(ctx, resource, filter)
					if err != nil {
						if errors.Is(err, observability.ErrEventQueryLimitTooLarge) {
							return PrintError(errw, *format, "limit_too_large",
								fmt.Sprintf("limit must be <= %d", observability.MaxEventQueryLimit), ExitUsage)
						}
						return PrintError(errw, *format, "query_failed", err.Error(), ExitBusinessError)
					}
					res = r
				}
				return printQueryResult(out, *format, res)
			}
		},
	}
}

// PsWatchInterval is the default ticker cadence for `ps --watch`. Tests
// override via WithPsWatchInterval to keep wall clock short.
var PsWatchInterval = 2 * time.Second

func (a *App) psCommand() *Command {
	return &Command{
		Name:     "ps",
		Summary:  "Fleet view (active executions + workers + open input requests + pending issues).",
		LongHelp: "agent-center ps [--watch] [--project=<slug>] [--format=human|json]",
		Flags: func(fs *flag.FlagSet) Handler {
			format := fs.String("format", FormatTable, formatFlagHelp())
			watch := fs.Bool("watch", false, "Loop + re-render every 2s")
			project := fs.String("project", "", "Filter all segments to this project slug")
			return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
				doSnap := func() ExitCode {
					var snap query.FleetSnapshot
					if a.Client != nil {
						if cerr := a.Client.FleetSnapshotRaw(ctx, *project, &snap); cerr != nil {
							return PrintError(errw, *format, "fleet_failed", cerr.Error(), ExitBusinessError)
						}
					} else {
						snap = a.FleetSvc.Snapshot(ctx, query.SnapshotFilter{ProjectID: *project})
					}
					return printFleet(out, errw, *format, snap)
				}
				if !*watch {
					return doSnap()
				}
				// v1 watch: clear-screen + redraw every PsWatchInterval. Stops on ctx cancel.
				ticker := time.NewTicker(PsWatchInterval)
				defer ticker.Stop()
				_ = doSnap()
				for {
					select {
					case <-ctx.Done():
						return ExitOK
					case <-ticker.C:
						fmt.Fprint(out, "\033[H\033[2J") // ANSI clear-screen
						if rc := doSnap(); rc != ExitOK {
							return rc
						}
					}
				}
			}
		},
	}
}

func (a *App) statsCommand() *Command {
	return &Command{
		Name:    "stats",
		Summary: "Aggregate metrics (tasks / executions / workers / events / issues).",
		Flags: func(fs *flag.FlagSet) Handler {
			format := fs.String("format", FormatTable, formatFlagHelp())
			scope := fs.String("scope", "tasks", "tasks|executions|workers|events|issues")
			since := fs.String("since", "", "ISO-8601 or relative (1h, 30m, 24h)")
			return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
				if !query.ValidStatsScope(*scope) {
					return PrintError(errw, *format, "unknown_scope",
						fmt.Sprintf("scope must be one of %s", strings.Join(statsScopeNames(), "|")), ExitUsage)
				}
				var sincePtr *time.Time
				if *since != "" {
					t, err := parseSince(*since)
					if err != nil {
						return PrintError(errw, *format, "usage_error", err.Error(), ExitUsage)
					}
					sincePtr = &t
				}
				var res query.StatsResult
				if a.Client != nil {
					// Server-side accepts duration OR RFC3339; pass the raw string.
					if cerr := a.Client.StatsAggregateRaw(ctx, *scope, *since, &res); cerr != nil {
						return PrintError(errw, *format, "stats_failed", cerr.Error(), ExitBusinessError)
					}
				} else {
					r, err := a.StatsSvc.Aggregate(ctx, *scope, sincePtr)
					if err != nil {
						return PrintError(errw, *format, "stats_failed", err.Error(), ExitBusinessError)
					}
					res = r
				}
				return printStats(out, *format, res)
			}
		},
	}
}

func (a *App) logsCommand() *Command {
	return &Command{
		Name:    "logs",
		Summary: "Tail / dump archived task or execution logs (BlobStore).",
		Flags: func(fs *flag.FlagSet) Handler {
			format := fs.String("format", FormatTable, formatFlagHelp())
			follow := fs.Bool("follow", false, "Stream — not supported on archived blobs")
			return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
				if len(args) < 2 {
					return PrintError(errw, *format, "usage_error", "logs requires <kind> <id>", ExitUsage)
				}
				kind, id := args[0], args[1]
				if *follow {
					return PrintError(errw, *format, "follow_not_supported",
						"--follow not supported on archived blobs; use peek-trace for live", ExitUsage)
				}
				if a.Client != nil {
					body, _, cerr := a.Client.LogsOpen(ctx, kind, id)
					if cerr != nil {
						return mapLogsClientErr(cerr, *format, errw)
					}
					if _, err := io.Copy(out, bytes.NewReader(body)); err != nil {
						return PrintError(errw, *format, "logs_stream_error", err.Error(), ExitBusinessError)
					}
					return ExitOK
				}
				if a.LogsSvc == nil {
					return PrintError(errw, *format, "blob_store_unavailable",
						"BlobStore not configured", ExitBusinessError)
				}
				rc, ref, err := a.LogsSvc.Open(ctx, query.LogsRequest{
					Kind: query.LogsKind(kind), ID: id, Follow: *follow,
				})
				if err != nil {
					if errors.Is(err, query.ErrLogsArchivedFollow) {
						return PrintError(errw, *format, "follow_not_supported",
							"--follow not supported on archived blobs; use peek-trace for live", ExitUsage)
					}
					if errors.Is(err, query.ErrLogsKindUnknown) {
						return PrintError(errw, *format, "unknown_kind", err.Error(), ExitUsage)
					}
					if errors.Is(err, query.ErrLogsTargetMissing) {
						return PrintError(errw, *format, "blob_not_found", err.Error(), ExitNotFound)
					}
					return PrintError(errw, *format, "logs_failed", err.Error(), ExitBusinessError)
				}
				defer rc.Close()
				_ = ref
				if _, err := io.Copy(out, rc); err != nil {
					return PrintError(errw, *format, "logs_stream_error", err.Error(), ExitBusinessError)
				}
				return ExitOK
			}
		},
	}
}

func (a *App) peekTraceCommand() *Command {
	return &Command{
		Name:    "peek-trace",
		Summary: "Live agent trace stream from worker daemon (RPC).",
		Flags: func(fs *flag.FlagSet) Handler {
			format := fs.String("format", FormatTable, formatFlagHelp())
			last := fs.Int("last", 0, "Tail last N lines before --follow")
			kind := fs.String("kind", "", "Filter by AgentTraceEvent type (e.g. tool_call|thinking|tool_result|all)")
			follow := fs.Bool("follow", false, "Keep streaming new lines")
			socket := fs.String("socket", "", "Worker daemon socket override (default from config)")
			return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
				if len(args) < 1 {
					return PrintError(errw, *format, "usage_error", "peek-trace requires <execution_id>", ExitUsage)
				}
				execID := args[0]
				sock := *socket
				if sock == "" {
					sock = a.Config.Peek.WorkerSocket
				}
				if sock == "" {
					return PrintError(errw, *format, "socket_unset",
						"peek socket path not configured", ExitBusinessError)
				}
				client := peek.NewClient(sock)
				frames, err := client.Stream(ctx, peek.Request{
					ExecutionID: execID, Last: *last, Kind: *kind, Follow: *follow,
				})
				if err != nil {
					var ce *peek.ErrConnectFailed
					if errors.As(err, &ce) {
						return PrintError(errw, *format, peek.ReasonWorkerOffline,
							fmt.Sprintf("cannot reach worker daemon at %s: %v", sock, ce.Cause), ExitBusinessError)
					}
					return PrintError(errw, *format, "peek_failed", err.Error(), ExitBusinessError)
				}
				for f := range frames {
					if f.Err != nil {
						code := mapPeekReason(f.Err.Reason)
						return PrintError(errw, *format, f.Err.Reason, f.Err.Message, code)
					}
					if f.Done {
						return ExitOK
					}
					fmt.Fprintln(out, f.Line)
				}
				return ExitOK
			}
		},
	}
}

// ---- helpers --------------------------------------------------------------

func mapInspectErr(err error, format string, errw io.Writer) ExitCode {
	switch {
	case errors.Is(err, query.ErrInspectKindUnknown):
		return PrintError(errw, format, "unknown_kind", err.Error(), ExitUsage)
	case errors.Is(err, query.ErrInspectIDRequired):
		return PrintError(errw, format, "usage_error", err.Error(), ExitUsage)
	case errors.Is(err, query.ErrInspectNotFound):
		return PrintError(errw, format, "not_found", err.Error(), ExitNotFound)
	}
	return PrintError(errw, format, "inspect_failed", err.Error(), ExitBusinessError)
}

// mapInspectClientErr translates a Client error from the inspect
// endpoint into the same reason+exit-code shape as mapInspectErr.
func mapInspectClientErr(err error, format string, errw io.Writer) ExitCode {
	if err == nil {
		return ExitOK
	}
	if errors.Is(err, ErrClientNotConfigured) || errors.Is(err, ErrServerUnreachable) {
		return PrintError(errw, format, "server_unreachable",
			err.Error()+" (start the server: agent-center server)", ExitBusinessError)
	}
	var ce *ClientError
	if errors.As(err, &ce) {
		switch ce.Code {
		case "unknown_kind", "invalid_input":
			return PrintError(errw, format, "unknown_kind", ce.Message, ExitUsage)
		case "not_found":
			return PrintError(errw, format, "not_found", ce.Message, ExitNotFound)
		case "missing_kind_or_id":
			return PrintError(errw, format, "usage_error", ce.Message, ExitUsage)
		}
		if ce.IsNotFound() {
			return PrintError(errw, format, "not_found", ce.Message, ExitNotFound)
		}
	}
	return PrintError(errw, format, "inspect_failed", err.Error(), ExitBusinessError)
}

// mapQueryClientErr is the Client-mode counterpart for the query
// endpoint; preserves the "limit_too_large" + "unknown_resource"
// human-facing reasons.
func mapQueryClientErr(err error, format string, errw io.Writer) ExitCode {
	if err == nil {
		return ExitOK
	}
	if errors.Is(err, ErrClientNotConfigured) || errors.Is(err, ErrServerUnreachable) {
		return PrintError(errw, format, "server_unreachable",
			err.Error()+" (start the server: agent-center server)", ExitBusinessError)
	}
	var ce *ClientError
	if errors.As(err, &ce) {
		switch ce.Code {
		case "limit_too_large":
			return PrintError(errw, format, "limit_too_large", ce.Message, ExitUsage)
		case "unknown_resource":
			return PrintError(errw, format, "unknown_resource", ce.Message, ExitUsage)
		case "missing_resource", "invalid_input":
			return PrintError(errw, format, "usage_error", ce.Message, ExitUsage)
		}
	}
	return PrintError(errw, format, "query_failed", err.Error(), ExitBusinessError)
}

// mapLogsClientErr mirrors the inline error mapping from the legacy
// LogsSvc.Open path for the Client-mode logs endpoint.
func mapLogsClientErr(err error, format string, errw io.Writer) ExitCode {
	if err == nil {
		return ExitOK
	}
	if errors.Is(err, ErrClientNotConfigured) || errors.Is(err, ErrServerUnreachable) {
		return PrintError(errw, format, "server_unreachable",
			err.Error()+" (start the server: agent-center server)", ExitBusinessError)
	}
	var ce *ClientError
	if errors.As(err, &ce) {
		switch ce.Code {
		case "unknown_kind":
			return PrintError(errw, format, "unknown_kind", ce.Message, ExitUsage)
		case "blob_not_found", "not_found":
			return PrintError(errw, format, "blob_not_found", ce.Message, ExitNotFound)
		case "blob_store_unavailable", "logs_svc_not_wired":
			return PrintError(errw, format, "blob_store_unavailable", ce.Message, ExitBusinessError)
		case "missing_kind_or_id":
			return PrintError(errw, format, "usage_error", ce.Message, ExitUsage)
		}
		if ce.IsNotFound() {
			return PrintError(errw, format, "blob_not_found", ce.Message, ExitNotFound)
		}
	}
	return PrintError(errw, format, "logs_failed", err.Error(), ExitBusinessError)
}

func mapPeekReason(reason string) ExitCode {
	switch reason {
	case peek.ReasonExecutionNotFound:
		return ExitNotFound
	case peek.ReasonInvalidRequest:
		return ExitUsage
	}
	return ExitBusinessError
}

func parseSince(s string) (time.Time, error) {
	if d, err := time.ParseDuration(s); err == nil {
		return time.Now().Add(-d), nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("--since/--until: %q is neither a duration nor RFC3339", s)
}

func printInspect(out io.Writer, format string, res query.InspectResult) ExitCode {
	if format == "json" {
		return writeJSON(out, res)
	}
	fmt.Fprintf(out, "Kind:    %s\n", res.Kind)
	fmt.Fprintf(out, "ID:      %s\n", res.ID)
	if data, ok := res.Data.(map[string]any); ok {
		keys := make([]string, 0, len(data))
		for k := range data {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if k == "messages" || k == "executions" || k == "recent_events" || k == "artifacts" || k == "mappings" || k == "tasks" {
				if arr, ok := data[k].([]any); ok {
					fmt.Fprintf(out, "%-12s %d entries\n", k+":", len(arr))
					continue
				}
			}
			fmt.Fprintf(out, "%-12s %v\n", k+":", data[k])
		}
	}
	return ExitOK
}

func printQueryResult(out io.Writer, format string, res query.QueryResult) ExitCode {
	if format == "json" {
		return writeJSON(out, res)
	}
	fmt.Fprintf(out, "Resource: %s  (%d rows)\n", res.Resource, len(res.Items))
	if res.NextCursor != "" {
		fmt.Fprintf(out, "Next cursor: %s\n", res.NextCursor)
	}
	for _, it := range res.Items {
		if m, ok := it.(map[string]any); ok {
			fmt.Fprintf(out, "  - %v\n", oneLineMap(m))
		} else {
			fmt.Fprintf(out, "  - %v\n", it)
		}
	}
	return ExitOK
}

func printFleet(out, errw io.Writer, format string, snap query.FleetSnapshot) ExitCode {
	if format == "json" {
		return writeJSON(out, snap)
	}
	if len(snap.Warnings) > 0 {
		for _, w := range snap.Warnings {
			fmt.Fprintf(errw, "warning: %s\n", w)
		}
	}
	fmt.Fprintf(out, "FLEET SNAPSHOT (generated %s)\n", snap.GeneratedAt)
	fmt.Fprintln(out, "")
	fmt.Fprintf(out, "WORK ITEMS (%d)\n", len(snap.WorkItems))
	for _, wi := range snap.WorkItems {
		fmt.Fprintf(out, "  %s task=%s agent=%s status=%s activity=%q\n",
			wi.WorkItemID, wi.TaskID, wi.AgentID, wi.Status, wi.CurrentActivity)
	}
	fmt.Fprintf(out, "\nWORKERS (%d)\n", len(snap.Workers))
	for _, w := range snap.Workers {
		fmt.Fprintf(out, "  %s status=%s active=%d mappings=%d\n",
			w.WorkerID, w.Status, w.ActiveCount, w.MappingsCount)
	}
	fmt.Fprintf(out, "\nOPEN INPUT REQUESTS (%d)\n", len(snap.OpenInputRequests))
	for _, ir := range snap.OpenInputRequests {
		fmt.Fprintf(out, "  %s exec=%s urgency=%s q=%q\n",
			ir.InputRequestID, ir.TaskExecutionID, ir.Urgency, ir.Question)
	}
	fmt.Fprintf(out, "\nPENDING ISSUES (%d)\n", len(snap.PendingIssues))
	for _, i := range snap.PendingIssues {
		fmt.Fprintf(out, "  %s project=%s opener=%s title=%q\n",
			i.IssueID, i.ProjectID, i.Opener, i.Title)
	}
	return ExitOK
}

func printStats(out io.Writer, format string, res query.StatsResult) ExitCode {
	if format == "json" {
		return writeJSON(out, res)
	}
	fmt.Fprintf(out, "Scope: %s\n", res.Scope)
	if res.Since != "" {
		fmt.Fprintf(out, "Since: %s\n", res.Since)
	}
	fmt.Fprintln(out, "Counters:")
	keys := make([]string, 0, len(res.Counters))
	for k := range res.Counters {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(out, "  %-30s %d\n", k+":", res.Counters[k])
	}
	if len(res.Totals) > 0 {
		fmt.Fprintln(out, "Totals:")
		tkeys := make([]string, 0, len(res.Totals))
		for k := range res.Totals {
			tkeys = append(tkeys, k)
		}
		sort.Strings(tkeys)
		for _, k := range tkeys {
			fmt.Fprintf(out, "  %-30s %v\n", k+":", res.Totals[k])
		}
	}
	return ExitOK
}

func writeJSON(out io.Writer, v any) ExitCode {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return ExitBusinessError
	}
	out.Write(b)
	out.Write([]byte{'\n'})
	return ExitOK
}

func inspectKindNames() []string {
	out := make([]string, 0, len(query.AllInspectKinds))
	for _, k := range query.AllInspectKinds {
		out = append(out, string(k))
	}
	return out
}

func queryResourceNames() []string {
	out := make([]string, 0, len(query.AllQueryResources))
	for _, r := range query.AllQueryResources {
		out = append(out, string(r))
	}
	return out
}

func statsScopeNames() []string {
	out := make([]string, 0, len(query.AllStatsScopes))
	for _, s := range query.AllStatsScopes {
		out = append(out, string(s))
	}
	return out
}

func oneLineMap(m map[string]any) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for i, k := range keys {
		if i > 0 {
			b.WriteString(" ")
		}
		fmt.Fprintf(&b, "%s=%v", k, m[k])
	}
	return b.String()
}
