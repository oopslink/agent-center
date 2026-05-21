// Package supervisor contains the CLI handler + supporting types for
// `agent-center supervisor` (subcommand entry point used by the
// SupervisorSpawner). Skill files embedded via Go embed.FS.
package supervisor

import (
	"embed"
	"errors"
	"io/fs"
	"path/filepath"
)

//go:embed skills/supervisor.md
var skillsFS embed.FS

// SkillContent returns the embedded supervisor.md content.
func SkillContent() ([]byte, error) {
	return fs.ReadFile(skillsFS, "skills/supervisor.md")
}

// WriteSkillTo writes the embedded supervisor.md into dir and returns its
// absolute path. Used by the subprocess to materialise a skill file claude
// can `--skill` reference.
func WriteSkillTo(dir string) (string, error) {
	if dir == "" {
		return "", errors.New("supervisor: dir required")
	}
	content, err := SkillContent()
	if err != nil {
		return "", err
	}
	p := filepath.Join(dir, "supervisor.md")
	if err := writeFile(p, content); err != nil {
		return "", err
	}
	return p, nil
}
