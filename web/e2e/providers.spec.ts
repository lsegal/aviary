import { expect, test } from "@playwright/test";
import { mockMCP, setAuthToken } from "./helpers/mockMCP";

const CLI_DEFINITIONS = { acme: { binary: "acme-cli" } };

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

async function setup(
	page: import("@playwright/test").Page,
	onSave?: (config: Record<string, unknown>) => void,
) {
	const calls: string[] = [];
	await setAuthToken(page);
	await mockMCP(page, {
		config_get: {
			server: { tls: { cert: "", key: "" } },
			agents: [],
			models: {
				providers: { anthropic: { auth: "auth:anthropic:default" } },
				defaults: { model: "", fallbacks: [] },
				cli_definitions: CLI_DEFINITIONS,
			},
			browser: { binary: "", reuse_tabs: true },
			scheduler: { concurrency: "" },
			skills: {},
		},
		config_save: (args) => {
			onSave?.(JSON.parse(String(args?.config ?? "{}")));
			return {};
		},
		auth_list: ["anthropic:default"],
		skills_list: [],
		tool_list: [],
		provider_cli_list: () => {
			calls.push("provider_cli_list");
			return CLIS;
		},
	});
	return calls;
}

test("one providers list holds the API providers and every coding CLI with its doctor status", async ({
	page,
}) => {
	const calls = await setup(page);
	// With a cached CLI list, the General tab reuses it instead of probing.
	await page.addInitScript((clis) => {
		localStorage.setItem("aviary.providers.clis", JSON.stringify(clis));
	}, CLIS.clis);
	await page.goto("/settings/general");
	await expect(page.getByTestId("default-model-selector")).toBeVisible();
	expect(calls).toEqual([]);

	await page.goto("/settings/providers");
	const card = page.getByTestId("providers-list");
	await expect(card).toBeVisible();
	await expect(page.getByText("Coding CLIs", { exact: true })).toHaveCount(0);

	const codex = card.locator('[data-cli="codex"]');
	await expect(codex).toHaveAttribute("data-status", "ok");
	await expect(codex.locator('[data-field="install"]')).toContainText(
		"codex-cli 0.130.0",
	);
	await expect(codex.locator('[data-field="install"]')).toContainText(
		"/usr/local/bin/codex",
	);
	await expect(codex.locator('[data-field="auth"]')).toHaveText("signed in");
	await expect(codex.locator('[data-field="quota"]')).toContainText(
		"72% of weekly quota left",
	);
	await expect(codex.locator('[data-field="models"]')).toContainText(
		"2 models",
	);
	await codex.locator("summary").click();
	await expect(codex.getByText("codex/gpt-5.5-mini")).toBeVisible();

	const gemini = card.locator('[data-cli="gemini"]');
	await expect(gemini).toHaveAttribute("data-status", "warn");
	await expect(gemini.locator('[data-field="auth"]')).toHaveText("signed out");

	// Absent CLIs, built-in or user-defined, are rows marked as not installed.
	for (const name of ["muse", "acme"]) {
		const row = card.locator(`[data-cli="${name}"]`);
		await expect(row).toHaveAttribute("data-status", "missing");
		await expect(row.locator('[data-field="install"]')).toContainText(
			"Not installed",
		);
		await expect(row.locator('[data-field="auth"]')).toHaveText(
			"not installed",
		);
	}

	// The API providers sit in the same list, ahead of the CLI providers.
	await expect(card.locator("table")).toHaveCount(1);
	await expect(card.getByText("Anthropic")).toBeVisible();
	await expect(card.getByText("API Key")).toBeVisible();
	await expect(codex.getByText("CLI", { exact: true })).toBeVisible();
	await expect(card.locator("tbody tr").first()).toContainText("Anthropic");
});

test("saving settings keeps the coding-CLI definitions", async ({ page }) => {
	let saved: Record<string, unknown> | null = null;
	await setup(page, (config) => {
		saved = config;
	});
	await page.goto("/settings/general");
	await page
		.getByRole("checkbox", { name: /Expose service externally/i })
		.check();
	await page.getByRole("button", { name: "Save Changes" }).click();
	await expect.poll(() => saved).not.toBeNull();
	expect(saved).toMatchObject({
		models: { cli_definitions: CLI_DEFINITIONS },
	});
});

test("the default model pickers offer the models of installed CLI providers", async ({
	page,
}) => {
	let saved: Record<string, unknown> | null = null;
	const calls: string[] = [];
	await setAuthToken(page);
	await mockMCP(page, {
		config_get: {
			server: { tls: { cert: "", key: "" } },
			agents: [],
			models: { providers: {}, defaults: { model: "", fallbacks: [] } },
			browser: { binary: "", reuse_tabs: true },
			scheduler: { concurrency: "" },
			skills: {},
		},
		config_save: (args) => {
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
	await page.goto("/settings/general");
	await expect.poll(() => calls).toEqual(["provider_cli_list"]);

	const offered = ["codex/gpt-5.5", "codex/gpt-5.5-mini"];
	for (const testId of [
		"default-model-selector",
		"default-fallbacks-selector",
	]) {
		await page.getByTestId(testId).locator("input").click();
		const options = page.locator(".absolute.z-50 .truncate.font-medium");
		// Only installed CLIs with models contribute: codex, not the missing
		// claude, acme and muse, nor gemini, which reports no models.
		await expect(options).toHaveText(offered);
		await page.keyboard.press("Escape");
	}

	await page.getByTestId("default-model-selector").locator("input").click();
	await page.getByText("codex/gpt-5.5-mini", { exact: true }).click();
	await page.getByRole("button", { name: "Save Changes" }).click();
	await expect.poll(() => saved).not.toBeNull();
	expect(saved).toMatchObject({
		models: { defaults: { model: "codex/gpt-5.5-mini" } },
	});
});
