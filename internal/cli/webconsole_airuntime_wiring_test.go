package cli

import "testing"

func TestNewAIRuntimeServiceRequiresRestartStableKey(t *testing.T) {
	if service := newAIRuntimeService(&App{}); service != nil {
		t.Fatal("AI Runtime import must be disabled without restart-stable key material")
	}
	app := &App{RuntimeImportValidationKey: []byte("0123456789abcdef0123456789abcdef")}
	if service := newAIRuntimeService(app); service == nil {
		t.Fatal("AI Runtime service not wired with a valid restart-stable key")
	}
}
