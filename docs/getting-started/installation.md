# Installation

Cairo needs three things:

1. **Go 1.25 or later** — the Go toolchain to build the binary
2. **Ollama** — the local LLM host
3. **One or more models pulled into Ollama**

Everything else (SQLite, the embedding provider) is bundled.

---

## 1. Install Go

macOS (Homebrew):

```bash
brew install go
```

Linux (most distros ship an older Go — use the upstream archive):

```bash
# see https://go.dev/dl/ for the latest; example:
curl -LO https://go.dev/dl/go1.25.0.linux-amd64.tar.gz
sudo rm -rf /usr/local/go && sudo tar -C /usr/local -xzf go1.25.0.linux-amd64.tar.gz
export PATH=$PATH:/usr/local/go/bin
```

Verify:

```bash
go version
# expect: go1.25.x or later
```

---

## 2. Install Ollama

macOS (Homebrew or download):

```bash
brew install ollama
# or: download the app from https://ollama.com
```

Linux:

```bash
curl -fsSL https://ollama.com/install.sh | sh
```

Verify Ollama is running:

```bash
curl http://localhost:11434/api/version
# expect: {"version":"0.x.x"}
```

If Ollama isn't running, start it:

```bash
ollama serve
# runs in the foreground; open a new terminal for the rest of the setup
# (on macOS, the .app starts the server automatically)
```

---

## 3. Pull models

You need at least one **chat model** and one **embedding model**. Cairo's defaults (configured via `config` keys — see [Config keys](../reference/config-keys.md)) reference:

- `devstral-24b:latest` — chat model default
- `nomic-embed:latest` — embedding model default
- `ministral-8b:latest` — summarizer model

You don't have to use these specific models. Any Ollama model works; you'll pick yours via the first-run config or `/init`.

Recommended starting set for ~16GB of RAM:

```bash
ollama pull qwen3:30b-a3b-instruct    # or any decent generalist
ollama pull nomic-embed-text          # embeddings
ollama pull ministral:8b              # fast summarizer
```

For ~32GB+ of RAM, upgrade the generalist to something bigger (`qwen3:32b`, `mistral-small:24b`, etc.).

Verify:

```bash
ollama list
# expect: a table showing your pulled models
```

---

## 4. Build and install cairo

Clone the repo and install the binary:

```bash
git clone https://github.com/scotmcc/cairo.git
cd cairo
make install
```

`make install` runs `go install ./cmd/cairo`, which places the binary in `$GOPATH/bin` (typically `~/go/bin`). Make sure that's on your PATH:

```bash
export PATH=$PATH:~/go/bin        # add to your shell rc
cairo -help
# expect: flag help output
```

Alternatively, `go install github.com/scotmcc/cairo/cmd/cairo@latest` works once the repo is public.

---

## 5. First run

Cairo creates `~/.cairo2/` on first run. It's not created by the installer — just start it:

```bash
cairo -new
```

Expected output:

```
cairo · Selene · session 1 · role:thinking_partner
type /help for commands, /exit to quit

(Selene is here but hasn't met you yet — type /init to introduce yourself, or /config for direct setup)

>
```

If you see that, installation worked. The next step is [Quickstart](quickstart.md) or [First run](first-run.md).

---

## Troubleshooting

### `cairo: command not found`
Your `~/go/bin` isn't on PATH. Run `echo $PATH` to confirm, and add `export PATH=$PATH:~/go/bin` to your `.bashrc`, `.zshrc`, or equivalent.

### `ollama: ollama unreachable at http://localhost:11434`
Ollama isn't running. Start it with `ollama serve` (or start the macOS app). Verify with `curl http://localhost:11434/api/version`.

### `ollama 404: model 'devstral-24b:latest' not found`
The model Cairo expects isn't pulled. Either:
- `ollama pull devstral-24b:latest` (or whatever model is referenced)
- Override the default: `cairo "use config to set model to <the model you have>"` in your first session, or edit `internal/db/seed.go` before building if you want a different default baked in.

Look at `ollama list` to see what you have; set `model`, `embed_model`, `summary_model` to names from that list.

### Build errors about `modernc.org/sqlite`
Make sure Go is ≥ 1.25. Older Go versions may not build modernc.org/sqlite cleanly.

### `cairo.db-wal` and `cairo.db-shm` appear next to the database
Expected. Those are SQLite's WAL sidecar files. Don't delete them while cairo is running; they're auto-managed.

---

## Optional: VM / Docker

Cairo doesn't ship a Dockerfile. The binary has no system dependencies beyond Ollama, so:

- Running Cairo in a container and Ollama on the host works if you expose Ollama's port.
- Running Ollama in a container is possible but GPU passthrough is fiddly.

The simplest setup is both on the host, which is what the defaults assume.
