# Developer Notes

- Don't support "legacy" code. Remove code. We don't yet care about breaking changes.
- Aviary is an MCP, so you can connect to it via `go run ./cmd/aviary start` and then connect on https://localhost:16677/mcp (read ~/.config/aviary/aviary.yaml for port, ~/.config/aviary/token has the bearer token).
  - The server is usually already running so you should assume that it is, and just connect to it. If you need to restart it, you can do so with the above command.
  - Always use the MCP to test features when developing
- Run `pnpm test:go` after any Go changes; `pnpm test:web` for web, run `pnpm lint` after any changes; run `pnpm test` to run everything
