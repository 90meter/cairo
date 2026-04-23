# Building

Cairo is a pure-Go binary with one cgo-free SQLite driver (`modernc.org/sqlite`). No C toolchain needed, no Python, no Node. `go install` works; `make` is a small convenience.

---

## Requirements

- **Go 1.25 or later.** Earlier versions may work but aren't tested.
- **Ollama running** at the target URL (default `http://localhost:11434`) for runtime, not for build.

Build is self-contained. Nothing in the build step contacts a network beyond `go mod download`.

---

## Targets

```bash
make build         # build to /tmp/cairo
make install       # go install — places cairo in $GOPATH/bin (~/go/bin)
make run           # make build then /tmp/cairo
make clean         # rm /tmp/cairo
```

The Makefile is nine lines. You can read it instead of this page.

---

## Equivalent raw `go` commands

If you don't want to use `make`:

```bash
go build -o /tmp/cairo ./cmd/cairo         # == make build
go install ./cmd/cairo                      # == make install
```

From outside the repo (once public):

```bash
go install github.com/scotmcc/cairo/cmd/cairo@latest
```

---

## Where the binary goes

`make install` places the binary at `$GOPATH/bin/cairo`. If `GOPATH` isn't set, Go defaults to `$HOME/go`, so the typical path is `~/go/bin/cairo`.

Make sure that's on PATH:

```bash
export PATH=$PATH:~/go/bin
```

---

## Build layout

```
cmd/cairo/               entrypoint — main.go + bundle.go (subcommands)
internal/agent/          agent loop, event bus, prompt composition
internal/cli/            line-oriented chat interface + stdout renderer
internal/db/             SQLite schema, migrations, query helpers
internal/llm/            Ollama HTTP client + streaming + embeddings
internal/tools/          every built-in tool
internal/tui/            Bubble Tea TUI
```

Nothing in `internal/` is importable externally — Go's `internal/` package convention enforces that. If you ever need to expose a package for reuse, move it to a top-level directory.

---

## Cross-compilation

Standard Go cross-compilation works because the SQLite driver is pure Go:

```bash
GOOS=linux   GOARCH=amd64 go build -o cairo-linux-amd64   ./cmd/cairo
GOOS=darwin  GOARCH=arm64 go build -o cairo-darwin-arm64  ./cmd/cairo
GOOS=windows GOARCH=amd64 go build -o cairo-windows.exe   ./cmd/cairo
```

Windows is untested — Ollama supports Windows, Cairo *should* build and run there, but no one on the project has tried as of this writing. Reports welcome.

---

## Common build errors

### `go: module lookup disabled by GOPROXY=off`
Your GOPROXY is set restrictively. Unset it (`unset GOPROXY`) or set it to `direct`.

### `undefined: os.<Something>`
Your Go version is too old. `go version` should show 1.25 or later.

### Tests fail with `ollama: unreachable`
Tests shouldn't require Ollama — if a test does hit Ollama it's a bug. File it.

### Linker errors on Linux
The `modernc.org/sqlite` driver should build cleanly on any modern Linux. If you're hitting link errors, report the full error and your distro/arch.

---

## Binary size

The resulting binary is ~25-30MB (Linux amd64, unstripped). Most of the size is `modernc.org/sqlite` (SQLite ported to Go) and the Bubble Tea / lipgloss / termenv stack. Normal for a Go binary with rich TUI + SQLite dependencies.

You can shrink it with `-ldflags="-s -w"` if size matters:

```bash
go build -ldflags="-s -w" -o cairo ./cmd/cairo
```

Strips debug symbols and DWARF info. Not recommended for dev builds; fine for distribution.

---

## Reproducible builds

Cairo doesn't currently pin Go version via `toolchain` directive in `go.mod`, nor does it use `-trimpath` or `GOTOOLCHAIN=auto` in the Makefile. Reproducible builds across machines need:

```bash
go build -trimpath -o cairo ./cmd/cairo
```

Plus the same Go version on both machines. Not a priority for solo dev use, worth doing if we ever publish released artifacts.

---

## See also

- [Testing](testing.md) — the test suite
- [Contributing](contributing.md) — what kinds of changes fit the project
