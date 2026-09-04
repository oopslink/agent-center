package mcphost

import "github.com/oopslink/agent-center/internal/agenttools"

// AgentFacingToolNames is the SOURCE-OF-TRUTH canonical set of MCP tool names the
// per-agent catalog (NewServer) exposes to the agent LLM. It exists to anchor the
// full-parity guard (TestAgentFacingToolParity): the guard asserts the live
// ListTools name-set EQUALS this list, so a tool added to the registration without
// being added here (or vice versa) fails CI — forcing a DELIBERATE decision about
// whether a new capability should be agent-facing.
//
// This closes the whole CLASS of the #285/#299 seam (a plan/admin handler written
// but never registered in the agent catalog → the agent LLM can't see it). The
// per-tool integration guards (TestPlanToolsRegistered) catch specific families;
// this catches ANY drift in either direction.
//
// When adding/removing an agent-facing tool: update BOTH the NewServer registration
// AND this list (and FilesSeamTools below if it moves bytes via the FileMover seam
// instead of the /admin/agent-tools/<name> proxy). The guard will tell you if you
// miss one.
var AgentFacingToolNames = agenttools.AgentFacingToolNames

// FilesSeamTools are the agent-facing tools that move BYTES through the FileMover
// seam (NewServer's Files dep) rather than proxying to an /admin/agent-tools/<name>
// HTTP endpoint via callAdmin. They are the legitimate EXCEPTION to the
// reverse-lockstep half of the parity guard: every other AgentFacingToolNames entry
// maps 1:1 to a /admin/agent-tools/<name> admin route, but these do not (download_file
// proxies to GET /admin/files/{ulid}). Keep this list minimal and explicit.
var FilesSeamTools = agenttools.FilesSeamTools
