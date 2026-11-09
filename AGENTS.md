# rem

Review-first memory for coding agents. Keep it independent of any config manager.

- Model capabilities, not vendors: generic lifecycle hooks, with provider names such as `memsearch` as implementation details.
- Hooks must degrade to empty context, never fail a session, and never block on the network.
- Nothing promotes facts into durable instructions automatically. Inbox notes are searchable but never injected, and a dream only reaches the injected layer when a person merges its branch.
- Markdown in the vault is canonical. Search indexes are derived and rebuildable.
- Keep harness-specific registration in `integrations/`, not in hook code.
