package workerdaemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oopslink/agent-center/internal/taskruntime/dispatch"
)

func TestAssemblePrompt_WithHomeDirInstructions(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "instructions.md"),
		[]byte("Focus on test coverage. Use TDD."), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := AssemblePrompt(StaticSkillLoader{"worker-agent.md": []byte("# Base skill")}, AssemblePromptInput{
		Envelope: dispatch.DispatchEnvelope{
			EnvelopeVersion: dispatch.EnvelopeVersionV2,
			TaskTitle:       "fix x",
		},
		BaseSkill: "worker-agent.md",
		HomeDir:   dir,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "## Agent Instructions") {
		t.Fatalf("missing agent instructions section: %s", got)
	}
	if !strings.Contains(got, "Focus on test coverage") {
		t.Fatalf("instructions text missing: %s", got)
	}
	if !strings.Contains(got, "Base skill") {
		t.Fatalf("base skill missing: %s", got)
	}
}

func TestAssemblePrompt_HomeDirMissingFile_Silent(t *testing.T) {
	dir := t.TempDir() // empty dir, no instructions.md
	got, err := AssemblePrompt(StaticSkillLoader{"base": []byte("# base")}, AssemblePromptInput{
		Envelope:  dispatch.DispatchEnvelope{TaskTitle: "do"},
		BaseSkill: "base",
		HomeDir:   dir,
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "## Agent Instructions") {
		t.Fatalf("should skip agent instructions when file missing: %s", got)
	}
}

func TestAssemblePrompt_EmptyHomeDir_Skipped(t *testing.T) {
	got, err := AssemblePrompt(StaticSkillLoader{"b": []byte("# b")}, AssemblePromptInput{
		Envelope:  dispatch.DispatchEnvelope{TaskTitle: "do"},
		BaseSkill: "b",
		HomeDir:   "",
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "## Agent Instructions") {
		t.Fatal()
	}
}

func TestLoadHomeDirInstructions_EmptyHomeDir(t *testing.T) {
	data, err := loadHomeDirInstructions("")
	if err != nil || data != nil {
		t.Fatalf("expected (nil, nil), got (%v, %v)", data, err)
	}
}

func TestLoadHomeDirInstructions_MissingFile(t *testing.T) {
	dir := t.TempDir()
	data, err := loadHomeDirInstructions(dir)
	if err != nil || data != nil {
		t.Fatalf("expected (nil, nil), got (%v, %v)", data, err)
	}
}

func TestLoadHomeDirInstructions_FileExists(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "instructions.md"), []byte("body"), 0o600)
	data, err := loadHomeDirInstructions(dir)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "body" {
		t.Fatalf("data: %s", data)
	}
}

// Permission-denied (or similar non-IsNotExist error) propagates.
func TestLoadHomeDirInstructions_NonNotExistError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("running as root: chmod 0 bypassed")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "instructions.md")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Make file unreadable.
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })
	_, err := loadHomeDirInstructions(dir)
	if err == nil {
		t.Fatal("expected permission error to propagate")
	}
}
