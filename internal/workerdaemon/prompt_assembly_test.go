package workerdaemon

import (
	"strings"
	"testing"

	"github.com/oopslink/agent-center/internal/taskruntime/dispatch"
)

func TestAssemblePrompt_Full(t *testing.T) {
	loader := StaticSkillLoader{
		"worker-agent.md": []byte("# Worker agent\nFollow rules."),
		"extra.md":        []byte("# Extra\nAlso this."),
	}
	out, err := AssemblePrompt(loader, AssemblePromptInput{
		BaseSkill: "worker-agent.md",
		Envelope: dispatch.DispatchEnvelope{
			TaskTitle:       "do thing",
			TaskDescription: "the description",
			ExtraSkillFiles: []string{"extra.md"},
		},
		ConstraintsExtra: "be careful",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Worker agent") {
		t.Fatal("missing base skill")
	}
	if !strings.Contains(out, "Extra") {
		t.Fatal("missing extra skill")
	}
	if !strings.Contains(out, "## Task") {
		t.Fatal("missing task section")
	}
	if !strings.Contains(out, "the description") {
		t.Fatal("missing description")
	}
	if !strings.Contains(out, "## Constraints") {
		t.Fatal("missing constraints")
	}
}

func TestAssemblePrompt_NilLoader(t *testing.T) {
	if _, err := AssemblePrompt(nil, AssemblePromptInput{}); err == nil {
		t.Fatal("expected error")
	}
}

func TestAssemblePrompt_EmptyResult(t *testing.T) {
	loader := StaticSkillLoader{}
	if _, err := AssemblePrompt(loader, AssemblePromptInput{}); err == nil {
		t.Fatal("expected empty error")
	}
}

func TestStaticSkillLoader_MissingFile(t *testing.T) {
	if _, err := (StaticSkillLoader{}).Load("missing"); err == nil {
		t.Fatal("expected not exist")
	}
}

func TestFSSkillLoader_Nil(t *testing.T) {
	if _, err := (FSSkillLoader{}).Load("x"); err == nil {
		t.Fatal("expected error")
	}
}
