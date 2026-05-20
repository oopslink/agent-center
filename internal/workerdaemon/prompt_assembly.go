package workerdaemon

import (
	"errors"
	"io/fs"
	"strings"

	"github.com/oopslink/agent-center/internal/taskruntime/dispatch"
)

// SkillLoader abstracts reading a worker-agent.md skill file (or other
// skills). Production wires an *embed.FS reader.
type SkillLoader interface {
	Load(name string) ([]byte, error)
}

// FSSkillLoader reads from any fs.FS rooted at the skill directory.
type FSSkillLoader struct{ FS fs.FS }

// Load reads a skill file by name.
func (l FSSkillLoader) Load(name string) ([]byte, error) {
	if l.FS == nil {
		return nil, errors.New("workerdaemon: skill FS not configured")
	}
	return fs.ReadFile(l.FS, name)
}

// StaticSkillLoader is a tiny in-memory loader used by tests.
type StaticSkillLoader map[string][]byte

// Load returns the bytes for the named skill or nil-error when missing.
func (s StaticSkillLoader) Load(name string) ([]byte, error) {
	b, ok := s[name]
	if !ok {
		return nil, fs.ErrNotExist
	}
	return b, nil
}

// AssemblePromptInput captures parameters for prompt assembly.
type AssemblePromptInput struct {
	Envelope        dispatch.DispatchEnvelope
	BaseSkill       string   // file name for worker-agent.md (typically "worker-agent.md")
	ConstraintsExtra string  // optional injected constraints
}

// AssemblePrompt builds the final prompt per agent-harness/01-prompt-
// assembly. Returns the concatenated string fed to the agent CLI via
// `--prompt` / `-p`.
func AssemblePrompt(loader SkillLoader, in AssemblePromptInput) (string, error) {
	if loader == nil {
		return "", errors.New("workerdaemon: nil skill loader")
	}
	var sb strings.Builder
	base, err := loader.Load(in.BaseSkill)
	if err == nil {
		sb.Write(base)
		sb.WriteString("\n\n")
	}
	for _, p := range in.Envelope.ExtraSkillFiles {
		extra, err := loader.Load(p)
		if err == nil {
			sb.Write(extra)
			sb.WriteString("\n\n")
		}
	}
	if in.ConstraintsExtra != "" {
		sb.WriteString("## Constraints\n")
		sb.WriteString(in.ConstraintsExtra)
		sb.WriteString("\n\n")
	}
	if in.Envelope.TaskTitle != "" {
		sb.WriteString("## Task\n")
		sb.WriteString(in.Envelope.TaskTitle)
		sb.WriteString("\n\n")
	}
	if in.Envelope.TaskDescription != "" {
		sb.WriteString("## Description\n")
		sb.WriteString(in.Envelope.TaskDescription)
		sb.WriteString("\n")
	}
	out := strings.TrimSpace(sb.String())
	if out == "" {
		return "", errors.New("workerdaemon: assembled prompt is empty")
	}
	return out, nil
}
