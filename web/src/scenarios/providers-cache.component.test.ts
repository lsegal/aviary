import { expect, test } from "vitest";
import { type Locator, page } from "vitest/browser";
import { goto, renderApp } from "../component-test/app";
import { mockMCP, type ToolFixtures } from "../component-test/server";

// From providers.spec.ts. Its own file: the CLI list is module state read
// from localStorage once, when the module loads, so the cached list must be
// in place before any test of the page loads it.

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

function setup(
	onSave?: (config: Record<string, unknown>) => void,
	extra: ToolFixtures = {},
) {
	const calls: string[] = [];
	mockMCP({
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
		config_save: (args?: { config?: unknown }) => {
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

const dot = (row: Locator) => row.locator('[data-field="status-dot"]');
const count = (loc: Locator) => loc.elements().length;

test("one providers list holds the API providers and every CLI provider with its doctor status", async () => {
	const calls = setup();
	// With a cached CLI list, the General tab reuses it instead of probing.
	localStorage.setItem("aviary.providers.clis", JSON.stringify(CLIS.clis));
	await renderApp("/system/settings");
	await expect
		.element(page.getByTestId("default-model-selector"))
		.toBeVisible();
	expect(calls).toEqual([]);

	await goto("/system/providers");
	const card = page.getByTestId("providers-list");
	await expect.element(card).toBeVisible();
	await expect
		.poll(() => count(page.getByText("CLI providers", { exact: true })))
		.toBe(0);

	const codex = card.locator('[data-cli="codex"]');
	await expect.element(codex).toHaveAttribute("data-status", "ok");
	await expect
		.element(codex.locator('[data-field="install"]'))
		.toMatchTextContent("codex-cli 0.130.0");
	await expect
		.element(codex.locator('[data-field="install"]'))
		.toMatchTextContent("/usr/local/bin/codex");
	await expect
		.element(codex.locator('[data-field="auth"]'))
		.toHaveText("signed in");
	await expect
		.element(codex.locator('[data-field="quota"]'))
		.toMatchTextContent("72% of weekly quota left");
	await expect
		.element(codex.locator('[data-field="models"]'))
		.toMatchTextContent("2 models");
	await codex.locator("summary").click();
	await expect.element(codex.getByText("codex/gpt-5.5-mini")).toBeVisible();

	const gemini = card.locator('[data-cli="gemini"]');
	await expect.element(gemini).toHaveAttribute("data-status", "warn");
	await expect
		.element(gemini.locator('[data-field="auth"]'))
		.toHaveText("signed out");

	// The dot follows the state the row shows (#495): a signed-in CLI is green
	// even with stale skills, which the row notes on their own; a warning names
	// its problem on the row and the dot; an unknown sign-in is never green.
	await expect.element(dot(codex)).toHaveClass(/bg-green-500/);
	await expect
		.element(dot(codex))
		.toHaveAttribute("title", "Ready: installed and signed in");
	await expect
		.element(codex.locator('[data-field="skills-note"]'))
		.toMatchTextContent("gh-fix stale");
	await expect
		.poll(() => count(codex.locator('[data-field="status-note"]')))
		.toBe(0);
	await expect.element(dot(gemini)).toHaveClass(/bg-amber-500/);
	await expect
		.element(dot(gemini))
		.toHaveAttribute("title", "Needs attention: signed out");
	await expect
		.element(gemini.locator('[data-field="status-note"]'))
		.toHaveText("signed out");
	const agy = card.locator('[data-cli="agy"]');
	await expect.element(agy).toHaveAttribute("data-status", "unknown");
	await expect.element(dot(agy)).not.toHaveClass(/bg-green-500|bg-amber-500/);
	await expect
		.element(dot(agy))
		.toHaveAttribute("title", "Not confirmed: sign-in state unknown");

	// Absent CLIs, built-in or user-defined, share one compact line that
	// names each of them and expands to their per-CLI detail.
	const missing = card.getByTestId("providers-missing-clis");
	await expect.poll(() => count(missing)).toBe(1);
	await expect
		.element(missing.locator("summary"))
		.toHaveText(/Not installed:\s*acme, muse/);
	await expect.element(card.locator('[data-cli="muse"]')).not.toBeVisible();
	await missing.locator("summary").click();
	for (const [name, binary] of [
		["muse", "muse"],
		["acme", "acme-cli"],
	]) {
		const row = missing.locator(`[data-cli="${name}"]`);
		await expect.element(row).toBeVisible();
		await expect.element(row).toHaveAttribute("data-status", "missing");
		await expect
			.element(row.locator('[data-field="install"]'))
			.toMatchTextContent(`${binary} not on PATH`);
		await expect
			.element(row.locator('[data-field="auth"]'))
			.toHaveText("not installed");
	}
	await expect
		.element(missing.locator('[data-cli="muse"]'))
		.toMatchTextContent("any model the CLI accepts");

	// Only installed CLIs keep full rows; the compact line is the last row,
	// so the Add provider row follows it directly.
	await expect.poll(() => count(card.locator("tbody > tr[data-cli]"))).toBe(3);
	await expect
		.element(card.locator("tbody > tr").last())
		.toHaveAttribute("data-testid", "providers-missing-clis");
	await expect.element(card.getByTestId("provider-add-row")).toBeVisible();

	// The API providers sit in the same list, ahead of the CLI providers.
	await expect.poll(() => count(card.locator("table"))).toBe(1);
	await expect.element(card.getByText("Anthropic")).toBeVisible();
	await expect.element(card.getByText("API Key")).toBeVisible();
	await expect.element(codex.getByText("CLI", { exact: true })).toBeVisible();
	await expect
		.element(card.locator("tbody tr").first())
		.toMatchTextContent("Anthropic");
}, 90_000);
