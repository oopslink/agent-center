package workerdaemon

import (
	"github.com/oopslink/agent-center/internal/taskruntime"
	"github.com/oopslink/agent-center/internal/taskruntime/reconcile"
)

func reconcileResponseHelper() reconcile.Response {
	return reconcile.Response{
		Stale:   []taskruntime.TaskExecutionID{"E-1"},
		Unknown: []taskruntime.TaskExecutionID{"E-2"},
	}
}
