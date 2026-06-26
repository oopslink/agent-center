package workerdaemon

import (
	"time"

	"github.com/oopslink/agent-center/internal/workerdaemon/taskexec"
)

// defaultGCInterval is the minimum interval between GC sweeps (design §11.3).
const defaultGCInterval = 1 * time.Hour

// maybeRunGC runs the task directory GC for all known agents if at least
// cfg.GCInterval has elapsed since the last sweep. It is called from OnTick
// (periodic) and ReconcileOnBoot (on daemon start).
func (c *AgentController) maybeRunGC(now time.Time) {
	if c.cfg.TaskDirManager == nil {
		return
	}

	interval := c.cfg.GCInterval
	if interval <= 0 {
		interval = defaultGCInterval
	}

	c.mu.Lock()
	if !c.lastGCAt.IsZero() && now.Sub(c.lastGCAt) < interval {
		c.mu.Unlock()
		return
	}
	// Snapshot agent IDs under the lock.
	ids := make([]string, 0, len(c.agents))
	for id := range c.agents {
		ids = append(ids, id)
	}
	c.mu.Unlock()

	gcCfg := taskexec.DefaultGCConfig()
	for _, id := range ids {
		_, tasksDir, _, err := c.agentPaths(id)
		if err != nil {
			continue
		}
		result := taskexec.RunGC(tasksDir, gcCfg, now)
		if result.AbortedCleaned > 0 || result.DoneCleaned > 0 || result.LeftoverCleaned > 0 || len(result.Errors) > 0 {
			c.log("gc agent=%s aborted=%d done=%d leftover=%d errors=%d",
				id, result.AbortedCleaned, result.DoneCleaned, result.LeftoverCleaned, len(result.Errors))
		}
	}

	c.mu.Lock()
	c.lastGCAt = now
	c.mu.Unlock()
}
