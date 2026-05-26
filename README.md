<h1 align="center">agent-center</h1>

<p align="center">
  <strong>English</strong>
  &nbsp;·&nbsp;
  <a href="./README.zh-CN.md">简体中文</a>
  &nbsp;·&nbsp;
  <a href="./docs/index.md">Docs</a>
  &nbsp;·&nbsp;
  <a href="./docs/design/roadmap.md">Roadmap</a>
  &nbsp;·&nbsp;
  <a href="./docs/deployment/v2.4-first-mile.md">Deploy Guide</a>
  &nbsp;·&nbsp;
  <a href="./CHANGELOG.md">CHANGELOG</a>
</p>

<br/>

<h3 align="center">A personal AI agent dispatch center.</h3>
<p align="center">Run one server. Attach workers from any machine. Agents run wherever — every conversation and decision lands on a thread you can trace.</p>

<br/>

> [!TIP]
> **Conversation is the product spine, not a log.** Tasks, Issues, decisions, progress — all hang off Conversation threads. This is the deepest difference between agent-center and the "agents are scripts" mindset: every dispatch, every InputRequest, every artifact is recoverable in its original thread.

> [!IMPORTANT]
> **v2.4.0 shipped (2026-05-26)** — First-mile deployment complete: one command installs center, one command installs worker (the Web Console gives you the exact command to copy-paste). Multi-worker per machine, atomic upgrade with auto-rollback. See [CHANGELOG](./CHANGELOG.md).

<br/>

## Install

`agent-center` is now **install-once-and-go**. Extract the release tarball, then run one command to install the center and another to install each worker:

```bash
# Install the center (on the host machine)
cd agent-center-v2.4.0-<os>-<arch>/
./install center
# The last line prints a Web Console URL — open it in a browser.

# Install a worker (same machine or any other machine)
# In the Web Console, click "+ Add Worker", type a friendly name,
# copy the generated command, and run it on the worker machine:
./install worker --bootstrap=... --worker-id=... --worker-name=... --token=...
```

Supported on macOS (this cycle's acceptance target) and Linux (systemd unit installed automatically; full validation deferred). **Upgrades use the same command** — extract the new tarball, run `./install center`, and the installer atomically swaps the symlink with auto-rollback if the new version fails its health probe.

| Command | What it does |
|---|---|
| `agent-center install center` | Install / upgrade the center (idempotent) |
| `agent-center install worker` | Install / upgrade a worker daemon |
| `agent-center server` | Run the center in the foreground (development) |
| `agent-center help` | Full command tree (subject-verb grouped) |
| `agent-center task create <proj> <title>` | Create a task |
| `agent-center issue open <proj> <title>` | Open an issue to discuss before doing |
| `agent-center ps [--watch]` | Live fleet view (worker × execution) |
| `agent-center inspect <kind> <id>` | Inspect a single entity (task / issue / worker / ...) |

Full CLI surface: [CLI subcommands reference](./docs/design/implementation/03-cli-subcommands.md). Full deploy walkthrough: [v2.4 first-mile guide](./docs/deployment/v2.4-first-mile.md).

<br/>

## What it solves

| Pain | How agent-center handles it |
|---|---|
| Multiple agents on multiple machines, state scattered across N terminals | One server collects everything; `/fleet` shows every worker × execution × pending IR in real time |
| Agent stops mid-task to ask you something ("should I commit?") | InputRequest is a first-class concept — answer in a Web Console card and the agent resumes |
| Hard to trace what the agent did, why, and on whose authority | Every Task / Issue gets a Conversation thread; dispatch, decision, progress, and artifacts all land in it |
| Skill / MCP config scattered across each agent's repo | AgentInstance is a first-class AR: instructions + MCP servers + skill mounts are bound to the agent identity |
| Credentials | UserSecret BC, AES-256, plaintext-never-echo; agents reference secrets by `secret:<name>` |
| Multi-host deployment | v2.3 multi-host TCP+TLS (SSH-style fingerprint pinning) + v2.4 one-command first-mile |

<br/>

## Core concepts

Each is a noun your users will learn, backed by a DDD aggregate / value object / event / service:

| Concept | One-line definition |
|---|---|
| **Task** | A unit of work you (or Supervisor) created; retryable — each retry is a new TaskExecution, task identity stays |
| **Issue** | A topic to discuss ("should we use X or Y?"); the conclusion can spawn 0, 1, or N Tasks |
| **Conversation** | A message thread attached to a Task / Issue / Channel / DM — the product spine |
| **Worker** | A machine running agents (local or remote); one machine can host multiple workers (v2.4) |
| **AgentInstance** | A named, persistent agent identity ("the coder on my MBP") with instructions + MCP + skills |
| **Supervisor** | A built-in agent that reads Conversation context and decides what to dispatch next — not a "brain," just another agent with logs |
| **InputRequest** | Agent blocks mid-execution asking you to decide; you answer in the Web Console and the agent resumes |
| **Project** | The container Tasks belong to; a worker can be mapped to multiple Projects |
| **Artifact** | The output of an execution (PR URL, file, report) |
| **Memory** | Supervisor's persistent notes (markdown files, scoped per project / task / global) |

Full ubiquitous-language glossary: [bounded contexts § 1](./docs/design/architecture/strategic/03-bounded-contexts.md#-1-通用语言ubiquitous-language).

<br/>

## Design

`agent-center` follows [Domain-Driven Design](https://en.wikipedia.org/wiki/Domain-driven_design) with **seven Bounded Contexts**:

- **TaskRuntime** — Task, TaskExecution, Dispatch, Kill
- **Discussion** — Issue, IssueComment, Conclude
- **Workforce** — Worker, AgentInstance, Project, WorkerProjectMapping
- **Cognition** — Supervisor, Invocation, Memory
- **Observability** — Event, Trace, Stats
- **Conversation** — Channel, DM, Thread, Message
- **SecretManagement** — UserSecret + master key

Cross-BC interactions go through events / RPC; no shared physical tables (see [§ 9.z](./docs/rules/conventions.md)). All persistence is gated by each BC's Application Service — the transport (unix socket / TCP+TLS) is an implementation detail; **domain invariants always live behind the AppService**.

Documentation entry points:
- [Design overview](./docs/design/README.md)
- [DDD blueprint (plan + status)](./docs/design/ddd-blueprint.md)
- [Strategic / domain vision](./docs/design/architecture/strategic/00-domain-vision.md)
- [Tactical / per-BC overviews](./docs/design/architecture/tactical/)
- [ADR index](./docs/design/decisions/)
- [Project conventions (must-read)](./docs/rules/conventions.md)
- [Roadmap (deferred features)](./docs/design/roadmap.md)

<br/>

## Development

### Prerequisites

- **Go** 1.22+
- **Node.js** 20+ with **pnpm** (for the Web Console SPA)
- **macOS** or **Linux** (Windows untested)

### Build

```bash
make build                  # frontend (vite) + backend (go) + worker-daemon + fakeagent
                            # produces ./bin/{agent-center, agent-center-worker-daemon, fakeagent}

VERSION=v2.4.1 make build   # build with a specific version (default v2.4.0)
```

The frontend SPA is built first (`web/` → `internal/webconsole/spa/dist/`) and then embedded into the Go binary via `go:embed`, so a single binary ships the full Web Console.

For SPA development, run the vite dev server separately and proxy `/api` to the loopback Go server — vite hot-reloads and the embedded chunk in the binary is ignored:

```bash
pnpm --dir web install      # one-time
pnpm --dir web run dev      # http://localhost:5173 with proxy → 127.0.0.1:7100
```

### Test, lint, smoke

```bash
make test            # go test ./...
make cover           # go test with coverage report
make cover-html      # render coverage as ./coverage.html
make vet             # go vet ./...
make lint            # vet + lint-vendor + lint-mock-default + lint-doc-impl-drift
                     # (enforces conventions § 0.4 architectural rules)
make smoke           # fresh-binary deploy + drive a task to done — § 0.4 #4 gate
```

End-to-end tests (Playwright):

```bash
make e2e-install     # one-time: pnpm install + chromium download
make e2e             # full E2E suite, including deployed-pipeline spec
```

### Project layout

```
agent-center/
├── cmd/
│   ├── agent-center/               # main binary (server + CLI + install command)
│   ├── worker-daemon/              # worker daemon (separate binary)
│   └── fakeagent/                  # smoke-test agent (no LLM)
├── internal/                       # one subpackage per Bounded Context
│   ├── taskruntime/  discussion/   # plus admin transport, webconsole, cli, ...
│   ├── workforce/    cognition/
│   ├── observability/ conversation/
│   ├── secret/       admintoken/
│   └── ...
├── web/                            # React SPA (vite + TS + Tailwind)
│   └── src/                        # → internal/webconsole/spa/dist via go:embed
├── docs/
│   ├── design/                     # DDD architecture, ADRs, requirements
│   ├── plans/                      # phase / cycle plans + audits
│   ├── deployment/                 # deploy guides per version
│   ├── operations/                 # runbooks
│   └── rules/conventions.md        # cross-cutting design rules — read this
├── sites/                          # VitePress docs site (sources from docs/)
├── tests/                          # E2E suites
├── contrib/                        # legacy install scripts (kept for reference)
└── Makefile
```

### Conventions

Read [`docs/rules/conventions.md`](./docs/rules/conventions.md) before contributing. Two rules that catch new contributors most often:

- **§ 0.4 — AppService is the only entry to domain state.** No process other than the server reads SQLite directly; CLI / worker / web all go through the admin transport.
- **§ 0.6 — Don't infer design intent without evidence.** Describe what *is* (observation) and what's *capable* (model). Don't bridge to "the system was designed to assume X" unless you can `grep` for it.

### Packaging (release tarballs)

`make release` builds a self-contained tarball for the host platform that's ready to feed to `./install`:

```bash
make clean-dist     # optional: wipe previous tarballs
make release        # → dist/agent-center-v<ver>-<os>-<arch>.tar.gz + sha256

# what it does:
#   1. make build (frontend + backend + worker-daemon)
#   2. assembles dist/agent-center-v<ver>-<os>-<arch>/ with bin/ +
#      install wrapper + LICENSE + README.md
#   3. tar -czf and prints sha256 + extract/verify recipe
```

Cross-platform tarballs (Linux × amd64/arm64 from a Mac build host, etc.), signing, GitHub Releases publishing, and CI are all deferred to the v3 "Deployment as Product" theme. For now `make release` covers the local-platform case, which is what you need to test the install flow end-to-end before promoting a release.

### Local docs site

The `sites/` directory is a VitePress scaffold whose markdown sources point straight at `docs/`:

```bash
cd sites/
npm install
npm run dev      # http://localhost:5173 with markdown hot-reload
npm run build    # static output → sites/.vitepress/dist/, copy anywhere
```

<br/>

## Contributing & feedback

This is currently a single-author project. If you'd like to contribute:

- **Bugs and design discussion** — open a GitHub Issue
- **Code contributions** — read [`docs/rules/conventions.md`](./docs/rules/conventions.md) first (§ 0.4 AppService discipline + § 0.6 layer discipline catch most issues)
- **Roadmap input** — point to a row in [Roadmap](./docs/design/roadmap.md) or open a Discussion

The VitePress site under `sites/` will be the canonical entry point once deployed; for now please browse `docs/` directly in the repo.
