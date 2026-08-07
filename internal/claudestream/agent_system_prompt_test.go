package claudestream

import (
	"strings"
	"testing"
)

func TestAgentSystemPromptsIncludeReportingFormatRule(t *testing.T) {
	prompts := map[string]string{
		"work_queue":   AgentWorkQueueSystemPrompt,
		"orchestrator": OrchestratorSystemPrompt,
	}
	want := []string{
		"== Reporting format ==",
		"【完成】",
		"【阻塞】",
		"【风险】",
		"✓已做",
		"⏸等待",
		"🛡️防护",
		"Do not chain different facts with commas",
		"Write one atomic fact per sentence",
		"现在的决策是什么？谁等什么？",
	}
	for name, prompt := range prompts {
		for _, w := range want {
			if !strings.Contains(prompt, w) {
				t.Fatalf("%s prompt missing reporting guidance %q", name, w)
			}
		}
	}
}
