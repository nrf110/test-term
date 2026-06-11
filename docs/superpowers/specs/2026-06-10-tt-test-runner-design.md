# `tt` — Terminal UI Test Runner: Design Spec

- **Date:** 2026-06-10
- **Status:** Approved design, pre-implementation
- **Intent:** Open-source product

## 1. Summary

`tt` is a terminal-UI test runner for polyglot projects. It auto-discovers tests
across multiple language ecosystems, presents them in a hierarchical tree view
with live status, and runs them locally or on a remote machine. A single engine
serves multiple observers: a local TUI, a remote TUI, and AI agents connected
over MCP all watch and drive the *same* test session.

### Goals

1. Support popular test frameworks across Node/Bun, Go, Python, and Rust ecosystems
   via a pluggable adapter system.
2. Auto-discover all tests in the current directory and subdirectories on startup.
3. Present a visual, tree-based view suited to hierarchical BDD/spec-style suites.
4. Run tests remotely while observing locally (e.g., a sandboxed devbox running an
   agent), and expose an MCP server so agents can trigger and read test runs.
5. Jump from a failing test to its source location in a configurable editor.

### Non-goals (v1)

- Watch mode / auto-rerun on file change (fast-follow after v1).
- Bun test and cargo test adapters (fast-follow; their parsers are the trickiest).
- External/out-of-process adapter plugins (interface is designed to allow them later).
- Coverage visualization, flaky-test tracking, historical runs/dashboards.

## 2. Language & framework choice

**Go + Bubble Tea** (with Lipgloss for styling and Bubbles for prebuilt components).

Rationale:
- The rendering workload is modest and bounded (a test tree of hundreds–thousands
  of nodes updated on discrete events), so Rust's immediate-mode rendering edge
  does not pay off here; both Bubble Tea and ratatui require viewport windowing
  for very large trees regardless.
- Familiarity yields velocity and long-term maintainability for a solo-maintained
  OSS project.
- Go has a gentle on-ramp for contributors (important for adapter PRs).
- Trivial static, cross-compiled single-binary distribution.
- Mature TUI ecosystem (Bubble Tea / Lipgloss / Bubbles) and an official Go MCP SDK.

## 3. Architecture

**Principle: one engine, many clients. The TUI is never special — it is just a
client of the engine, even locally.** This yields a single code path whether the
engine runs on the user's laptop or a remote devbox.

```
                         ┌─────────────────────────────────────────┐
                         │              tt engine (core)            │
                         │  ┌────────────┐   ┌───────────────────┐  │
   discovers/runs tests  │  │  Session   │   │   Adapter set     │  │
   on this machine  ◀────┼──│  state     │◀─▶│  go / vitest /    │  │
                         │  │  (the tree)│   │  pytest (+more)   │  │
                         │  └────────────┘   └───────────────────┘  │
                         │         ▲  emits events / takes commands  │
                         └─────────┼───────────────┬────────────────┘
                                   │               │
                      ┌────────────┴───┐     ┌─────┴──────────────┐
                      │  Client API    │     │   MCP server       │
                      │ (WS event      │     │ (tools over MCP,   │
                      │  stream +      │     │  Streamable HTTP)  │
                      │  commands)     │     │                    │
                      └───┬────────┬───┘     └─────────┬──────────┘
                          │        │                   │
                  ┌───────┴──┐  ┌──┴────────┐   ┌──────┴───────────┐
                  │ Local    │  │ Remote    │   │ AI agent         │
                  │ TUI      │  │ TUI       │   │ (local or in     │
                  │          │  │ (devbox)  │   │  sandboxed devbox)│
                  └──────────┘  └───────────┘   └──────────────────┘
```

### CLI: single binary `tt`, three modes

- `tt` (default) — discover tests in cwd, start an **in-process engine** bound to
  `127.0.0.1:<ephemeral>`, and open the TUI connected to it over the same client
  protocol used remotely. The common local case "just works."
- `tt serve [--addr host:port] [--token <secret>]` — headless engine + MCP + client
  API. This runs on a sandboxed devbox; an agent there talks MCP to it.
- `tt connect <addr> [--token <secret>]` — open the TUI against a remote engine to
  watch and trigger runs live.

Because even local mode goes engine → client API → TUI, the remote case is the
same code with a network transport; the protocol is dogfooded on every local run.

The engine owns the single source of truth (the session tree). Clients are
stateless renderers of streamed events, so multiple observers cannot disagree
about state.

## 4. Adapter contract

The engine speaks **one normalized vocabulary**; each adapter translates its
framework into that vocabulary. Nothing in the engine, TUI, or MCP layer knows
what a specific framework is.

### Adapter responsibilities (four verbs)

| Verb | What it does | Example |
|------|-------------|---------|
| `Detect(dir)` | Does this adapter apply here? | go.mod present / `vitest` in package.json / pytest config |
| `Discover(dir)` | List tests *without running* → build the tree | `go test -list`, `vitest list --json`, `pytest --collect-only -q` |
| `Run(dir, selection)` | Execute (all or subset) → stream events | parse `go test -json`, vitest JSON reporter, pytest JSON |
| `Locate(nodeID)` | Map a test → `file:line:col` for jump | from parsed output / collection metadata |

Adapters own their subprocess and parser; the engine owns the tree. A contributor
adds a framework by writing one package implementing these four methods, touching
nothing else.

### Adapter interface (Go sketch)

```go
type Adapter interface {
    Name() string
    Detect(dir string) (Detection, bool)
    Discover(ctx context.Context, dir string, emit func(event.Event)) error
    Run(ctx context.Context, dir string, sel Selection, emit func(event.Event)) error
    Locate(nodeID string) (event.Location, bool)
}
```

### Normalized event stream

```
RunStarted     { runID, scope }
NodeDiscovered { nodeID, parentID, name, kind: file|suite|test, location? }
NodeStarted    { nodeID }
NodeFinished   { nodeID, status: pass|fail|skip|error, durationMs,
                 failure?: { message, diff?, frames: [{file,line,col}] } }
Output         { nodeID?, stream: stdout|stderr, text }
RunFinished    { runID, summary }
```

**NodeID** is a stable, namespaced address used as the universal handle by every
layer, e.g. `vitest:src/auth.test.ts::login::rejects bad password` or
`go:./auth::TestLogin/bad_password`. The TUI highlights by it; "rerun this" sends
it; an agent references it over MCP.

### Discovery vs. run

Discovery and run are separate so the tree appears instantly on startup (before
anything runs). However, Go subtests and pytest parametrized cases are created at
runtime and cannot be fully discovered statically. The tree is therefore
"best-known at discovery, refined during run" — `NodeDiscovered` events may arrive
mid-run, which the event model handles naturally.

### Discovery across a polyglot tree

On startup the engine walks the directory tree, runs each adapter's `Detect`
across applicable subtrees (a monorepo may have Go + Vitest + pytest), and runs
`Discover` for each match. The top level of the tree is grouped by framework.

### Selection / rerun

`Selection` is a set of NodeIDs or a symbolic set (`all`, `failed`). Each adapter
translates it to framework flags:

- Go: `go test -run 'TestName/subtest' ./pkg`
- Vitest: `vitest run <file> -t "name"`
- pytest: `pytest "tests/test_x.py::TestClass::test_method"`

### Packaging

v1 ships **compiled-in Go adapters** (one package each, compiled into `tt`):
type-safe, fast, trivially testable, no IPC. The interface is designed so an
external-plugin loader (subprocess adapters speaking a JSON protocol) can be added
later without changing the contract.

## 5. TUI layout

**Layout B — stacked:** full-width hierarchical tree on top, failure-detail strip
below. Chosen because hierarchical BDD/spec names need horizontal room. May
gracefully degrade on very short terminals later.

Elements:
- **Header:** project path, connection indicator (`● local` / remote addr),
  summary counts (passed / failed / running / skipped), elapsed time.
- **Tree (top, full width):** expandable nodes grouped by framework, per-node status
  glyphs (✓ pass, ✗ fail, ⟳ running, ⊘ skipped), durations, aggregate roll-ups.
- **Detail (bottom strip):** selected failure's message, diff, stack frames, and a
  clickable `file:line:col`.
- **Footer:** keybinding hints.
- **Filter/search:** `/` to filter the tree.

Bubble Tea's `Update` is a pure `(model, msg) → (model, cmd)`, enabling headless
golden-snapshot tests of rendered views.

## 6. Engine ↔ client protocol, MCP surface, security

### Protocol

A **shared HTTP server hosts both** the MCP endpoint (`/mcp`, Streamable HTTP) and
the TUI client stream (`/ws`, WebSocket). Events are the normalized JSON from
§4; commands return over the same socket.

Rationale: MCP is already JSON-RPC over HTTP, so colocating means one listener,
one auth story, one thing to tunnel. JSON is hackable (alternate clients,
`websocat` debugging). It preserves the single code path: local `tt` starts the
engine on `127.0.0.1:<ephemeral>` and the TUI connects over WS exactly as the
remote case does — local and remote differ only by address.

Client → engine commands include: `run` (selection), `rerun_failed`, `cancel`,
and `subscribe` (initial tree snapshot + live event stream).

### MCP tool surface

| Tool | Purpose |
|------|---------|
| `list_tests` | Current tree + statuses (discovered structure) |
| `run_tests` | Run all, or a selection (nodeIDs / file / pattern / `failed`) |
| `get_results` | Summary + per-node statuses, filterable to failures |
| `get_failure` | Full detail for a node: message, diff, stack frames, `file:line` |
| `rerun_failed` | Re-run just the failing set |
| `cancel_run` | Stop an in-flight run |

Agent loop: `run_tests` → `get_results` → `get_failure` per failure → fix code →
`rerun_failed`. A connected TUI sees every one of these runs live because both are
clients of the same session.

### Security

The engine executes arbitrary test code, so the default posture is conservative:

- Binds **`127.0.0.1` by default** — not network-exposed unless opted in.
- Remote access via **SSH tunnel** (`ssh -L`): the agent talks to the engine
  locally on the devbox; the human tunnels in to observe. Zero TLS config.
- `--addr 0.0.0.0` **requires** a `--token` (shared secret sent as a header) and
  prints a clear warning.

## 7. Jump-to-editor

A single configurable mechanism satisfies all targets:

- A **command template** expands `{file}`, `{line}`, `{col}` and is exec'd, e.g.
  `code -g {file}:{line}`, `nvim +{line} {file}`, `goland --line {line} {file}`.
- Sensible presets for VS Code/Cursor, Neovim/Vim, JetBrains, and plain `$EDITOR`.
- Failure locations are always rendered as `path/to/file:line:col` so terminals
  with link detection (e.g., Warp) make them clickable into the user's editor.

Note on Warp specifically: Warp's URI scheme opens windows/tabs at a *directory*
path and launch configs, but has **no** parameter to open a file at a specific
line (open feature request warpdotdev/warp#9561). Warp *does* auto-detect
`file:line:col` patterns in output and open them in the configured editor. So no
Warp-specific integration is built or needed; the command template plus clickable
locations cover it.

The jump logic is one small, testable unit: resolve failure → fill template → exec.

## 8. Project structure

```
cmd/tt/              CLI entrypoint — root cmd = TUI, plus `serve`, `connect`
internal/event/      Normalized event types (the shared vocabulary)
internal/engine/     Session tree, run orchestration, event fan-out to clients
internal/adapter/    Adapter interface + registry
  gotest/  vitest/  pytest/    one package per framework
internal/server/     HTTP server: /mcp + /ws, bind/auth/token logic
internal/mcp/        MCP tool handlers (thin — call into engine)
internal/client/     Client-side protocol (consumed by the TUI)
internal/tui/        Bubble Tea models: layout(B), tree, detail, header, filter
internal/locate/     Jump-to-editor command-template expansion
internal/config/     Config file (~/.config/tt/config.toml): editor cmd, etc.
testdata/            Captured framework outputs + tiny example projects
```

## 9. Testing strategy

- **Adapters → golden-file tests.** Capture real `go test -json` / vitest-JSON /
  pytest-JSON output once, store under `testdata/`, feed to the parser, assert the
  normalized events. Deterministic and fast; makes framework-version output changes
  loud and obvious.
- **Engine → unit tests.** Feed event sequences, assert resulting tree, selection
  resolution, and state transitions (including mid-run `NodeDiscovered`).
- **TUI → `teatest` snapshots** of rendered views (headless).
- **Protocol / MCP → loopback integration.** Engine + WS server on `127.0.0.1`,
  test client asserts event round-trips and MCP tool results.
- **End-to-end → real example projects** under `testdata/projects/{go,vitest,pytest}`,
  running the actual `tt` in CI.

## 10. v1 build sequence

Each step is independently demoable; vertical slices first.

1. **Event model + engine core** — types, tree, in-process engine. Test-driven.
2. **Go adapter end-to-end** — Detect/Discover/Run/Locate via `go test -json`.
   Dogfood: `tt` runs its own tests.
3. **TUI (layout B)** — tree + detail against the in-process engine.
4. **Server + protocol** — extract the WS client API; TUI becomes a real client;
   add `tt serve` / `tt connect`.
5. **MCP server** — tools on the shared HTTP listener.
6. **Vitest + pytest adapters** — prove the contract across hierarchical styles.
7. **Jump-to-editor** — config + command template + clickable `file:line:col`.
8. **Polish** — filter/search, summary header, keybindings, config file.

## 11. Fast-follows (post-v1)

- Watch mode (file watching + change→test mapping).
- Bun test and cargo test adapters.
- External/out-of-process adapter plugins.
