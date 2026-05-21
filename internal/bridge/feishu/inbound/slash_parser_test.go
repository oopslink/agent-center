package inbound

import (
	"errors"
	"testing"
)

func TestSlashCommandParser_NonSlash(t *testing.T) {
	p := NewSlashCommandParser()
	for _, body := range []string{
		"hello", "  free text", "  @bot please do x", "what",
	} {
		cmd, err := p.Parse(body)
		if cmd != nil || err != nil {
			t.Errorf("non-slash %q → cmd=%v err=%v", body, cmd, err)
		}
	}
}

func TestSlashCommandParser_Track(t *testing.T) {
	p := NewSlashCommandParser()
	cmd, err := p.Parse("/track T-42")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if cmd.Verb != SlashVerbTrack {
		t.Errorf("verb: %s", cmd.Verb)
	}
	if cmd.TaskID() != "T-42" {
		t.Errorf("task id: %s", cmd.TaskID())
	}
	if cmd.Raw != "/track T-42" {
		t.Errorf("raw mismatch: %s", cmd.Raw)
	}
}

func TestSlashCommandParser_TrackMissingArg(t *testing.T) {
	p := NewSlashCommandParser()
	cmd, err := p.Parse("/track")
	if !errors.Is(err, ErrSlashInsufficientArgs) {
		t.Fatalf("want ErrSlashInsufficientArgs, got %v", err)
	}
	if cmd == nil || cmd.Verb != SlashVerbTrack {
		t.Errorf("cmd should still be partially populated: %#v", cmd)
	}
}

func TestSlashCommandParser_Answer(t *testing.T) {
	p := NewSlashCommandParser()
	cmd, err := p.Parse("/answer I-7 B option text")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if cmd.Verb != SlashVerbAnswer {
		t.Errorf("verb: %s", cmd.Verb)
	}
	if cmd.InputRequestID() != "I-7" {
		t.Errorf("ir: %s", cmd.InputRequestID())
	}
	if cmd.AnswerChoice() != "B option text" {
		t.Errorf("choice: %s", cmd.AnswerChoice())
	}
}

func TestSlashCommandParser_AnswerInsufficient(t *testing.T) {
	p := NewSlashCommandParser()
	cmd, err := p.Parse("/answer I-7")
	if !errors.Is(err, ErrSlashInsufficientArgs) {
		t.Fatalf("want ErrSlashInsufficientArgs, got %v", err)
	}
	if cmd == nil {
		t.Fatal("expected partial cmd")
	}
}

func TestSlashCommandParser_Dispatch(t *testing.T) {
	p := NewSlashCommandParser()
	cmd, err := p.Parse("/dispatch project=p1 \"do x\"")
	if !errors.Is(err, ErrSlashFeatureDeferred) {
		t.Fatalf("want ErrSlashFeatureDeferred, got %v", err)
	}
	if cmd == nil || cmd.Verb != SlashVerbDispatch {
		t.Fatalf("cmd: %#v", cmd)
	}
}

func TestSlashCommandParser_UnknownVerb(t *testing.T) {
	p := NewSlashCommandParser()
	cmd, err := p.Parse("/wtf arg")
	if !errors.Is(err, ErrSlashUnknownVerb) {
		t.Fatalf("want ErrSlashUnknownVerb, got %v", err)
	}
	if cmd != nil {
		t.Errorf("unknown verb should return nil cmd")
	}
}

func TestSlashCommandParser_Empty(t *testing.T) {
	p := NewSlashCommandParser()
	for _, body := range []string{"/", "/   ", "/\t"} {
		_, err := p.Parse(body)
		if !errors.Is(err, ErrSlashEmpty) {
			t.Errorf("body %q → want ErrSlashEmpty, got %v", body, err)
		}
	}
}

func TestSlashCommandParser_MultiSpace(t *testing.T) {
	p := NewSlashCommandParser()
	cmd, err := p.Parse("/track    T-42    extra")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if cmd.TaskID() != "T-42" {
		t.Errorf("task id: %s", cmd.TaskID())
	}
	if len(cmd.Args) != 2 || cmd.Args[1] != "extra" {
		t.Errorf("args: %v", cmd.Args)
	}
}

func TestSlashCommandParser_LeadingWhitespace(t *testing.T) {
	p := NewSlashCommandParser()
	cmd, err := p.Parse("   /answer I-7 yes")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if cmd.Verb != SlashVerbAnswer || cmd.AnswerChoice() != "yes" {
		t.Errorf("cmd: %#v", cmd)
	}
}

func TestSlashCommandParser_NilCommandGetters(t *testing.T) {
	var c *SlashCommand
	if c.TaskID() != "" || c.InputRequestID() != "" || c.AnswerChoice() != "" {
		t.Error("nil getters should return empty")
	}
	other := &SlashCommand{Verb: SlashVerbDispatch}
	if other.TaskID() != "" || other.InputRequestID() != "" || other.AnswerChoice() != "" {
		t.Error("wrong-verb getters should return empty")
	}
}

func TestSlashCommandParser_UppercaseVerb(t *testing.T) {
	p := NewSlashCommandParser()
	cmd, err := p.Parse("/TRACK T-1")
	if err != nil {
		t.Fatalf("uppercase should be normalised: %v", err)
	}
	if cmd.Verb != SlashVerbTrack {
		t.Errorf("verb: %s", cmd.Verb)
	}
}
