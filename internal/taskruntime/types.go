// Package taskruntime hosts the TaskRuntime BC tactical types shared across
// task / execution / inputrequest sub-packages: typed IDs, enums, sentinel
// errors that span multiple AR boundaries.
//
// AR-specific types live in:
//   - internal/taskruntime/task        (Task AR)
//   - internal/taskruntime/execution   (TaskExecution AR + Artifact entity)
//   - internal/taskruntime/inputrequest(InputRequest AR)
//   - internal/taskruntime/dispatch    (DispatchEnvelope / Ack / Nack VO)
//   - internal/taskruntime/reconcile   (Reconcile VO + Service)
//   - internal/taskruntime/kill        (KillCoordinator)
//   - internal/taskruntime/timeoutscan (TimeoutScanner)
package taskruntime

// Typed IDs shared by the package (conventions § 0.3).
type (
	TaskID          string
	TaskExecutionID string
	InputRequestID  string
	ArtifactID      string
)

func (id TaskID) String() string          { return string(id) }
func (id TaskExecutionID) String() string { return string(id) }
func (id InputRequestID) String() string  { return string(id) }
func (id ArtifactID) String() string      { return string(id) }
