package orchestrator

import (
	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/idgen"
)

// ulidMinter is the production IDMinter: it mints path-safe "<prefix>-<8hex>"
// entity ids via idgen (collision-free, no path separators — satisfies
// executor.validateExecutorID).
type ulidMinter struct {
	gen idgen.Generator
}

// NewULIDMinter builds the production IDMinter over an idgen generator seeded with
// clk. A nil clock defaults to the system clock.
func NewULIDMinter(clk clock.Clock) IDMinter {
	if clk == nil {
		clk = clock.SystemClock{}
	}
	return &ulidMinter{gen: idgen.NewGenerator(clk)}
}

func (m *ulidMinter) NewExecutorID() string { return m.gen.NewEntityID("exec") }
func (m *ulidMinter) NewProblemID() string  { return m.gen.NewEntityID("problem") }
