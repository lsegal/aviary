# Developer Notes

- Don't support "legacy" code. Remove code. We don't yet care about breaking changes.
- The server is always running, so you can connect to it and test features as you develop. Don't worry about starting/stopping the server. This applies for go server, vite server, and docs vitepress server.
  - Don't rebuild docs, don't rebuild the Go server. Just connect to the running server and test your changes.
    - https://localhost:5173 for web frontend
    - https://localhost:5174 for docs.
- Aviary is an MCP, so you can connect to it via `go run ./cmd/aviary serve` and then connect on https://localhost:16677/mcp (read ~/.config/aviary/aviary.yaml for port, ~/.config/aviary/token has the bearer token).
  - The server is usually already running so you should assume that it is, and just connect to it. If you need to restart it, you can do so with the above command.
  - Always use the MCP to test features when developing
- Run `pnpm test:go` after any Go changes; `pnpm test:web` and `pnpm test:e2e` for web, run `pnpm lint` after any changes; run `pnpm test` to run everything
- Projects (`internal/projects/`) must use core Aviary's implementation of any capability core already has: scheduling, providers/models, agent and CLI running, browser automation, configuration, state storage, auth, logging, process/restart management, MCP, and the rest. Projects' own code is only the Projects-specific delta (watching GitHub issues, ownership/handoff, dispatching fix jobs, presenting them). Never add a Projects-side reimplementation of a core capability; extend core instead. See #424 for the audit of existing duplication.
