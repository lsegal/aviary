import { expect, test } from "vitest";
import { page } from "vitest/browser";
import { renderApp } from "../component-test/app";
import { declarePages } from "../component-test/routes";
import { mockMCP } from "../component-test/server";
import { chooseOption, optionLabels } from "./helpers/select";

// The pages this file renders (see component-test/routes.ts).
declarePages(() => import("../views/SettingsView.vue"));

// From providers.spec.ts. Its own file: the CLI list is module state, so
// the General tab only probes for it on a page that has not loaded it yet.

const CLIS = {
	clis: [
		{
			name: "acme",
			installed: false,
			binary: "acme-cli",
			auth: "unknown",
			status: "missing",
		},
		{
			name: "codex",
			installed: true,
			binary: "codex",
			path: "/usr/local/bin/codex",
			version: "codex-cli 0.130.0",
			auth: "signed in",
			quota: "72% of weekly quota left",
			tracksQuota: true,
			models: ["codex/gpt-5.5", "codex/gpt-5.5-mini"],
			defaultModel: "gpt-5.5",
			skillsNote:
				"gh-fix stale for this aviary dev build; update them under System > Skills",
			status: "ok",
		},
		{
			name: "gemini",
			installed: true,
			binary: "gemini",
			path: "/opt/homebrew/bin/gemini",
			version: "0.40.1",
			auth: "signed out",
			status: "warn",
			statusNote: "signed out",
		},
		{
			name: "agy",
			installed: true,
			binary: "agy",
			path: "/usr/local/bin/agy",
			version: "1.0.0",
			auth: "unknown",
			status: "unknown",
			statusNote: "sign-in state unknown",
		},
		{
			name: "muse",
			installed: false,
			binary: "muse",
			auth: "unknown",
			modelNote: "any model the CLI accepts",
			status: "missing",
		},
	],
};

test("the default model pickers offer the models of installed CLI providers", async () => {
	let saved: Record<string, unknown> | null = null;
	const calls: string[] = [];
	mockMCP({
		config_get: {
			server: { tls: { cert: "", key: "" } },
			agents: [],
			models: { providers: {}, defaults: { model: "", fallbacks: [] } },
			browser: { binary: "", reuse_tabs: true },
			scheduler: { concurrency: "" },
			skills: {},
		},
		config_save: (args?: { config?: unknown }) => {
			saved = JSON.parse(String(args?.config ?? "{}"));
			return {};
		},
		auth_list: [],
		skills_list: [],
		tool_list: [],
		provider_cli_list: () => {
			calls.push("provider_cli_list");
			return {
				clis: [
					...CLIS.clis,
					{
						name: "claude",
						installed: false,
						binary: "claude",
						models: ["claude/opus"],
						status: "missing",
					},
				],
			};
		},
	});

	// No CLI list is cached yet, so the General tab probes for one.
	await renderApp("/system/settings");
	await expect.poll(() => calls).toEqual(["provider_cli_list"]);

	const offered = ["codex/gpt-5.5", "codex/gpt-5.5-mini"];
	for (const testId of [
		"default-model-selector",
		"default-fallbacks-selector",
	]) {
		const selector = page.getByTestId(testId);
		// Only installed CLIs with models contribute: codex, not the missing
		// claude, acme and muse, nor gemini, which reports no models.
		expect(await optionLabels(selector.locator("input"))).toEqual(offered);
	}

	const defaultModel = page.getByTestId("default-model-selector");
	await chooseOption(defaultModel.locator("input"), "codex/gpt-5.5-mini");
	await page.getByRole("button", { name: "Save Changes" }).click();
	await expect.poll(() => saved).not.toBeNull();
	expect(saved).toMatchObject({
		models: { defaults: { model: "codex/gpt-5.5-mini" } },
	});
}, 90_000);
