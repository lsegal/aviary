# Memory

Memory in Aviary is managed through the agent's workspace files rather than dedicated MCP tools. Agents read and write these files directly using the [filesystem and note tools](./files-and-notes).

## Memory Files

Each agent has a workspace directory (`~/.config/aviary/agents/<name>/`) that persists across sessions:

- **`MEMORY.md`** — long-term curated notes; agents update this file to persist facts across sessions.
- **`memory/YYYY-MM-DD.md`** — daily session logs written by the agent as work progresses.

Agents read and write these files with `file_read`, `file_write`, and `note_write`.

## Memory Pooling

Agents can share a memory pool via the `memory` config field. See the [Configuration Reference](/reference/config#agents) for full field documentation.

```yaml
agents:
  - name: my-agent
    memory: shared        # "shared", "private", or a named pool (e.g. "team-memory")
    memory_tokens: 4096   # max tokens injected into each prompt
```

At the start of each session Aviary injects relevant memory content into the system prompt automatically, up to `memory_tokens`.
