# `tt` — Implementation Plan

- **Date:** 2026-06-10
- **Design spec:** [`docs/superpowers/specs/2026-06-10-tt-test-runner-design.md`](../specs/2026-06-10-tt-test-runner-design.md)
- **Approach:** Test-driven, vertical slices first. Each phase is independently
  demoable and lands behind passing tests before the next begins.

## Conventions

- **TDD loop per task:** write a failing test → make it pass → refactor. Adapter
  parsers use golden-file fixtures captured from real framework output.
- **Module path:** `github.com/nickfisher/tt` (adjust if the repo lands elsewhere).
- **Go version:** latest stable (1.23+).
- **Each phase ends green:** `go test ./...` passes, `go vet ./...` clean, and the
  phase's demo command works.
- **Commits:** one focused commit per task or small task group, conventional style.

---

## Phase 0 — Project scaffolding

**Goal:** A compiling, testable Go module with CI and tooling in place.

Tasks:
1. `go mod init github.com/nickfisher/tt`; add `cmd/tt/main.go` with a stub root
   command (Cobra) that prints version and exits.
2. Add dependencies: `cobra` (CLI), `bubbletea`/`lipgloss`/`bubbles` (TUI),
   `gorilla/websocket` or `nhooyr/coder websocket` (WS), the official Go MCP SDK,
   `BurntSushi/toml` (config).
3. Add `Makefile`/`Taskfile` targets: `build`, `test`, `lint`, `run`.
4. Add GitHub Actions CI: `go test ./...`, `go vet`, `golangci-lint`, build matrix
   (linux/darwin, amd64/arm64).
5. Add `README.md` stub and `LICENSE` (MIT or Apache-2.0 — confirm with maintainer).

**Acceptance:** `go build ./... && go test ./...` succeed; `tt --version` runs; CI green.

---

## Phase 1 — Event model + engine core

**Goal:** The normalized vocabulary and the in-process session engine, fully
test-driven with no UI.

Tasks:
1. `internal/event`: define `Event` types (`RunStarted`, `NodeDiscovered`,
   `NodeStarted`, `NodeFinished`, `Output`, `RunFinished`), `Status` enum,
   `Location`, `Failure`, `Frame`. JSON tags for wire use. Round-trip tests.
2. `internal/engine`: `Session` holding the tree (`Node{ID, ParentID, Name, Kind,
   Status, Duration, Failure, Children}`). Implement `Apply(event.Event)` that
   mutates the tree, including:
   - mid-run `NodeDiscovered` (Go subtests / pytest params) inserting under a parent,
   - status roll-up to ancestors (a suite is failing if any descendant failed),
   - summary counts.
3. `internal/engine`: event fan-out — `Subscribe() <-chan event.Event` returning a
   live stream plus an initial snapshot; multiple subscribers supported.
4. `internal/engine`: `Selection` type (`All`, `Failed`, explicit `[]NodeID`) and
   resolution against the current tree.

**Tests:** feed crafted event sequences; assert tree shape, roll-ups, summaries,
selection resolution, and multi-subscriber fan-out.

**Acceptance:** engine package fully unit-tested; no UI; `go test ./internal/...` green.

---

## Phase 2 — Go adapter, end-to-end (dogfood)

**Goal:** A real adapter so `tt` can run its own tests via the engine.

Tasks:
1. `internal/adapter`: define `Adapter` interface (`Name`, `Detect`, `Discover`,
   `Run`, `Locate`), `Detection`, and a `Registry`.
2. `internal/adapter/gotest`:
   - `Detect`: presence of `go.mod` / `*_test.go`.
   - `Discover`: `go test -list '.*' ./...` → file/package/test nodes (top-level
     funcs; subtests deferred to run).
   - `Run`: spawn `go test -json -run <selection> ./...`, stream-parse the
     `test2json` events into normalized events (handle `run`/`pass`/`fail`/`skip`/
     `output`, subtests via `Test/Sub` naming → nested `NodeDiscovered`).
   - `Locate`: parse `file:line` from failure output / `t.Fatal` frames.
3. Golden-file tests: capture real `go test -json` output under
   `testdata/gotest/*.jsonl`; assert normalized event streams. Cover pass, fail,
   skip, subtests, build failure, panic.
4. A tiny example module under `testdata/projects/go/` for end-to-end runs.

**Acceptance:** `go test ./internal/adapter/gotest/...` green from goldens; an
integration test runs the example project and asserts the resulting tree.

---

## Phase 3 — TUI (layout B) against the in-process engine

**Goal:** A usable terminal UI: stacked full-width tree + detail strip, driven by
the engine in-process.

Tasks:
1. `internal/tui`: root Bubble Tea model wiring header / tree / detail / footer;
   subscribe to engine events via a `tea.Cmd` that pumps the event channel into
   `tea.Msg`s.
2. Tree component: expand/collapse, cursor navigation, status glyphs (✓ ✗ ⟳ ⊘),
   durations, aggregate roll-ups, framework grouping at the top level.
3. Detail component: render selected failure (message, diff, frames, clickable
   `file:line:col`).
4. Header: project path, `● local`, summary counts, elapsed time. Footer: keymap.
5. Filter: `/` to filter the tree by substring.
6. Wire `tt` (root command) to: discover via registry → start in-process engine →
   launch TUI. Keys: run-all, rerun-failed, rerun-node, cancel, open-in-editor
   (stub until Phase 7).
7. `teatest` golden snapshots of rendered views for representative tree states.

**Acceptance:** `tt` in a Go project shows the discovered tree, runs tests, and
updates live; snapshot tests green.

---

## Phase 4 — Server + client protocol (make the TUI a real client)

**Goal:** Extract the engine behind a WebSocket protocol so local == remote.

Tasks:
1. `internal/server`: HTTP server with `/ws` (WebSocket). Bind `127.0.0.1` by
   default; `--addr` and `--token` (header auth) for exposure with a warning.
2. Protocol messages: client→server `subscribe`, `run`, `rerun_failed`, `cancel`;
   server→client = normalized events + initial snapshot. JSON, reusing `internal/event`.
3. `internal/client`: WS client exposing the same interface the TUI used in-process
   (subscribe channel + command methods).
4. Refactor `tt` (root) to start an in-process engine + server on an ephemeral
   loopback port and connect the TUI via `internal/client` — single code path.
5. `tt serve [--addr] [--token]` (headless) and `tt connect <addr> [--token]`
   (TUI against remote).
6. Loopback integration tests: engine+server on `127.0.0.1`, test client asserts
   subscribe snapshot, live events, command round-trips, auth rejection.

**Acceptance:** `tt`, `tt serve`, `tt connect` all work; remote = same code as local
modulo address; integration tests green.

---

## Phase 5 — MCP server

**Goal:** Agents can drive and observe the same session over MCP.

Tasks:
1. `internal/mcp`: register tools on the shared HTTP server (`/mcp`, Streamable
   HTTP) using the Go MCP SDK; handlers are thin wrappers over the engine.
2. Tools: `list_tests`, `run_tests`, `get_results`, `get_failure`, `rerun_failed`,
   `cancel_run` — schemas + handlers.
3. Ensure agent-triggered runs emit events seen by connected TUIs (shared session).
4. Tests: in-memory engine + MCP handler tests; an end-to-end test where an MCP
   `run_tests` call produces events observed on a `/ws` subscriber.

**Acceptance:** MCP tools function against a live engine; agent and TUI observe the
same run; tests green.

---

## Phase 6 — Vitest + pytest adapters

**Goal:** Prove the adapter contract across hierarchical styles.

Tasks (per adapter, golden-file driven):
1. **Vitest** (`internal/adapter/vitest`): `Detect` (vitest in package.json/config);
   `Discover` via `vitest list --json`; `Run` via JSON reporter, mapping nested
   describe/it → suite/test nodes; `Locate` from reporter file/line. Goldens for
   pass/fail/nested/skipped.
2. **pytest** (`internal/adapter/pytest`): `Detect` (pytest config / `tests/`);
   `Discover` via `pytest --collect-only -q` (nodeids → module/class/test);
   `Run` via a JSON report plugin, mapping nodeids → nested nodes incl. parametrized
   cases (mid-run discovery); `Locate` from nodeid file:line. Goldens accordingly.
3. Example projects under `testdata/projects/{vitest,pytest}/`; e2e tree assertions.
4. Verify multi-framework discovery: a monorepo fixture with Go + Vitest + pytest
   produces a correctly grouped top-level tree.

**Acceptance:** all three adapters green from goldens; multi-framework discovery
test green.

---

## Phase 7 — Jump-to-editor

**Goal:** Open a failing test at its source location.

Tasks:
1. `internal/config`: load `~/.config/tt/config.toml` (and project-local override);
   field for editor command template; presets for VS Code/Cursor, Neovim/Vim,
   JetBrains, `$EDITOR`.
2. `internal/locate`: expand `{file}`/`{line}`/`{col}` and exec; tests for template
   expansion and arg splitting (no shell injection).
3. Wire the TUI `o` key to resolve the selected node's location and invoke the
   template. Always render locations as `file:line:col` for terminal link detection
   (Warp et al.).

**Acceptance:** `o` opens the configured editor at the failing line; template
expansion unit-tested; documented Warp behavior holds (clickable locations).

---

## Phase 8 — Polish & release

**Goal:** Ship-ready v1.

Tasks:
1. Filter/search refinements; summary header polish; full keybinding set + help
   overlay; graceful narrow-terminal degradation.
2. Config file documentation; `--help` for all commands.
3. README with install (`go install`, Homebrew tap), usage, screenshots/gif,
   adapter-contributing guide.
4. GoReleaser config for cross-compiled binaries + checksums; tag `v0.1.0`.

**Acceptance:** clean install on a fresh machine; docs complete; release artifacts
build.

---

## Risks & mitigations

- **Framework JSON drift** (vitest/pytest change output between versions): golden
  fixtures pin known-good output and make breakage loud; document supported
  version ranges per adapter.
- **Runtime-discovered nodes** (Go subtests, pytest params): handled by mid-run
  `NodeDiscovered` in the engine (Phase 1) — verify with targeted fixtures.
- **pytest JSON reporting** depends on a plugin (e.g. `pytest-report`/JSON): decide
  whether to require a plugin or parse `--report-log` (JSONL, built into pytest).
  Prefer the built-in `--report-log` to avoid a user-installed dependency; confirm
  during Phase 6.
- **MCP code execution exposure:** localhost-default + token-gated `--addr`
  enforced in Phase 4/5; covered by auth tests.

## Open items to confirm before/while coding

- Module path / repo location and license choice (Phase 0).
- Binary name `tt` (collision check on PATH; alternatives if undesired).
- pytest JSON source: built-in `--report-log` vs. a plugin (Phase 6).
- WebSocket library choice (`coder/websocket` vs `gorilla/websocket`).
