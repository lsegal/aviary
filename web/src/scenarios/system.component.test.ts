import { beforeEach, expect, test } from "vitest";
import { commands, type Locator, page } from "vitest/browser";
import { currentURL, renderApp } from "../component-test/app";
import { mockMCP, route } from "../component-test/server";
import { boundingBoxOf } from "./helpers/box";
import { chooseValue, expectValue } from "./helpers/select";
import { systemList } from "./helpers/sidebar";

/** Waits until locator matches n elements, as Playwright's toHaveCount. */
async function expectCount(locator: Locator, n: number) {
	await expect.poll(() => locator.elements().length).toBe(n);
}

beforeEach(() => {
	mockMCP({
		config_get: {
			server: {
				port: 16677,
				tls: { cert: "", key: "" },
				external_access: false,
				no_tls: false,
			},
			agents: [],
			models: { providers: {}, defaults: { model: "", fallbacks: [] } },
			browser: { binary: "", cdp_port: 0, reuse_tabs: true },
			scheduler: { concurrency: "" },
			skills: {
				gogcli: { enabled: true },
			},
		},
		config_save: {},
		coding_skills_list: {
			version: "2.0.0",
			source: "lsegal/aviary",
			release: true,
			clis: [],
			skills: [],
			outdated: 0,
		},
		skills_list: [
			{
				name: "gogcli",
				description: "Control GOG Galaxy tasks.",
				path: "skills/gogcli/SKILL.md",
				source: "builtin",
				enabled: true,
			},
			{
				name: "deploy",
				description: "Deployment checklist helpers.",
				path: "C:/skills/deploy/SKILL.md",
				source: "disk",
				enabled: false,
			},
		],
		tool_list: [
			{ name: "agent_list", description: "List configured agents." },
			{
				name: "task_update",
				description: "Update a scheduled task.",
				inputSchema: {
					type: "object",
					required: ["name"],
					properties: {
						name: {
							type: "string",
							description: "Task name to run immediately.",
						},
						enabled: {
							type: "boolean",
							description: "Whether the task runs on its schedule.",
						},
					},
				},
			},
			{ name: "config_get", description: "Read the active configuration." },
		],
		task_update: async (args?: Record<string, unknown>) => {
			await new Promise((resolve) => setTimeout(resolve, 150));
			return {
				ok: true,
				args,
			};
		},
	});
});

test("system tools shows grouped MCP tools and active skills", async () => {
	await renderApp("/system/tools");

	await expect.element(page.getByTestId("settings-title")).toHaveText("Tools");
	await expect.element(page.getByText("agent_list")).toBeVisible();
	await expect.element(page.getByText("task_update")).toBeVisible();
	await expect.element(
		page.getByRole("heading", { name: "Activated Skills" }),
	).toBeVisible();
	await expect.element(page.getByText("gogcli", { exact: true })).toBeVisible();
});

test("system tools can run a tool from the menu and show output", async () => {
	await renderApp("/system/tools");

	await page.getByTestId("run-tool-task_update").click();

	await expect.element(page.getByRole("heading", { name: "Run Tool" })).toBeVisible();
	await expect.element(
		page.getByRole("paragraph").filter({ hasText: "task_update" }),
	).toBeVisible();
	await expect.element(
		page.getByPlaceholder("Task name to run immediately."),
	).toBeVisible();

	await page.getByPlaceholder("Task name to run immediately.").fill("nightly");
	await chooseValue(page.getByRole("combobox").nth(0), "true");
	await page.getByRole("button", { name: "Run Tool" }).click();

	await expect.element(page.getByTestId("tool-run-output")).toMatchTextContent('"ok":true');
	await expect.element(page.getByTestId("tool-run-output")).toMatchTextContent(
		'"name":"nightly"',
	);
	await expect.element(page.getByTestId("tool-run-output")).toMatchTextContent(
		'"enabled":true',
	);

	await page.getByRole("button", { name: "Close" }).click();
	await page.getByTestId("run-tool-task_update").click();

	await expect.element(
		page.getByPlaceholder("Task name to run immediately."),
	).toHaveValue("nightly");
	await expectValue(page.getByRole("combobox").nth(0), "true");
	await expect.element(page.getByTestId("tool-run-output")).toMatchTextContent('"ok":true');

	await page.getByPlaceholder("Task name to run immediately.").fill("weekly");
	await page.getByRole("button", { name: "Run Tool" }).click();
	await expect.element(page.getByTestId("tool-run-output")).toHaveClass(
		/text-gray-400/,
	);
	await expect.element(page.getByTestId("tool-run-output")).toMatchTextContent('"ok":true');
	await expect.element(page.getByTestId("tool-run-output")).toMatchTextContent(
		'"name":"weekly"',
	);
});

test("system skills can disable and enable installed skills", async () => {
	await renderApp("/system/skills");

	await expect.element(
		page.getByRole("heading", { name: "Skills", exact: true }),
	).toBeVisible();
	// The skills browser sits on the same page as both installed lists.
	const discovery = page.getByTestId("skills-discovery");
	await expect.element(discovery).toMatchTextContent("Looking for more skills?");
	await expect.element(discovery).toMatchTextContent("npx skills find notion");
	await expect.element(
		discovery.getByRole("link", { name: "Open skills.sh" }),
	).toHaveAttribute("href", "https://skills.sh/");
	await expect.element(
		page.getByRole("region", { name: "Installed in providers" }),
	).toBeVisible();
	const stats = page.getByTestId("skill-stats");
	await expect.element(stats).toMatchTextContent("Installed2");
	await expect.element(stats).toMatchTextContent("Enabled1");
	await expect.element(stats).toMatchTextContent("Showing2");
	const gogcli = page
		.getByRole("region", { name: "Available to Aviary agents" })
		.locator("div.rounded-xl")
		.filter({ hasText: "Control GOG Galaxy tasks." });
	const enabled = gogcli.getByRole("checkbox", { name: "Enabled" });
	await expect.element(enabled).toBeChecked();
	await enabled.click();
	await expect.element(enabled).not.toBeChecked();
	await expect.element(gogcli.getByText("disabled", { exact: true })).toBeVisible();

	// The status filter narrows the list to the disabled skills.
	await page.getByRole("button", { name: "Disabled", exact: true }).click();
	await expect.element(page.getByText("C:/skills/deploy/SKILL.md")).toBeVisible();
	await expect.element(page.getByText("skills/gogcli/SKILL.md")).toBeVisible();
	await page.getByRole("button", { name: "Enabled", exact: true }).click();
	await expect.element(page.getByText("No skills match.")).toBeVisible();
});

test("settings leaves server and cdp ports unset until the user enters them", async () => {
	let savedConfig: Record<string, unknown> | null = null;
	mockMCP({
		config_get: {
			server: {
				tls: { cert: "", key: "" },
				external_access: false,
				no_tls: false,
			},
			agents: [],
			models: { providers: {}, defaults: { model: "", fallbacks: [] } },
			browser: { binary: "", reuse_tabs: true },
			scheduler: { concurrency: "" },
			skills: {},
		},
		config_save: (args) => {
			savedConfig = JSON.parse(String(args?.config ?? "{}")) as Record<
				string,
				unknown
			>;
			return {};
		},
		skills_list: [],
		tool_list: [],
	});

	await renderApp("/system/settings");

	const portInput = page.getByPlaceholder("16677");
	const cdpPortInput = page.getByPlaceholder("9222");

	await expect.element(portInput).toHaveValue("");
	await expect.element(cdpPortInput).toHaveValue("");

	await portInput.fill("12a3");
	await cdpPortInput.fill("9b2c2d");
	await expect.element(portInput).toHaveValue("123");
	await expect.element(cdpPortInput).toHaveValue("922");

	await portInput.clear();
	await cdpPortInput.clear();
	const expose = page.getByRole("checkbox", {
		name: /Expose service externally/i,
	});
	await expose.click();
	await expect.element(expose).toBeChecked();
	await page.getByRole("button", { name: "Save Changes" }).click();

	await expect.poll(() => savedConfig).not.toBeNull();
	expect(savedConfig).toMatchObject({
		server: {
			port: 0,
			external_access: true,
		},
		browser: {
			cdp_port: 0,
		},
	});
});

test("settings can disable browser tab reuse", async () => {
	let savedConfig: Record<string, unknown> | null = null;
	mockMCP({
		config_get: {
			server: {
				port: 16677,
				tls: { cert: "", key: "" },
				external_access: false,
				no_tls: false,
			},
			agents: [],
			models: { providers: {}, defaults: { model: "", fallbacks: [] } },
			browser: { binary: "", cdp_port: 9222 },
			scheduler: { concurrency: "" },
			skills: {},
		},
		config_save: (args) => {
			savedConfig = JSON.parse(String(args?.config ?? "{}")) as Record<
				string,
				unknown
			>;
			return {};
		},
		skills_list: [],
		tool_list: [],
	});

	await renderApp("/system/settings");

	const reuseTabs = page.getByRole("checkbox", {
		name: /Reuse matching tabs/i,
	});
	const saveButton = page.getByRole("button", { name: "Save Changes" });
	const cdpPortInput = page.getByPlaceholder("9222");
	await expect.element(reuseTabs).toBeChecked();
	await reuseTabs.click();
	await expect.element(reuseTabs).not.toBeChecked();
	await cdpPortInput.fill("9333");
	await expect.element(saveButton).toBeEnabled();
	await saveButton.click();

	await expect.poll(() => savedConfig).not.toBeNull();
	expect(savedConfig).toMatchObject({
		browser: {
			cdp_port: 9333,
			reuse_tabs: false,
		},
	});
});

async function setupApprovalSettings(
	approval: Record<string, string>,
): Promise<Record<string, unknown>[]> {
	const saved: Record<string, unknown>[] = [];
	mockMCP({
		config_get: {
			server: { port: 16677 },
			agents: [],
			models: { providers: {}, defaults: { model: "", fallbacks: [] } },
			browser: { binary: "" },
			scheduler: { concurrency: "" },
			skills: {},
			approval,
		},
		config_save: (args) => {
			saved.push(JSON.parse(String(args?.config ?? "{}")));
			return {};
		},
		skills_list: [],
		tool_list: [],
	});
	await renderApp("/system/settings");
	return saved;
}

test("settings sets the chat approval mode over the general one", async () => {
	const saved = await setupApprovalSettings({ mode: "read-only" });
	await expectValue(page.getByTestId("approval-mode"), "read-only");
	const chat = page.getByTestId("approval-chat");
	await expectValue(chat, "");
	await chooseValue(chat, "full");
	await page.getByRole("button", { name: "Save Changes" }).click();
	await expect.poll(() => saved.length).toBe(1);
	expect(saved[0].approval).toEqual({ mode: "read-only", chat: "full" });
});

test("settings drops the approval section at its defaults", async () => {
	const saved = await setupApprovalSettings({
		mode: "read-only",
		chat: "full",
	});
	const chat = page.getByTestId("approval-chat");
	await expectValue(chat, "full");
	await chooseValue(page.getByTestId("approval-mode"), "");
	await chooseValue(chat, "");
	await page.getByRole("button", { name: "Save Changes" }).click();
	await expect.poll(() => saved.length).toBe(1);
	expect(saved[0].approval).toBeUndefined();
});

test("settings tolerates an empty config payload", async () => {
	mockMCP({
		config_get: "",
		skills_list: [],
		tool_list: [],
	});

	await renderApp("/system/settings");

	await expect.element(
		page.getByRole("heading", { name: "Settings", exact: true }),
	).toBeVisible();
	await expect.element(
		page.getByRole("heading", { name: "Server", exact: true }),
	).toBeVisible();
	await expectCount(page.getByText("Unexpected end of JSON input"), 0);
});

test("settings tolerates empty auxiliary JSON payloads on startup", async () => {
	mockMCP({
		config_get: {
			server: {
				port: 16677,
				tls: { cert: "", key: "" },
				external_access: false,
				no_tls: false,
			},
			agents: [],
			models: { providers: {}, defaults: { model: "", fallbacks: [] } },
			browser: { binary: "", cdp_port: 0, reuse_tabs: true },
			scheduler: { concurrency: "" },
			skills: {},
		},
		auth_list: "",
		skills_list: "",
		agent_list: "",
		tool_list: [],
	});

	await renderApp("/system/settings");

	await expectCount(
		page.getByRole("heading", { name: "Settings", exact: true }),
	).toBeVisible();
	await expect.element(
		page.getByRole("heading", { name: "Server", exact: true }),
	).toBeVisible();
	await expectCount(page.getByText("Unexpected end of JSON input"), 0);
});

test("system skills shows provider installs and installs them on one page", async () => {
	const installs: Record<string, { version?: string; current: boolean }> = {
		"gh-fix/claude": { version: "2.0.0", current: true },
		"gh-fix/codex": { version: "1.5.16", current: false },
		"gh-discuss/claude": { version: "2.0.0", current: true },
		"gh-discuss/codex": { current: false },
		"gh-create/claude": { version: "2.0.0", current: true },
	};
	const installCalls: Record<string, unknown>[] = [];
	const savedConfigs: string[] = [];
	const status = () => {
		const skills = ["gh-fix", "gh-discuss", "gh-create"].map((name) => ({
			name,
			summary: `${name} summary`,
			run_by_projects: name !== "gh-create",
			installs: ["claude", "codex"].map((cli) => {
				const entry = installs[`${name}/${cli}`];
				return {
					cli,
					installed: Boolean(entry),
					current: entry?.current ?? false,
					version: entry?.version,
				};
			}),
		}));
		return {
			version: "2.0.0",
			source: "lsegal/aviary",
			release: true,
			clis: [
				{ id: "claude", name: "Claude", target: "claude-code" },
				{ id: "codex", name: "Codex", target: "codex" },
			],
			skills,
			outdated: skills
				.flatMap((skill) => skill.installs)
				.filter((entry) => entry.installed && !entry.current).length,
		};
	};
	mockMCP({
		config_get: {
			server: { port: 16677 },
			agents: [],
			models: { providers: {}, defaults: { model: "", fallbacks: [] } },
			browser: {},
			scheduler: { concurrency: "" },
			skills: {},
		},
		config_save: async (args) => {
			savedConfigs.push(String(args?.config));
			return {};
		},
		skills_list: [],
		coding_skills_list: async () => status(),
		coding_skills_install: async (
			args: { skill?: string; cli?: string; update_only?: boolean } = {},
		) => {
			installCalls.push(args);
			const installed: string[] = [];
			for (const key of Object.keys(installs).concat(
				args.skill && args.cli ? [`${args.skill}/${args.cli}`] : [],
			)) {
				const [skill, cli] = key.split("/");
				if (args.skill && args.skill !== skill) continue;
				if (args.cli && args.cli !== cli) continue;
				if (installs[key]?.current) continue;
				if (!installs[key] && args.update_only) continue;
				installs[key] = { version: "2.0.0", current: true };
				installed.push(key);
			}
			return { installed };
		},
	});

	await renderApp("/system/skills");

	await expectCount(page.getByRole("tab"), 0);
	await expectCount(
		page.getByRole("region", { name: "Installed in providers" }),
	).toBeVisible();
	await expect.element(
		page.getByRole("region", { name: "Available to Aviary agents" }),
	).toBeVisible();
	await expect.element(page.getByTestId("coding-skill-gh-fix-claude")).toMatchTextContent(
		"2.0.0",
	);
	await expect.element(page.getByTestId("coding-skill-gh-fix-codex")).toMatchTextContent(
		"1.5.16",
	);
	await expect.element(page.getByTestId("coding-skill-gh-discuss-codex")).toMatchTextContent(
		"Older copy",
	);
	await expect.element(page.getByTestId("coding-skill-gh-create-codex")).toMatchTextContent(
		"Not installed",
	);
	await expect.element(page.getByTestId("coding-skill-gh-fix")).toMatchTextContent(
		"run by Code Loop",
	);
	await expect.element(page.getByTestId("coding-skill-gh-create")).toMatchTextContent(
		"you run it",
	);

	await page
		.getByTestId("coding-skill-gh-create-codex")
		.getByRole("button", { name: "Install" })
		.click();
	await expect.element(page.getByText("Installed gh-create/codex.")).toBeVisible();
	await expect.element(page.getByTestId("coding-skill-gh-create-codex")).toMatchTextContent(
		"2.0.0",
	);
	expect(installCalls[0]).toEqual({ skill: "gh-create", cli: "codex" });

	await page
		.getByTestId("coding-skill-gh-fix-codex")
		.getByRole("button", { name: "Update to 2.0.0" })
		.click();
	await expect.element(page.getByText("Installed gh-fix/codex.")).toBeVisible();

	const updateAll = page.getByTestId("coding-skills-update-all");
	await expect.element(updateAll).toHaveText("Update all (1)");
	await updateAll.click();
	await expect.element(page.getByText("Installed gh-discuss/codex.")).toBeVisible();
	expect(installCalls[2]).toEqual({ update_only: true });
	await expect.element(updateAll).toHaveText("Update all (0)");
	await expect.element(updateAll).toBeDisabled();

	await expect.element(page.getByTestId("coding-skills-install-command")).toHaveText(
		"aviary install-skills",
	);
	await expect.element(
		page.getByText("Aviary runs it automatically on first launch."),
	).toBeVisible();

	const autoUpdate = page.getByTestId("coding-skills-auto-update");
	await autoUpdate.click();
	await expect.element(autoUpdate).not.toBeChecked();
	await expect
		.poll(() => savedConfigs.at(-1) ?? "")
		.toContain('"coding_skills":{"auto_update":false}');
});

test("system skills shows its skills when opened from another settings page", async () => {
	await renderApp("/system/settings");
	await (await systemList())
		.getByRole("link", { name: "Skills", exact: true })
		.click();

	const agentSkills = page.getByRole("region", {
		name: "Available to Aviary agents",
	});
	await expect.element(agentSkills).toBeVisible();
	await expect.element(agentSkills.getByText("skills/gogcli/SKILL.md")).toBeVisible();
	await expect.element(
		page.getByRole("heading", { name: "Server", exact: true }),
	).not.toBeInTheDocument();
});

async function mockServerSettings(
	server: Record<string, unknown> = {},
): Promise<{ saved: () => Record<string, unknown> | null }> {
	let savedConfig: Record<string, unknown> | null = null;
	mockMCP({
		config_get: {
			server: {
				port: 16677,
				tls: { cert: "", key: "" },
				external_access: false,
				no_tls: false,
				...server,
			},
			agents: [],
			models: { providers: {}, defaults: { model: "", fallbacks: [] } },
			browser: { binary: "", cdp_port: 9222, reuse_tabs: true },
			scheduler: { concurrency: "" },
			skills: {},
		},
		config_save: (args) => {
			savedConfig = JSON.parse(String(args?.config ?? "{}")) as Record<
				string,
				unknown
			>;
			return {};
		},
		skills_list: [],
		tool_list: [],
	});
	return { saved: () => savedConfig };
}

function disableTokenAuthCheckbox() {
	// The modal hides the page from the accessibility tree while open, so find
	// the box by test id rather than by role.
	return page.getByTestId("disable-token-auth");
}

test("checking Disable token authentication asks for confirmation first (#877)", async () => {
	const { saved } = await mockServerSettings();
	await renderApp("/system/settings");

	const checkbox = disableTokenAuthCheckbox();
	await expect.element(checkbox).not.toBeChecked();
	await expect.element(
		page.getByText("anyone who can reach the server can use it"),
	).toBeVisible();

	await checkbox.click();
	const modal = page.getByTestId("disable-token-auth-modal");
	await expect.element(modal).toBeVisible();
	await expect.element(modal).toMatchTextContent(
		"anyone who can reach the service can use it without a token",
	);
	await expect.element(modal).toMatchTextContent(
		'The risk is greater when "Expose service externally" is on or TLS is disabled.',
	);
	await expect.element(checkbox).not.toBeChecked();

	await modal.getByRole("button", { name: "Cancel" }).click();
	await expect.element(modal).not.toBeInTheDocument();
	await expect.element(checkbox).not.toBeChecked();

	await checkbox.click();
	await page
		.getByTestId("disable-token-auth-modal")
		.getByRole("button", { name: "Disable token authentication" })
		.click();
	await expect.element(page.getByTestId("disable-token-auth-modal")).not.toBeInTheDocument();
	await expect.element(checkbox).toBeChecked();
	await expect.element(
		page.getByText("Changing server settings will restart the service."),
	).toBeVisible();

	await page.getByRole("button", { name: "Save Changes" }).click();
	await expect.poll(saved).toMatchObject({
		server: { disable_token_auth: true },
	});
});

test("dismissing the Disable token authentication modal changes nothing (#877)", async () => {
	const { saved } = await mockServerSettings({ external_access: true });
	await renderApp("/system/settings");

	const checkbox = disableTokenAuthCheckbox();
	await checkbox.click();
	const modal = page.getByTestId("disable-token-auth-modal");
	await expect.element(modal).toBeVisible();
	await expect.element(page.getByTestId("disable-token-auth-extra-risk")).toMatchTextContent(
		"exposed externally",
	);
	await commands.keyPress("Escape");
	await expect.element(modal).toBeHidden();
	await expect.element(checkbox).not.toBeChecked();

	// Change something else so the save goes through, and confirm the
	// dismissed modal left the setting out.
	const noTLS = page.getByRole("checkbox", { name: /Disable TLS/i });
	await noTLS.click();
	await expect.element(noTLS).toBeChecked();
	await page.getByRole("button", { name: "Save Changes" }).click();
	await expect.poll(saved).not.toBeNull();
	const server = saved()?.server as Record<string, unknown> | undefined;
	expect(server?.no_tls).toBe(true);
	expect(server?.disable_token_auth).toBeUndefined();
});

test("unchecking Disable token authentication needs no confirmation (#877)", async () => {
	const { saved } = await mockServerSettings({
		disable_token_auth: true,
	});
	await renderApp("/system/settings");

	const checkbox = disableTokenAuthCheckbox();
	await expect.element(checkbox).toBeChecked();
	await checkbox.click();
	await expectCount(page.getByTestId("disable-token-auth-modal"), 0);
	await expect.element(checkbox).not.toBeChecked();

	await page.getByRole("button", { name: "Save Changes" }).click();
	await expect.poll(saved).not.toBeNull();
	expect(
		(saved()?.server as Record<string, unknown> | undefined)
			?.disable_token_auth,
	).toBeUndefined();
});

test("system tools shows voice transcription deps and installs them", async () => {
	const missing = {
		enabled: true,
		engine: "local",
		language: "auto",
		ready: false,
		missing: ["whisper-cli", "whisper model base.en"],
		install: "run `aviary deps install`, or Install on the System > Tools page",
		status: {
			whisper: { name: "whisper-cli", installed: false },
			ffmpeg: {
				name: "ffmpeg",
				installed: true,
				path: "/opt/homebrew/bin/ffmpeg",
			},
			model: {
				name: "base.en",
				downloaded: false,
				path: "/data/deps/whisper/ggml-base.en.bin",
			},
		},
	};
	const installed = {
		...missing,
		ready: true,
		missing: [],
		install: undefined,
		status: {
			...missing.status,
			whisper: {
				name: "whisper-cli",
				installed: true,
				path: "/opt/homebrew/bin/whisper-cli",
			},
			model: { ...missing.status.model, downloaded: true },
		},
	};
	const methods: string[] = [];
	route("/api/transcription", (r) => {
		methods.push(r.request().method());
		return r.fulfill({
			json: r.request().method() === "POST" ? installed : missing,
		});
	});
	await renderApp("/system/tools");

	const card = page.getByTestId("transcription-status");
	await expect.element(card.getByTestId("transcription-engine")).toMatchTextContent(
		"Missing",
	);
	await expect.element(card.getByTestId("transcription-model")).toMatchTextContent(
		"Model base.en",
	);
	await expect.element(card.getByTestId("transcription-model")).toMatchTextContent(
		"Missing",
	);
	await expect.element(card.getByTestId("transcription-ffmpeg")).toMatchTextContent(
		"/opt/homebrew/bin/ffmpeg",
	);

	await card.getByTestId("transcription-install").click();
	await expect.element(card.getByTestId("transcription-engine")).toMatchTextContent(
		"Installed",
	);
	await expect.element(card.getByTestId("transcription-model")).toMatchTextContent(
		"Downloaded",
	);
	await expectCount(card.getByTestId("transcription-install"), 0);
	expect(methods).toEqual(["GET", "POST"]);
});

test("every System page header links to Settings with a gear (#932)", async () => {
	for (const path of [
		"/system/overview",
		"/system/usage",
		"/system/tools",
		"/system/skills",
		"/system/sessions",
		"/system/models",
		"/system/logs",
		"/system/daemons",
	]) {
		await renderApp(path);
		const gear = page.getByTestId("system-settings-link");
		await expect.element(gear).toBeVisible();
		await expect.element(gear).toHaveAccessibleName("Settings");
		await expect.element(gear).toHaveAttribute("title", "Settings");
		await expect.element(gear).toHaveAttribute("href", "/system/settings");
	}
});

test("the System gear opens Settings, which shows no gear (#932)", async () => {
	await renderApp("/system/overview");
	await page.getByRole("link", { name: "Settings", exact: true }).click();
	await expect.poll(currentURL).toMatch(/\/system\/settings$/);
	await expect.element(page.getByTestId("settings-title")).toHaveText("Settings");
	await expectCount(page.getByRole("dialog"), 0);
	await expectCount(page.getByTestId("system-settings-link"), 0);

	await renderApp("/system/providers");
	await expect.element(page.getByTestId("settings-title")).toBeVisible();
	await expectCount(page.getByTestId("system-settings-link"), 0);
});

test("the System gear fits beside header actions on a phone (#932)", async () => {
	await page.viewport(390, 844);
	for (const path of [
		"/system/overview",
		"/system/usage",
		"/system/tools",
		"/system/skills",
		"/system/sessions",
		"/system/models",
		"/system/logs",
		"/system/daemons",
	]) {
		await renderApp(path);
		const gear = page.getByTestId("system-settings-link");
		await expect.element(gear).toBeVisible();
		const box = await boundingBoxOf(gear);
		expect(box.x + box.width, path).toBeLessThanOrEqual(390);
		const refresh = page.getByRole("button", { name: /^(Refresh|Loading)/ });
		if (refresh.elements().length) {
			const r = await boundingBoxOf(refresh.first());
			// The gear sits right of Refresh or on its own row, never on it.
			const overlaps =
				r.x < box.x + box.width &&
				box.x < r.x + r.width &&
				r.y < box.y + box.height &&
				box.y < r.y + r.height;
			expect(overlaps, path).toBe(false);
		}
	}
});
