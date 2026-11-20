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

test("providers lists every coding CLI with its doctor status beside the API providers", async ({
	page,
}) => {
	const calls = await setup(page);
	await page.goto("/settings/general");
	await expect(page.getByTestId("cli-providers")).toHaveCount(0);
	expect(calls).toEqual([]);

	await page.goto("/settings/providers");
	const card = page.getByTestId("cli-providers");
	await expect(card).toBeVisible();

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

	// The API providers' credential table is still there, and separate.
	const credentials = page.locator("table", { hasText: "Auth Type" });
	await expect(credentials.getByText("Anthropic")).toBeVisible();
	await expect(credentials.getByText("API Key")).toBeVisible();
	await expect(credentials.locator("[data-cli]")).toHaveCount(0);
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
