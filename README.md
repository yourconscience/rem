# rem

Review-first memory for coding agents. rem gives each new agent session a short memory context, lets you capture notes, and turns past sessions into memory pages that you review before any session loads them.

It works beside any config manager and has three parts:

- **A session-start hook.** It injects the head of `MEMORY.md` (at most 4 KB, the current project's group first) and a list of recent sessions from [AgentsView](https://github.com/kenn-io/agentsview): the last 7 days, at most 12, with each session's first request. Anything missing degrades to empty context.
- **`rem`.** Capture a note that is searchable at once, search the vault, dream (read past sessions with a model and write memory pages onto a vault branch for you to review), and migrate an older vault to the current format.
- **`knowledge-sync`.** Guarded commit, merge and push for a git-backed vault, with a best-effort search reindex.

## What it does not do

- It does not record sessions. AgentsView indexes each harness's own transcripts, and rem reads them from there.
- It runs no daemon and installs no schedule. Installing copies files. Nothing runs until you register the hook.
- It never promotes a fact on its own. Notes from `rem add` are searchable but never injected. A dream lands on a branch, and only what you merge reaches `MEMORY.md`, the file that sessions load.
- The hook never fails a session over a missing piece. Without AgentsView, memsearch or a vault it returns empty context and exits 0.

## What it writes

| Command | Writes |
|---|---|
| `rem install`, `make install` | `hooks/` and `lib/` in `$REM_HOME` (`~/.local/share/rem`) |
| session-start hook | Nothing in the vault. If AgentsView has remote hosts, it may start one detached `agentsview sync --host <host>` for a host not synced in 12 hours, and keeps lock, stamp and log files under `${XDG_STATE_HOME:-~/.local/state}/rem/` |
| `rem add` | One note in `$KNOWLEDGE_DIR/memory/_inbox/`. If the vault is missing or empty, it first creates it |
| `rem dream --v2` | A `rem-dream/<date>` branch in a worktree under `$KNOWLEDGE_DIR/.worktrees/`, with pages in `memory/` and the index `MEMORY.md`. On an older vault the branch starts with a migration commit. It never writes to `main` |
| `rem migrate` | A `rem-migrate/<date>` branch with the index moved to `MEMORY.md`. It makes no model call, never writes to `main` and never pushes |
| `rem sync`, `knowledge-sync` | A commit, merge and push of the vault, then a memsearch reindex if memsearch is installed |

The vault is `$KNOWLEDGE_DIR`, by default `~/agent-memory`, the path the Agent Memory Repo skill uses. Neither install nor the hook creates it. The first `rem` command that writes to it creates it as a valid Agent Memory Repo and prints the full path: `git init -b main`, a `MEMORY.md` skeleton, and a commit of that file if git has `user.name` and `user.email` set. rem never changes git config. A path that already has files is used as it is.

`rem dream --v2` reads more than the vault. It renders recent AgentsView transcripts, redacts common token formats, and sends them to a model through the `claude` CLI. It also reads Claude Code and Codex memory files changed since the last dream.

## Format

The vault follows the [Agent Memory Repo](https://github.com/AgentMemoryRepo/agentmemoryrepo) format ([spec](https://github.com/AgentMemoryRepo/agentmemoryrepo/blob/main/SPEC.md)) so other tools that read it can read the vault, and nothing reaches `MEMORY.md` on `main` without a merge you make.

```text
agent-memory/
  MEMORY.md         entry point; the hook injects its first 4 KB
  memory/
    <slug>.md       one page per topic
    _inbox/         unreviewed notes from `rem add`, never injected
```

`MEMORY.md` has a `# Memory` title, the few entries every session needs, then `## Index`: one line per page, `- [[memory/slug]]: one sentence`, under `### <domain>` and `### project: <name>` headings. An entry is one line with metadata at the end:

```markdown
- Deploys run with `make ship` [source: <session id>; added: 2026-11-16]
```

Links start at the vault root and omit `.md`. Keys other than `source` and `added`, such as `verified`, are allowed.

Where rem differs from the spec:

- The spec lets an agent write to the repo with no human in the loop. In rem, `rem add` writes only to `memory/_inbox/`, and a dream writes to a branch.
- New pages have no frontmatter; pages from an older rem keep theirs. An inbox note keeps a small frontmatter (`harness`, `cwd`, `created`) that a dream uses to match it to a session. Its `source` is the harness name, because `rem add` does not know the session.
- `rem add` leaves an uncommitted note, and the Agent Memory Repo skill refuses to write to a repo with uncommitted changes. Commit the inbox note or run `rem sync` first.

A vault from before this format has `memory/_index.md` and `[[slug]]` links. The hook reads that index until you migrate. `rem migrate` commits the move to a `rem-migrate/<date>` branch. It refuses on a vault that has uncommitted changes, and running it on a migrated vault changes nothing.

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

Capture a note. The first write creates the vault at `~/agent-memory` and prints where it is:

```bash
rem add -src manual "prefers pnpm for Node work"
rem search "pnpm"
```

The note is one entry line in `memory/_inbox/`. It is labeled unreviewed in search and is never injected at session start.

A `KNOWLEDGE_DIR` that already has files is used as it is, so make it a git repository with a `main` branch and a `MEMORY.md` yourself:

```bash
cd "$KNOWLEDGE_DIR"
git init -b main
printf '# Memory\n\n## Index\n' > MEMORY.md
git add -A && git commit -m "Start the vault"
```

An older vault with `memory/_index.md` keeps working. To move it to the current format, run `rem migrate`, review the `rem-migrate/<date>` branch it prints and merge it.

Then dream over recent sessions:

```bash
rem dream --v2 --limit 10 --dry-run   # list what a dream would read, run no model
rem dream --v2 --limit 10             # write memory pages onto a rem-dream/<date> branch
```

Read the report the dream prints, review the branch and merge it into `main`. The merged `MEMORY.md` is what the hook injects from then on.

Add a remote to sync the vault between machines. `rem sync` runs `knowledge-sync` once. Schedule `knowledge-sync` yourself if you want it periodic.

More detail: [cmd/rem](cmd/rem/README.md), [cmd/knowledge-sync](cmd/knowledge-sync/README.md) and [memsearch](memsearch/README.md).

## Configuration

| Variable | Default | Purpose |
|---|---|---|
| `KNOWLEDGE_DIR` | `~/agent-memory` | The vault, created by the first `rem` command that writes to it |
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

The vault stays, at `~/agent-memory` by default. Delete it only if you mean to delete your memory.

## Development

```bash
make test
```

## License

[MIT](LICENSE)
