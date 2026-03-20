# Configuration

This page is the placeholder for the full `aviary.yaml` and live settings documentation.

## Major Config Domains

- `server`: port, external access, TLS, and restart-sensitive options.
- `agents`: model selection, fallbacks, working directory, verbosity, permissions, channels, tasks, and agent root files.
- `models`: provider credentials and default model fallback chains.
- `browser`: binary, CDP port, and headless behavior.
- `search`: web-search credential linkage.
- `scheduler`: concurrency and task execution controls.
- `skills`: installed skill activation and per-skill settings.

## Important Behaviors To Document

- The settings UI saves the whole config object.
- Validation rejects error-level configuration issues before save.
- Agent renames and template sync have file-system side effects.
- Provider availability checks are partially asynchronous.

## Placeholder Deliverables

- Field-by-field schema reference.
- Minimal and advanced examples.
- Permission model guide.
- Channel and task examples.
