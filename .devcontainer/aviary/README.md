# Devcontainer Aviary Config

This directory is bind-mounted into the dev container as `~/.config/aviary`.

Typical first-run flow:

```sh
cat > .devcontainer/aviary/aviary.yaml <<'EOF'
models:
  defaults:
    model: openai-codex/gpt-5.2
EOF
```

After the container starts, Aviary will generate local runtime files here, including:

- `token`
- `certs/`
- `logs/`
- `browser/`

Useful local endpoints:

- UI: `https://localhost:16677`
- MCP: `https://localhost:16677/mcp`
- Token: `.devcontainer/aviary/token`
