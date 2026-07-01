package workerdaemon

import "github.com/oopslink/agent-center/internal/agentsupervisor"

func cloneEnvVars(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func runtimeAgentEnv(agentID, displayName string, profileEnv map[string]string) map[string]string {
	out := agentsupervisor.GitIdentityEnv(agentID)
	if out == nil {
		out = map[string]string{}
	}
	for k, v := range agentsupervisor.DisplayNameEnv(displayName) {
		out[k] = v
	}
	for k, v := range profileEnv {
		out[k] = v
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
