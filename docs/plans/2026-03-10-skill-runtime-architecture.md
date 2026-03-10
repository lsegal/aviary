# Skill Runtime Architecture

## Decision

Move Aviary from implicit `SKILL.md` prompt injection to explicit skill runtime registration.

Skills remain markdown-defined in `skills/<name>/SKILL.md`, but they are no longer automatically appended to every agent prompt. Instead, each enabled skill becomes an executable runtime capability that registers an MCP tool named `skill_<name>`.

This keeps the user-facing model simple:

- A skill is a capability package.
- Enabled skills show up as tools.
- Permissions continue to use the existing tool allowlist model.

## User-Facing Config Shape

```yaml
skills:
  gogcli:
    enabled: true
    binary: gog
    allowed_commands:
      - gmail
      - calendar
      - drive
```

Semantics:

- `enabled: true` registers `skill_gogcli`
- no separate `permissions.skills`
- no separate `restrictSkills`
- agent/channel access is controlled through existing tool permissions:
  - `agents[].permissions.tools`
  - `agents[].channels[].allowFrom[].restrictTools`

## Runtime Model

Each skill needs:

1. A markdown definition:
   - `skills/<name>/SKILL.md`
   - frontmatter metadata (`name`, `description`)
   - implementation guidance/reference content

2. A runtime executor:
   - built-in executor in Go for first-party skills like `gogcli`
   - later: external executors or packaged scripts

3. An MCP registration:
   - tool name: `skill_<name>`
   - description sourced from skill metadata/runtime

## Migration Steps

### Phase 1

- Remove automatic `SKILL.md` prompt injection from agent runs
- Keep markdown skill parsing utilities for metadata/content loading
- Document the new runtime direction

### Phase 2

- Add `skills` to `internal/config.Config`
- Add validation for enabled skills and skill-specific config
- Load configured skills on startup and config reload

### Phase 3

- Add `internal/skills` registry/runtime package
- Define runtime interface:
  - `Name()`
  - `ToolName()`
  - `Description()`
  - `Execute(...)`
- Register `skill_<name>` tools dynamically in MCP

### Phase 4

- Feed dynamic skill tools into existing tool listing
- Ensure web permission UI shows `skill_<name>` alongside built-in tools
- Reuse `permissions.tools` and `restrictTools` unchanged

### Phase 5

- Implement first built-in runtime: `skill_gogcli`
- Source docs/metadata from `skills/gogcli/SKILL.md`
- Back execution with `gog` binary and skill config

## Notes

- `SKILL.md` remains the source-of-truth format for skill definition and metadata.
- Skills are no longer a hidden prompt-side effect.
- MCP becomes the public interface for skill invocation.
- This avoids introducing a second permission bag just for skills.
