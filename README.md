# rem

Review-first memory for coding agents, as a standalone package that works beside any config manager.

- **Session start context:** the head of the memory index (at most 4 KB), a compact list of recent sessions from [AgentsView](https://github.com/kenn-io/agentsview), and pinned Claude Desktop sessions when a sessions tool is installed. Anything unavailable degrades to empty context.
- **`rem`:** capture notes that are searchable at once, dream over past sessions onto a reviewable branch, and search the vault. Only a branch you merge reaches the memory sessions load.
- **`knowledge-sync`:** guarded commit, merge and push for a git-backed vault, with a best-effort search reindex.

It does not capture sessions itself. AgentsView indexes each harness's own transcripts.

## Requirements

Go 1.26+ and Python 3 to build and run. AgentsView and [memsearch](https://github.com/zilliztech/memsearch) are optional.

## Install

```bash
make install   # BINDIR=~/.local/bin, REM_HOME=~/.local/share/rem
```

Release binaries ship the hooks embedded, so `go install` works too:

```bash
go install github.com/yourconscience/rem/cmd/rem@latest
go install github.com/yourconscience/rem/cmd/knowledge-sync@latest
rem install              # writes hooks/ and lib/ into $REM_HOME
rem hook session-start   # prints the session-start hook command
```

Installing only copies files. No hook runs until you register it.

## Register the hook

### With tackroom

Add the hook to `hooks:` in your `tackroom.yaml` and run `tackroom sync`:

```yaml
hooks:
  - name: rem-session-start
    enabled: true
    event: SessionStart
    command: ~/.local/share/rem/hooks/session-start.sh
    timeout: 15
    agents: [claude, codex, pi, omp]
```

tackroom writes Pi and OMP a generated module, so the files in `integrations/` are not needed.

### By hand

Any harness that accepts a command hook can run `~/.local/share/rem/hooks/session-start.sh` on session start. It reads the hook's JSON payload on stdin and prints a `hookSpecificOutput.additionalContext` reply. Pi and OMP run hooks as TypeScript modules: copy `integrations/session-start.pi.ts` to `~/.pi/agent/extensions/rem-session-start.ts`, or `integrations/session-start.omp.ts` to `~/.omp/agent/hooks/post/rem-session-start.ts`.

## Use

```bash
rem add -src claude "prefers pnpm for Node work"
rem search "preferences"
rem dream --v2 --limit 10 --dry-run   # show what a dream would read
rem dream --v2 --limit 10             # write a dream onto a vault branch
rem sync
```

Run `knowledge-sync` on a schedule to keep the vault synced. See [cmd/rem](cmd/rem/README.md), [cmd/knowledge-sync](cmd/knowledge-sync/README.md) and [memsearch](memsearch/README.md).

## Configuration

| Variable | Default | Purpose |
|---|---|---|
| `KNOWLEDGE_DIR` | `${XDG_DATA_HOME:-~/.local/share}/rem/vault` | The vault, created by the first `rem` command that writes to it |
| `KNOWLEDGE_REMOTE` | `origin` | Remote for `knowledge-sync` and `rem sync` |
| `KNOWLEDGE_BRANCH` | `main` | Branch for `knowledge-sync` |
| `KNOWLEDGE_CONF` | `~/.config/rem/memsearch.conf` | Optional shell file that sets the vault and memsearch paths |
| `REM_PRIVATE_PROJECTS` | `personal` | Comma-separated markers; a session whose project name or folder contains one keeps its metadata but loses its text in other sessions' context |
| `REM_DREAM_RUNNER` | `claude` | Headless runner for `rem dream --v2`; only `claude` is supported so far |
| `REM_DREAM_MODEL` | `haiku` | Model for the dream |
| `REM_AGENTS_MD` | `~/.agents/AGENTS.md` | The file a dream reads and writes patches for |
| `REM_HOME` | `~/.local/share/rem` | Where `rem install` writes the hook and its library; the Pi and OMP modules in `integrations/` look there |
| `AGENTSVIEW_BIN` | found on `PATH` | Tool override |

## Development

```bash
make test
```

## License

[MIT](LICENSE)
