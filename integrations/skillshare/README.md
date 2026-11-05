# skillshare companion setup

rem only covers memory. These notes cover other setup that a rem and skillshare stack needs. Tested with skillshare v0.28.2.

## Codex roles

skillshare converts Markdown roles to Codex TOML with its bundled `codex-agents` extension, which needs Node. `skillshare init` doesn't set this up.

1. Copy [`extensions/codex-agents`](https://github.com/runkids/skillshare/tree/main/extensions/codex-agents) from the skillshare repo to `~/.config/skillshare/extensions/codex-agents`. The dashboard also offers this from a target's Agents tab.
2. Add the target to `~/.config/skillshare/config.yaml`:

   ```yaml
   targets:
     codex:
       skills:
         path: ~/.codex/skills
       agents:
         path: ~/.codex/agents
         extension: codex-agents
   ```

3. Run `skillshare sync agents`.

The extension's limits:

- `model` is copied as written, so a tier name such as `model: opus` reaches Codex as a model name.
- `effort` and `model_reasoning_effort` are dropped.
- Its frontmatter parser is flat, so a nested `codex: { model: ... }` block replaces the top-level `model`.

Roles that set only `name`, `description` and a body convert cleanly.

## The `universal` target

`skillshare init --all-targets` can add a `universal` target at `~/.agents/skills`, and sync links skills there. If another tool owns that folder, remove the target before the first sync.
