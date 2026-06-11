# tt

A terminal-UI test runner for polyglot projects.

`tt` auto-discovers tests across multiple language ecosystems, shows them in a
hierarchical tree view with live status, and runs them locally or on a remote
machine. A single engine serves multiple observers — a local TUI, a remote TUI,
and AI agents over MCP all watch and drive the *same* test session.

> **Status:** early development. See the
> [design spec](docs/superpowers/specs/2026-06-10-tt-test-runner-design.md) and
> [implementation plan](docs/superpowers/plans/2026-06-10-tt-implementation-plan.md).

## Planned features

- **Adapters** for Go (`go test`), Vitest, and pytest in v1; Bun test and cargo
  test as fast-follows.
- **Auto-discovery** of all tests in the working tree on startup.
- **Tree view** suited to hierarchical BDD/spec-style suites.
- **Headless engine** so tests can run on a remote/sandboxed devbox while you
  observe locally.
- **MCP server** so an AI agent can trigger and read test runs against the same
  live session you're watching.
- **Jump to failure** in a configurable editor.

## Development

```sh
make build   # build the tt binary into ./bin
make test    # go test ./...
make lint    # go vet + golangci-lint (if installed)
make run     # go run ./cmd/tt
```

## License

[MIT](LICENSE) © 2026 Nick Fisher
