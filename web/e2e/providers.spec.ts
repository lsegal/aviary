import { expect, test } from "@playwright/test";
import { mockMCP, setAuthToken, type ToolFixtures } from "./helpers/mockMCP";

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
			skillsNote:
				"gh-fix stale for this aviary dev build; update them under Skills > For coding CLIs",
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

async function setup(
	page: import("@playwright/test").Page,
	onSave?: (config: Record<string, unknown>) => void,
	extra: ToolFixtures = {},
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
		...extra,
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

	// The dot follows the state the row shows (#495): a signed-in CLI is green
	// even with stale skills, which the row notes on their own; a warning names
	// its problem on the row and the dot; an unknown sign-in is never green.
	const dot = (row: typeof codex) => row.locator('[data-field="status-dot"]');
	await expect(dot(codex)).toHaveClass(/bg-green-500/);
	await expect(dot(codex)).toHaveAttribute(
		"title",
		"Ready: installed and signed in",
	);
	await expect(codex.locator('[data-field="skills-note"]')).toContainText(
		"gh-fix stale",
	);
	await expect(codex.locator('[data-field="status-note"]')).toHaveCount(0);
	await expect(dot(gemini)).toHaveClass(/bg-amber-500/);
	await expect(dot(gemini)).toHaveAttribute(
		"title",
		"Needs attention: signed out",
	);
	await expect(gemini.locator('[data-field="status-note"]')).toHaveText(
		"signed out",
	);
	const agy = card.locator('[data-cli="agy"]');
	await expect(agy).toHaveAttribute("data-status", "unknown");
	await expect(dot(agy)).not.toHaveClass(/bg-green-500|bg-amber-500/);
	await expect(dot(agy)).toHaveAttribute(
		"title",
		"Not confirmed: sign-in state unknown",
	);

	// Absent CLIs, built-in or user-defined, share one compact line that
	// names each of them and expands to their per-CLI detail.
	const missing = card.getByTestId("providers-missing-clis");
	await expect(missing).toHaveCount(1);
	await expect(missing.locator("summary")).toHaveText(
		/Not installed:\s*acme, muse/,
	);
	await expect(card.locator('[data-cli="muse"]')).toBeHidden();
	await missing.locator("summary").click();
	for (const [name, binary] of [
		["muse", "muse"],
		["acme", "acme-cli"],
	]) {
		const row = missing.locator(`[data-cli="${name}"]`);
		await expect(row).toBeVisible();
		await expect(row).toHaveAttribute("data-status", "missing");
		await expect(row.locator('[data-field="install"]')).toContainText(
			`${binary} not on PATH`,
		);
		await expect(row.locator('[data-field="auth"]')).toHaveText(
			"not installed",
		);
	}
	await expect(missing.locator('[data-cli="muse"]')).toContainText(
		"any model the CLI accepts",
	);

	// Only installed CLIs keep full rows; the compact line is the last row,
	// so the Add provider row follows it directly.
	await expect(card.locator("tbody > tr[data-cli]")).toHaveCount(3);
	await expect(card.locator("tbody > tr").last()).toHaveAttribute(
		"data-testid",
		"providers-missing-clis",
	);
	await expect(card.getByTestId("provider-add-row")).toBeVisible();

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
		const selector = page.getByTestId(testId);
		await selector.locator("input").click();
		const options = selector.locator(".truncate.font-medium");
		// Only installed CLIs with models contribute: codex, not the missing
		// claude, acme and muse, nor gemini, which reports no models.
		await expect(options).toHaveText(offered);
		await page.keyboard.press("Escape");
	}

	const defaultModel = page.getByTestId("default-model-selector");
	await defaultModel.locator("input").click();
	await defaultModel.getByText("codex/gpt-5.5-mini", { exact: true }).click();
	await page.getByRole("button", { name: "Save Changes" }).click();
	await expect.poll(() => saved).not.toBeNull();
	expect(saved).toMatchObject({
		models: { defaults: { model: "codex/gpt-5.5-mini" } },
	});
});

test("an Add provider row expands into tiles of provider kinds that start the add flow", async ({
	page,
}) => {
	const toolCalls: Array<Record<string, unknown>> = [];
	await setup(page, undefined, {
		auth_set: (args?: Record<string, unknown>) => {
			toolCalls.push(args ?? {});
			return {};
		},
	});
	await page.goto("/settings/providers");
	const card = page.getByTestId("providers-list");
	await expect(card.locator("select")).toHaveCount(0);

	// The add row sits in the providers list itself, after the last provider.
	const row = card.getByTestId("provider-add-row");
	await expect(row).toHaveText(/Add provider/);
	await row.click();
	const tiles = card
		.getByRole("group", { name: "Provider kinds" })
		.getByRole("button");
	// The same kinds the select offered: Anthropic's stored API key is not offered again.
	await expect(card.locator('[data-kind="anthropic:oauth"]')).toBeVisible();
	await expect(card.locator('[data-kind="openai:apikey"]')).toContainText(
		"API Key",
	);
	await expect(card.locator('[data-kind="vllm:endpoint"]')).toContainText(
		"Endpoint",
	);
	await expect(card.locator('[data-kind="anthropic:apikey"]')).toHaveCount(0);

	// Arrow keys move between tiles; Escape collapses back to the focused row.
	await expect(tiles.first()).toBeFocused();
	await page.keyboard.press("ArrowRight");
	await expect(tiles.nth(1)).toBeFocused();
	await page.keyboard.press("End");
	await expect(tiles.last()).toBeFocused();
	await page.keyboard.press("Home");
	await expect(tiles.first()).toBeFocused();
	await page.keyboard.press("Escape");
	await expect(tiles).toHaveCount(0);
	await expect(row).toBeFocused();

	// An outside click collapses it too.
	await row.click();
	await expect(tiles.first()).toBeVisible();
	await page.getByText("Extra Secrets", { exact: true }).click();
	await expect(tiles).toHaveCount(0);

	// Choosing a kind from the keyboard opens that kind's form in place.
	await row.focus();
	await page.keyboard.press("Enter");
	await card.locator('[data-kind="openai:apikey"]').focus();
	await page.keyboard.press("Enter");
	const form = card.getByTestId("provider-add-form");
	await expect(form).toContainText("OpenAI (API Key)");
	await expect(row).toHaveCount(0);
	await form.getByPlaceholder("API key…").fill("sk-test");
	await form.getByRole("button", { name: "Add", exact: true }).click();
	await expect
		.poll(() => toolCalls)
		.toEqual([{ name: "openai:default", value: "sk-test" }]);
	await expect(form).toHaveCount(0);
	await expect(row).toBeVisible();

	// Cancel drops the chosen kind without adding anything.
	await row.click();
	await card.locator('[data-kind="vllm:endpoint"]').click();
	await expect(form.getByPlaceholder("Base URI (optional)")).toBeVisible();
	await form.getByTestId("provider-add-cancel").click();
	await expect(form).toHaveCount(0);
});
