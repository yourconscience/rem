# rem

Review-first memory for coding agents. rem gives each new agent session a short memory context, lets you capture notes, and turns past sessions into memory pages that you review before any session loads them.

It works beside any config manager and has three parts:

- **A session-start hook.** It injects the head of your memory index (at most 4 KB) and a list of recent sessions from [AgentsView](https://github.com/kenn-io/agentsview): the last 7 days, at most 12, with each session's first request. Anything missing degrades to empty context.
- **`rem`.** Capture a note that is searchable at once, search the vault, and dream: read past sessions with a model and write memory pages onto a vault branch for you to review.
- **`knowledge-sync`.** Guarded commit, merge and push for a git-backed vault, with a best-effort search reindex.

## What it does not do

- It does not record sessions. AgentsView indexes each harness's own transcripts, and rem reads them from there.
- It runs no daemon and installs no schedule. Installing copies files. Nothing runs until you register the hook.
- It never promotes a fact on its own. Notes from `rem add` are searchable but never injected. A dream lands on a branch, and only what you merge reaches the index that sessions load.
- The hook never fails a session over a missing piece. Without AgentsView, memsearch or a vault it returns empty context and exits 0.

## What it writes

| Command | Writes |
|---|---|
| `rem install`, `make install` | `hooks/` and `lib/` in `$REM_HOME` (`~/.local/share/rem`) |
| session-start hook | Nothing in the vault. If AgentsView has remote hosts, it may start one detached `agentsview sync --host <host>` for a host not synced in 12 hours, and keeps lock, stamp and log files under `${XDG_STATE_HOME:-~/.local/state}/rem/` |
| `rem add` | One note in `$KNOWLEDGE_DIR/memory/_inbox/` |
| `rem dream --v2` | A `rem-dream/<date>` branch in a worktree under `$KNOWLEDGE_DIR/.worktrees/`. It never writes to `main` |
| `rem sync`, `knowledge-sync` | A commit, merge and push of the vault, then a memsearch reindex if memsearch is installed |

The vault is `$KNOWLEDGE_DIR`, by default `${XDG_DATA_HOME:-~/.local/share}/rem/vault`. Neither install nor the hook creates it. The first `rem` command that writes to it creates it and prints the full path.

`rem dream --v2` reads more than the vault. It renders recent AgentsView transcripts, redacts common token formats, and sends them to a model through the `claude` CLI. It also reads Claude Code and Codex memory files changed since the last dream.

## Requirements

- macOS or Linux.
- Python 3 for the hook. It uses only the standard library.
- git, if you dream or sync. The vault must be a git repository with a `main` branch for both.
- Go 1.26 or newer, only to build from source.

Optional: AgentsView, [memsearch](https://github.com/zilliztech/memsearch) and the Claude Code CLI (`claude`). See [Optional pieces](#optional-pieces).

## Install

Pick one.

**Release binary.** Download `rem_<version>_<os>_<arch>.tar.gz` from [Releases](https://github.com/yourconscience/rem/releases), unpack it, and put `rem` and `knowledge-sync` on your `PATH`. Then write the hook files:

```bash
rem install
```

**go install.** The binaries carry the hook files, so this needs no checkout:

```bash
go install github.com/yourconscience/rem/cmd/rem@latest
go install github.com/yourconscience/rem/cmd/knowledge-sync@latest
rem install
```

**From a checkout.** This builds both binaries and copies the hook files itself:

```bash
make install   # BINDIR=~/.local/bin, REM_HOME=~/.local/share/rem
```

Installing only copies files. `rem hook session-start` prints the command to register next.

## Register the hook

### With tackroom

[tackroom](https://github.com/yourconscience/tackroom) keeps one config for many harnesses. Add the hook to `hooks:` in your `tackroom.yaml` and run `tackroom sync`:

```yaml
hooks:
  - name: rem-session-start
    enabled: true
    event: SessionStart
    command: ~/.local/share/rem/hooks/session-start.sh
    timeout: 15
    agents: [claude, codex, pi, omp]
```

List only the harnesses you use. tackroom registers a command hook where the harness takes one and generates a TypeScript module for Pi and OMP, so you need nothing from `integrations/`. Its [hook docs](https://github.com/yourconscience/tackroom/blob/main/docs/hooks.md) list every harness it can reach.

The hook replies in Claude Code's format, `hookSpecificOutput.additionalContext`. A harness that reads another field gets no context. tackroom names Antigravity CLI as one.

### By hand

Register `~/.local/share/rem/hooks/session-start.sh` as a command hook on session start in each harness. The command reads the harness's JSON payload on stdin and prints a `hookSpecificOutput.additionalContext` reply. Check it from a shell:

```bash
echo "{\"cwd\":\"$PWD\"}" | "$(rem hook session-start)"
```

Pi and OMP run hooks as TypeScript modules. Copy `integrations/session-start.pi.ts` to `~/.pi/agent/extensions/rem-session-start.ts`, or `integrations/session-start.omp.ts` to `~/.omp/agent/hooks/post/rem-session-start.ts`.

## First use

Capture a note. The first write creates the vault and prints where it is:

```bash
rem add -src manual "prefers pnpm for Node work"
rem search "pnpm"
```

Inbox notes are labeled unreviewed in search and are never injected at session start.

To dream and sync, make the vault a git repository:

```bash
cd "${KNOWLEDGE_DIR:-${XDG_DATA_HOME:-$HOME/.local/share}/rem/vault}"
git init -b main
git add -A && git commit -m "Start the vault"
```

Then dream over recent sessions:

```bash
rem dream --v2 --limit 10 --dry-run   # list what a dream would read, run no model
rem dream --v2 --limit 10             # write memory pages onto a rem-dream/<date> branch
```

Read the report the dream prints, review the branch and merge it into `main`. The merged `memory/_index.md` is what the hook injects from then on.

Add a remote to sync the vault between machines. `rem sync` runs `knowledge-sync` once. Schedule `knowledge-sync` yourself if you want it periodic.

More detail: [cmd/rem](cmd/rem/README.md), [cmd/knowledge-sync](cmd/knowledge-sync/README.md) and [memsearch](memsearch/README.md).

## Configuration

| Variable | Default | Purpose |
|---|---|---|
| `KNOWLEDGE_DIR` | `${XDG_DATA_HOME:-~/.local/share}/rem/vault` | The vault, created by the first `rem` command that writes to it |
| `KNOWLEDGE_REPO` | `$KNOWLEDGE_DIR` | The vault `knowledge-sync` syncs |
| `KNOWLEDGE_REMOTE` | `origin` | Remote for `knowledge-sync` and `rem sync` |
| `KNOWLEDGE_BRANCH` | `main` | Branch for `knowledge-sync` |
| `KNOWLEDGE_CONF` | `${XDG_CONFIG_HOME:-~/.config}/rem/memsearch.conf` | Optional shell file the hook sources; it can set the vault and memsearch paths. `rem` does not read it |
| `REM_HOME` | `~/.local/share/rem` | Where `rem install` writes the hook and its library; the Pi and OMP modules in `integrations/` look there |
| `REM_PRIVATE_PROJECTS` | `personal` | Comma-separated markers; a session whose project name or folder contains one keeps its metadata but loses its text in other sessions' context, and a dream skips it |
| `REM_DREAM_RUNNER` | `claude` | Headless runner for `rem dream --v2`; only `claude` is supported so far |
| `REM_DREAM_MODEL` | `haiku` | Model for the dream |
| `REM_AGENTS_MD` | `~/.agents/AGENTS.md` | The instructions file a dream reads and writes patches for |
| `REM_SYNC_BIN` | `~/.local/bin/knowledge-sync`, else found on `PATH` | The binary `rem sync` runs |
| `REM_COLLECTION` | `ai` | memsearch collection for `rem search` |
| `AGENTSVIEW_BIN` | found on `PATH` | Tool override |

If you move the vault, set `KNOWLEDGE_DIR` in your shell profile so `rem`, the hook and `knowledge-sync` agree.

## Optional pieces

| Piece | Used for | Without it |
|---|---|---|
| AgentsView | Recent sessions in the hook; the transcripts `rem dream --v2` reads | The hook injects only the memory index. `rem dream --v2` stops with an error |
| memsearch | Semantic search over the vault | `rem search` shows inbox matches only, and fails when there are none. `knowledge-sync` skips the reindex |
| Claude Code CLI | Runs the dream (`claude -p`) | `rem dream --v2` cannot run |

The dream uses your Claude subscription login: rem removes `ANTHROPIC_API_KEY` from the runner's environment so the run is not billed to the API.

## Uninstall

1. Unregister the hook. With tackroom, set `enabled: false`, run `tackroom sync`, then delete the entry. By hand, remove it from each harness, and delete any Pi or OMP module you copied.
2. Delete the `rem` and `knowledge-sync` binaries from where you installed them.
3. Remove the hook files and state:

   ```bash
   rm -rf "${REM_HOME:-$HOME/.local/share/rem}/hooks" "${REM_HOME:-$HOME/.local/share/rem}/lib"
   rm -rf "${XDG_STATE_HOME:-$HOME/.local/state}/rem"
   ```

The vault stays. With the default location it sits inside `~/.local/share/rem`, so delete that folder only if you mean to delete your memory.

## Development

```bash
make test
```

## License

[MIT](LICENSE)
