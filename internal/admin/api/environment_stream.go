package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/oopslink/agent-center/internal/environment"
	"github.com/oopslink/agent-center/internal/environment/controlstream"
)

// envWorkerCommandsStreamHandler is the center-side SSE down-push of worker
// control commands (v2.7 D5 slice-1):
//
//	GET /admin/environment/worker/commands/stream?worker_id=X&after=N
//
// It rides the SAME bearer auth + the SAME WorkerControlEvent log as the poll
// endpoint (/admin/environment/worker/commands). The down-push is a low-latency
// alternative to polling; the poll path's delivery guarantees are preserved:
//
//   - offset-ordered: catch-up (CommandsAfter, ORDER BY offset) is ordered, and
//     live commands arrive in the bus in append order.
//   - at-least-once: NO command is dropped across the catch-up/live boundary
//     (see the race handling below), and a missed publish is recovered by the
//     next reconnect's catch-up (the log, not the bus, owns at-least-once).
//   - reconnect-by-offset: the daemon reconnects with ?after=<lastOffset>; the
//     catch-up IS the resume (Last-Event-ID is secondary — offset is truth).
func (s *Server) envWorkerCommandsStreamHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.EnvControlSvc == nil {
		writeError(w, http.StatusNotImplemented, "env_control_svc_not_wired", "")
		return
	}
	if d.ControlStreamBus == nil {
		// No bus wired → the poll endpoint is the path. Fail explicit, not silent.
		writeError(w, http.StatusNotImplemented, "control_stream_not_wired",
			"SSE down-push not enabled; use GET /admin/environment/worker/commands")
		return
	}
	workerID := r.URL.Query().Get("worker_id")
	if workerID == "" {
		writeError(w, http.StatusBadRequest, "missing_worker_id", "")
		return
	}
	after, err := parseAfter(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_after", err.Error())
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming_unsupported", "")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // disable nginx buffering
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	wid := environment.WorkerID(workerID)

	// ── catch-up/live RACE HANDLING (so NO command is lost or double-sent) ──
	//
	// 1. SUBSCRIBE FIRST. From now on every newly-appended command for this
	//    worker lands on sub.Ch AND in the bus ring — so a command committed
	//    AFTER our catch-up snapshot below cannot slip through the gap.
	sub := d.ControlStreamBus.Subscribe(wid)
	defer sub.Close()

	// 2. Read CATCH-UP from the log (CommandsAfter == the offset-driven resume).
	//    This is the authoritative ordered backlog up to the current log max,
	//    including any command appended-but-publish-missed before we subscribed.
	cmds, err := d.EnvControlSvc.CommandsAfter(r.Context(), wid, after)
	if err != nil {
		// Headers already sent; surface as an SSE error frame, not an HTTP code.
		writeStreamError(w, flusher, "catch_up_failed")
		return
	}
	// sentMax tracks the highest offset already written so the live phase can
	// DEDUP by offset: a command present in BOTH catch-up and the live buffer
	// (the overlap window) is sent exactly once.
	var sentMax int64 = after
	for _, c := range cmds {
		cmd := controlstream.CommandFromEvent(c)
		writeCommandFrame(w, cmd)
		if cmd.Offset > sentMax {
			sentMax = cmd.Offset
		}
	}
	flusher.Flush()

	// 3. Stream LIVE. Commands from sub.Ch with offset <= sentMax were already
	//    delivered in catch-up (or are out-of-order duplicates) → skip. This is
	//    the offset-dedup of the catch-up/live overlap.
	hb := time.NewTicker(sub.Heartbeat())
	defer hb.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-sub.Done():
			return
		case cmd, ok := <-sub.Ch:
			if !ok {
				return
			}
			if cmd.Offset <= sentMax {
				continue // already sent in catch-up / overlap → dedup by offset.
			}
			writeCommandFrame(w, cmd)
			sentMax = cmd.Offset
			flusher.Flush()
		case <-hb.C:
			// Heartbeat as a real data frame (no `id:` line so the offset cursor
			// is untouched), mirroring the webconsole SSE liveness fix.
			fmt.Fprint(w, "data: {\"type\":\"control.heartbeat\"}\n\n")
			flusher.Flush()
		}
	}
}

// parseAfter reads ?after=N (default 0). The resume cursor is the OFFSET.
func parseAfter(r *http.Request) (int64, error) {
	v := r.URL.Query().Get("after")
	if v == "" {
		return 0, nil
	}
	return strconv.ParseInt(v, 10, 64)
}

// writeCommandFrame writes one command as an SSE frame. `id:` carries the OFFSET
// (so a native EventSource's Last-Event-ID reflects the offset — secondary to
// the explicit ?after=, but kept consistent); `data:` is the JSON command that
// itself carries the offset (the daemon tracks OFFSET, not the SSE seq).
func writeCommandFrame(w http.ResponseWriter, cmd controlstream.Command) {
	body, _ := json.Marshal(cmd)
	fmt.Fprintf(w, "id: %d\ndata: %s\n\n", cmd.Offset, body)
}

// writeStreamError emits a terminal SSE error frame (headers are already sent).
func writeStreamError(w http.ResponseWriter, flusher http.Flusher, code string) {
	fmt.Fprintf(w, "event: error\ndata: {\"error\":%q}\n\n", code)
	flusher.Flush()
}
