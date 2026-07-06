package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/agentruntime/executor"
	"github.com/oopslink/agent-center/internal/agentruntime/modelrouter"
)

// SubprocessJudge is the LLM difficulty judge (issue-93dd8daa ②, approach A): a
// ONE-SHOT subprocess on the cheap/fast orchestrator_model that reads the task goal +
// the catalog-annotated executor pool and picks the cheapest sufficient {cli, model}.
// It reuses the SAME claude CLI primitive/plumbing as the executor runner (model +
// auth flags + stream-json), never a parallel call path (guardrail 2).
//
// GUARDRAILS (pd, T950):
//  1. BOUNDED + fail-to-fallback: the subprocess runs under a timeout; a timeout /
//     nonzero exit / unparseable output returns an error so the router falls back to
//     the deterministic pool[0] — the judge NEVER blocks dispatch or hard-fails. This
//     keeps OFF/failure byte-identical to today.
//  3. STRUCTURED output: the judge asks for a single JSON object and parses it
//     tolerantly; a parse failure is guardrail-1 fallback. Every attempt is logged
//     fail-loud (consulted / picked / rationale / why-fallback) for post-hoc audit.
type SubprocessJudge struct {
	orchestratorModel string
	binary            string
	timeout           time.Duration
	// run executes the judge argv and returns its combined output. Injectable so tests
	// drive canned output / timeouts without a real CLI; nil → a real exec.
	run func(ctx context.Context, argv []string) (string, error)
	// log is the fail-loud decision sink (nil → no-op).
	log func(format string, args ...any)
}

// JudgeConfig wires a SubprocessJudge. OrchestratorModel + Binary are required; a
// zero Timeout defaults to defaultJudgeTimeout.
type JudgeConfig struct {
	OrchestratorModel string
	Binary            string // the claude CLI binary (same as the executor runner)
	Timeout           time.Duration
	Run               func(ctx context.Context, argv []string) (string, error)
	Log               func(format string, args ...any)
}

const defaultJudgeTimeout = 45 * time.Second

// NewSubprocessJudge builds a SubprocessJudge. Returns nil when it can't run (no
// orchestrator model or binary) so the caller wires NewRouter(nil) → pure pool[0]
// fallback (never a half-configured judge).
func NewSubprocessJudge(cfg JudgeConfig) *SubprocessJudge {
	if strings.TrimSpace(cfg.OrchestratorModel) == "" || strings.TrimSpace(cfg.Binary) == "" {
		return nil
	}
	to := cfg.Timeout
	if to <= 0 {
		to = defaultJudgeTimeout
	}
	run := cfg.Run
	if run == nil {
		run = realJudgeRun
	}
	return &SubprocessJudge{
		orchestratorModel: cfg.OrchestratorModel,
		binary:            cfg.Binary,
		timeout:           to,
		run:               run,
		log:               cfg.Log,
	}
}

func (j *SubprocessJudge) logf(format string, args ...any) {
	if j.log != nil {
		j.log(format, args...)
	}
}

// judgeVerdict is the JSON the judge subprocess is asked to emit.
type judgeVerdict struct {
	Difficulty string `json:"difficulty"`
	CLI        string `json:"cli"`
	Model      string `json:"model"`
	Rationale  string `json:"rationale"`
}

// Judge implements modelrouter.DifficultyJudge. Any failure returns an error (the
// router then falls back to pool[0] — guardrail 1); success returns the chosen
// {cli, model} (the router validates it is in AllowedExecutors and rejects otherwise).
func (j *SubprocessJudge) Judge(ctx context.Context, req modelrouter.JudgeRequest) (modelrouter.Judgment, error) {
	if len(req.AllowedExecutors) == 0 {
		return modelrouter.Judgment{}, modelrouter.ErrInconclusive
	}
	prompt := buildJudgePrompt(req)
	argv := judgeArgv(j.binary, j.orchestratorModel, prompt)

	cctx, cancel := context.WithTimeout(ctx, j.timeout)
	defer cancel()
	out, err := j.run(cctx, argv)
	if err != nil {
		// timeout / nonzero exit → fall back (guardrail 1). Fail-loud.
		j.logf("modelrouter judge: subprocess failed (model=%s): %v → pool fallback", j.orchestratorModel, err)
		return modelrouter.Judgment{}, fmt.Errorf("%w: subprocess: %v", modelrouter.ErrInconclusive, err)
	}
	result, _ := executor.ParseRunnerStream(out)
	if r := strings.TrimSpace(result); r != "" {
		out = r // prefer the extracted final result; fall through to raw if empty
	}
	v, perr := parseJudgeVerdict(out)
	if perr != nil {
		j.logf("modelrouter judge: unparseable output → pool fallback: %v", perr)
		return modelrouter.Judgment{}, fmt.Errorf("%w: parse: %v", modelrouter.ErrInconclusive, perr)
	}
	// Fail-loud decision log: consulted, what it picked, why.
	j.logf("modelrouter judge: difficulty=%q picked cli=%q model=%q rationale=%q",
		v.Difficulty, v.CLI, v.Model, v.Rationale)
	return modelrouter.Judgment{CLI: strings.TrimSpace(v.CLI), Model: strings.TrimSpace(v.Model)}, nil
}

// buildJudgePrompt renders the goal + the catalog-annotated pool into a single prompt
// asking for one JSON verdict. tier is free text → the LLM reads it as a capability
// description and prefers the cheapest sufficient model.
func buildJudgePrompt(req modelrouter.JudgeRequest) string {
	var b strings.Builder
	b.WriteString("You are a model-routing difficulty judge. Read the TASK and pick the CHEAPEST model from the POOL that is still capable enough. Reply with ONE JSON object and nothing else:\n")
	b.WriteString(`{"difficulty":"low|medium|high","cli":"<cli>","model":"<model>","rationale":"<short why>"}` + "\n\n")
	b.WriteString("TASK:\n")
	b.WriteString(strings.TrimSpace(req.Goal.Title))
	if d := strings.TrimSpace(req.Goal.Description); d != "" {
		b.WriteString("\n")
		b.WriteString(d)
	}
	b.WriteString("\n\nPOOL (choose exactly one cli+model pair from this list):\n")
	for _, c := range req.AllowedExecutors {
		fmt.Fprintf(&b, "- cli=%s model=%s", c.CLI, c.Model)
		if c.Tier != "" {
			fmt.Fprintf(&b, " tier=%q", c.Tier)
		}
		if c.InputCost > 0 || c.OutputCost > 0 {
			fmt.Fprintf(&b, " cost(in/out per MTok)=%.2f/%.2f", c.InputCost, c.OutputCost)
		}
		if c.ContextWindow > 0 {
			fmt.Fprintf(&b, " context=%d", c.ContextWindow)
		}
		b.WriteByte('\n')
	}
	b.WriteString("\nPrefer the lowest-cost model whose tier/capability covers the task's difficulty. Reply with only the JSON object.")
	return b.String()
}

// judgeArgv builds the one-shot claude argv — the SAME model/auth/output plumbing the
// executor runner uses (guardrail 2), minus the executor system prompt / session-id /
// workspace (a judge is a stateless reasoning call, not a task executor).
func judgeArgv(binary, model, prompt string) []string {
	return []string{
		binary,
		"-p", prompt,
		"--model", model,
		"--output-format", "stream-json",
		"--verbose",
		"--setting-sources", "user,project",
		"--allow-dangerously-skip-permissions",
		"--dangerously-skip-permissions",
		"--permission-mode", "bypassPermissions",
	}
}

// parseJudgeVerdict extracts the JSON verdict from the model's text output, tolerating
// leading/trailing prose or code fences by scanning for the outermost {...} object.
func parseJudgeVerdict(out string) (judgeVerdict, error) {
	s := strings.TrimSpace(out)
	i := strings.IndexByte(s, '{')
	k := strings.LastIndexByte(s, '}')
	if i < 0 || k <= i {
		return judgeVerdict{}, errors.New("no JSON object in judge output")
	}
	var v judgeVerdict
	if err := json.Unmarshal([]byte(s[i:k+1]), &v); err != nil {
		return judgeVerdict{}, err
	}
	if strings.TrimSpace(v.Model) == "" || strings.TrimSpace(v.CLI) == "" {
		return judgeVerdict{}, errors.New("judge verdict missing cli/model")
	}
	return v, nil
}

// realJudgeRun runs the judge argv with the process's own environment (inherits the
// worker's claude auth). ctx carries the timeout; on ctx expiry the process is killed.
func realJudgeRun(ctx context.Context, argv []string) (string, error) {
	if len(argv) == 0 {
		return "", errors.New("orchestrator: empty judge argv")
	}
	c := exec.CommandContext(ctx, argv[0], argv[1:]...)
	out, err := c.CombinedOutput()
	return string(out), err
}

// compile-time: SubprocessJudge satisfies the router port.
var _ modelrouter.DifficultyJudge = (*SubprocessJudge)(nil)
