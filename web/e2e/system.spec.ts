import { expect, type Page, test } from "@playwright/test";
import { mockMCP, setAuthToken } from "./helpers/mockMCP";
import { chooseValue, expectValue } from "./helpers/select";
import { systemList } from "./helpers/sidebar";

test.beforeEach(async ({ page }) => {
	await setAuthToken(page);
	await mockMCP(page, {
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

test("system tools shows grouped MCP tools and active skills", async ({
	page,
}) => {
	await page.goto("/system/tools");

	await expect(page.getByTestId("page-title")).toHaveText("Tools");
	await expect(page.getByText("agent_list")).toBeVisible();
	await expect(page.getByText("task_update")).toBeVisible();
	await expect(
		page.getByRole("heading", { name: "Activated Skills" }),
	).toBeVisible();
	await expect(page.getByText("gogcli", { exact: true })).toBeVisible();
});

test("system tools can run a tool from the menu and show output", async ({
	page,
}) => {
	await page.goto("/system/tools");

	await page.getByTestId("run-tool-task_update").click();

	await expect(page.getByRole("heading", { name: "Run Tool" })).toBeVisible();
	await expect(
		page.getByRole("paragraph").filter({ hasText: "task_update" }),
	).toBeVisible();
	await expect(
		page.getByPlaceholder("Task name to run immediately."),
	).toBeVisible();

	await page.getByPlaceholder("Task name to run immediately.").fill("nightly");
	await chooseValue(page.getByRole("combobox").nth(0), "true");
	await page.getByRole("button", { name: "Run Tool" }).click();

	await expect(page.getByTestId("tool-run-output")).toContainText('"ok":true');
	await expect(page.getByTestId("tool-run-output")).toContainText(
		'"name":"nightly"',
	);
	await expect(page.getByTestId("tool-run-output")).toContainText(
		'"enabled":true',
	);

	await page.getByRole("button", { name: "Close" }).click();
	await page.getByTestId("run-tool-task_update").click();

	await expect(
		page.getByPlaceholder("Task name to run immediately."),
	).toHaveValue("nightly");
	await expectValue(page.getByRole("combobox").nth(0), "true");
	await expect(page.getByTestId("tool-run-output")).toContainText('"ok":true');

	await page.getByPlaceholder("Task name to run immediately.").fill("weekly");
	await page.getByRole("button", { name: "Run Tool" }).click();
	await expect(page.getByTestId("tool-run-output")).toHaveClass(
		/text-gray-400/,
	);
	await expect(page.getByTestId("tool-run-output")).toContainText('"ok":true');
	await expect(page.getByTestId("tool-run-output")).toContainText(
		'"name":"weekly"',
	);
});

test("system skills can disable and enable installed skills", async ({
	page,
}) => {
	await page.goto("/system/skills");

	await expect(
		page.getByRole("heading", { name: "Skills", exact: true }),
	).toBeVisible();
	// The skills browser sits on the same page as both installed lists.
	const discovery = page.getByTestId("skills-discovery");
	await expect(discovery).toContainText("Looking for more skills?");
	await expect(discovery).toContainText("npx skills find notion");
	await expect(
		discovery.getByRole("link", { name: "Open skills.sh" }),
	).toHaveAttribute("href", "https://skills.sh/");
	await expect(
		page.getByRole("region", { name: "Installed in providers" }),
	).toBeVisible();
	const stats = page.getByTestId("skill-stats");
	await expect(stats).toContainText("Installed2");
	await expect(stats).toContainText("Enabled1");
	await expect(stats).toContainText("Showing2");
	const gogcli = page
		.getByRole("region", { name: "Available to Aviary agents" })
		.locator("div.rounded-xl")
		.filter({ hasText: "Control GOG Galaxy tasks." });
	const enabled = gogcli.getByRole("checkbox", { name: "Enabled" });
	await expect(enabled).toBeChecked();
	await enabled.uncheck();
	await expect(gogcli.getByText("disabled", { exact: true })).toBeVisible();

	// The status filter narrows the list to the disabled skills.
	await page.getByRole("button", { name: "Disabled", exact: true }).click();
	await expect(page.getByText("C:/skills/deploy/SKILL.md")).toBeVisible();
	await expect(page.getByText("skills/gogcli/SKILL.md")).toBeVisible();
	await page.getByRole("button", { name: "Enabled", exact: true }).click();
	await expect(page.getByText("No skills match.")).toBeVisible();
});

test("settings leaves server and cdp ports unset until the user enters them", async ({
	page,
}) => {
	let savedConfig: Record<string, unknown> | null = null;
	await setAuthToken(page);
	await mockMCP(page, {
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

	await page.goto("/system/settings");

	const portInput = page.getByPlaceholder("16677");
	const cdpPortInput = page.getByPlaceholder("9222");

	await expect(portInput).toHaveValue("");
	await expect(cdpPortInput).toHaveValue("");

	await portInput.fill("12a3");
	await cdpPortInput.fill("9b2c2d");
	await expect(portInput).toHaveValue("123");
	await expect(cdpPortInput).toHaveValue("922");

	await portInput.clear();
	await cdpPortInput.clear();
	await page
		.getByRole("checkbox", { name: /Expose service externally/i })
		.check();
	await page.getByRole("button", { name: "Save Changes" }).click();

	expect(savedConfig).not.toBeNull();
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

test("settings can disable browser tab reuse", async ({ page }) => {
	let savedConfig: Record<string, unknown> | null = null;
	await setAuthToken(page);
	await mockMCP(page, {
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

	await page.goto("/system/settings");

	const reuseTabs = page.getByRole("checkbox", {
		name: /Reuse matching tabs/i,
	});
	const saveButton = page.getByRole("button", { name: "Save Changes" });
	const cdpPortInput = page.getByPlaceholder("9222");
	await expect(reuseTabs).toBeChecked();
	await reuseTabs.uncheck();
	await cdpPortInput.fill("9333");
	await expect(saveButton).toBeEnabled();
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
	page: Page,
	approval: Record<string, string>,
): Promise<Record<string, unknown>[]> {
	const saved: Record<string, unknown>[] = [];
	await setAuthToken(page);
	await mockMCP(page, {
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
	await page.goto("/system/settings");
	return saved;
}

test("settings sets the chat approval mode over the general one", async ({
	page,
}) => {
	const saved = await setupApprovalSettings(page, { mode: "read-only" });
	await expectValue(page.getByTestId("approval-mode"), "read-only");
	const chat = page.getByTestId("approval-chat");
	await expectValue(chat, "");
	await chooseValue(chat, "full");
	await page.getByRole("button", { name: "Save Changes" }).click();
	await expect.poll(() => saved.length).toBe(1);
	expect(saved[0].approval).toEqual({ mode: "read-only", chat: "full" });
});

test("settings drops the approval section at its defaults", async ({
	page,
}) => {
	const saved = await setupApprovalSettings(page, {
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

test("settings tolerates an empty config payload", async ({ page }) => {
	await setAuthToken(page);
	await mockMCP(page, {
		config_get: "",
		skills_list: [],
		tool_list: [],
	});

	await page.goto("/system/settings");

	await expect(
		page.getByRole("heading", { name: "Settings", exact: true }),
	).toBeVisible();
	await expect(
		page.getByRole("heading", { name: "Server", exact: true }),
	).toBeVisible();
	await expect(page.getByText("Unexpected end of JSON input")).toHaveCount(0);
});

test("settings tolerates empty auxiliary JSON payloads on startup", async ({
	page,
}) => {
	await setAuthToken(page);
	await mockMCP(page, {
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

	await page.goto("/system/settings");

	await expect(
		page.getByRole("heading", { name: "Settings", exact: true }),
	).toBeVisible();
	await expect(
		page.getByRole("heading", { name: "Server", exact: true }),
	).toBeVisible();
	await expect(page.getByText("Unexpected end of JSON input")).toHaveCount(0);
});

test("system skills shows provider installs and installs them on one page", async ({
	page,
}) => {
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
	await mockMCP(page, {
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

	await page.goto("/system/skills");

	await expect(page.getByRole("tab")).toHaveCount(0);
	await expect(
		page.getByRole("region", { name: "Installed in providers" }),
	).toBeVisible();
	await expect(
		page.getByRole("region", { name: "Available to Aviary agents" }),
	).toBeVisible();
	await expect(page.getByTestId("coding-skill-gh-fix-claude")).toContainText(
		"2.0.0",
	);
	await expect(page.getByTestId("coding-skill-gh-fix-codex")).toContainText(
		"1.5.16",
	);
	await expect(page.getByTestId("coding-skill-gh-discuss-codex")).toContainText(
		"Older copy",
	);
	await expect(page.getByTestId("coding-skill-gh-create-codex")).toContainText(
		"Not installed",
	);
	await expect(page.getByTestId("coding-skill-gh-fix")).toContainText(
		"run by Code Loop",
	);
	await expect(page.getByTestId("coding-skill-gh-create")).toContainText(
		"you run it",
	);

	await page
		.getByTestId("coding-skill-gh-create-codex")
		.getByRole("button", { name: "Install" })
		.click();
	await expect(page.getByText("Installed gh-create/codex.")).toBeVisible();
	await expect(page.getByTestId("coding-skill-gh-create-codex")).toContainText(
		"2.0.0",
	);
	expect(installCalls[0]).toEqual({ skill: "gh-create", cli: "codex" });

	await page
		.getByTestId("coding-skill-gh-fix-codex")
		.getByRole("button", { name: "Update to 2.0.0" })
		.click();
	await expect(page.getByText("Installed gh-fix/codex.")).toBeVisible();

	const updateAll = page.getByTestId("coding-skills-update-all");
	await expect(updateAll).toHaveText("Update all (1)");
	await updateAll.click();
	await expect(page.getByText("Installed gh-discuss/codex.")).toBeVisible();
	expect(installCalls[2]).toEqual({ update_only: true });
	await expect(updateAll).toHaveText("Update all (0)");
	await expect(updateAll).toBeDisabled();

	await expect(page.getByTestId("coding-skills-install-command")).toHaveText(
		"aviary install-skills",
	);
	await expect(
		page.getByText("Aviary runs it automatically on first launch."),
	).toBeVisible();

	await page.getByTestId("coding-skills-auto-update").uncheck();
	await expect
		.poll(() => savedConfigs.at(-1) ?? "")
		.toContain('"coding_skills":{"auto_update":false}');
});

test("system skills shows its skills when opened from another settings page", async ({
	page,
}) => {
	await page.goto("/system/settings");
	await (await systemList(page))
		.getByRole("link", { name: "Skills", exact: true })
		.click();

	const agentSkills = page.getByRole("region", {
		name: "Available to Aviary agents",
	});
	await expect(agentSkills).toBeVisible();
	await expect(agentSkills.getByText("skills/gogcli/SKILL.md")).toBeVisible();
	await expect(
		page.getByRole("heading", { name: "Server", exact: true }),
	).toBeHidden();
});

async function mockServerSettings(
	page: Page,
	server: Record<string, unknown> = {},
): Promise<{ saved: () => Record<string, unknown> | null }> {
	let savedConfig: Record<string, unknown> | null = null;
	await setAuthToken(page);
	await mockMCP(page, {
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

function disableTokenAuthCheckbox(page: Page) {
	// The modal hides the page from the accessibility tree while open, so find
	// the box by test id rather than by role.
	return page.getByTestId("disable-token-auth");
}

test("checking Disable token authentication asks for confirmation first (#877)", async ({
	page,
}) => {
	const { saved } = await mockServerSettings(page);
	await page.goto("/system/settings");

	const checkbox = disableTokenAuthCheckbox(page);
	await expect(checkbox).not.toBeChecked();
	await expect(
		page.getByText("anyone who can reach the server can use it"),
	).toBeVisible();

	await checkbox.click();
	const modal = page.getByTestId("disable-token-auth-modal");
	await expect(modal).toBeVisible();
	await expect(modal).toContainText(
		"anyone who can reach the service can use it without a token",
	);
	await expect(modal).toContainText(
		'The risk is greater when "Expose service externally" is on or TLS is disabled.',
	);
	await expect(checkbox).not.toBeChecked();

	await modal.getByRole("button", { name: "Cancel" }).click();
	await expect(modal).toBeHidden();
	await expect(checkbox).not.toBeChecked();

	await checkbox.click();
	await page
		.getByTestId("disable-token-auth-modal")
		.getByRole("button", { name: "Disable token authentication" })
		.click();
	await expect(page.getByTestId("disable-token-auth-modal")).toBeHidden();
	await expect(checkbox).toBeChecked();
	await expect(
		page.getByText("Changing server settings will restart the service."),
	).toBeVisible();

	await page.getByRole("button", { name: "Save Changes" }).click();
	await expect.poll(saved).toMatchObject({
		server: { disable_token_auth: true },
	});
});

test("dismissing the Disable token authentication modal changes nothing (#877)", async ({
	page,
}) => {
	const { saved } = await mockServerSettings(page, { external_access: true });
	await page.goto("/system/settings");

	const checkbox = disableTokenAuthCheckbox(page);
	await checkbox.click();
	const modal = page.getByTestId("disable-token-auth-modal");
	await expect(modal).toBeVisible();
	await expect(page.getByTestId("disable-token-auth-extra-risk")).toContainText(
		"exposed externally",
	);
	await page.keyboard.press("Escape");
	await expect(modal).toBeHidden();
	await expect(checkbox).not.toBeChecked();

	// Change something else so the save goes through, and confirm the
	// dismissed modal left the setting out.
	await page.getByRole("checkbox", { name: /Disable TLS/i }).check();
	await page.getByRole("button", { name: "Save Changes" }).click();
	await expect.poll(saved).not.toBeNull();
	const server = saved()?.server as Record<string, unknown> | undefined;
	expect(server?.no_tls).toBe(true);
	expect(server?.disable_token_auth).toBeUndefined();
});

test("unchecking Disable token authentication needs no confirmation (#877)", async ({
	page,
}) => {
	const { saved } = await mockServerSettings(page, {
		disable_token_auth: true,
	});
	await page.goto("/system/settings");

	const checkbox = disableTokenAuthCheckbox(page);
	await expect(checkbox).toBeChecked();
	await checkbox.click();
	await expect(page.getByTestId("disable-token-auth-modal")).toHaveCount(0);
	await expect(checkbox).not.toBeChecked();

	await page.getByRole("button", { name: "Save Changes" }).click();
	await expect.poll(saved).not.toBeNull();
	expect(
		(saved()?.server as Record<string, unknown> | undefined)
			?.disable_token_auth,
	).toBeUndefined();
});

test("system tools shows voice transcription deps and installs them", async ({
	page,
}) => {
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
	await page.route("**/api/transcription", (route) => {
		methods.push(route.request().method());
		return route.fulfill({
			json: route.request().method() === "POST" ? installed : missing,
		});
	});
	await page.goto("/system/tools");

	const card = page.getByTestId("transcription-status");
	await expect(card.getByTestId("transcription-engine")).toContainText(
		"Missing",
	);
	await expect(card.getByTestId("transcription-model")).toContainText(
		"Model base.en",
	);
	await expect(card.getByTestId("transcription-model")).toContainText(
		"Missing",
	);
	await expect(card.getByTestId("transcription-ffmpeg")).toContainText(
		"/opt/homebrew/bin/ffmpeg",
	);

	await card.getByTestId("transcription-install").click();
	await expect(card.getByTestId("transcription-engine")).toContainText(
		"Installed",
	);
	await expect(card.getByTestId("transcription-model")).toContainText(
		"Downloaded",
	);
	await expect(card.getByTestId("transcription-install")).toHaveCount(0);
	expect(methods).toEqual(["GET", "POST"]);
});

test("every System page header links to Settings with a gear (#932)", async ({
	page,
}) => {
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
		await page.goto(path);
		const gear = page.getByTestId("system-settings-link");
		await expect(gear, path).toBeVisible();
		await expect(gear, path).toHaveAccessibleName("Settings");
		await expect(gear, path).toHaveAttribute("title", "Settings");
		await expect(gear, path).toHaveAttribute("href", "/system/settings");
	}
});

test("the System gear opens Settings, which shows no gear (#932)", async ({
	page,
}) => {
	await page.goto("/system/overview");
	await page.getByRole("link", { name: "Settings", exact: true }).click();
	await expect(page).toHaveURL(/\/system\/settings$/);
	await expect(page.getByTestId("page-title")).toHaveText("Settings");
	await expect(page.getByRole("dialog")).toHaveCount(0);
	await expect(page.getByTestId("system-settings-link")).toHaveCount(0);

	await page.goto("/system/providers");
	await expect(page.getByTestId("page-title")).toBeVisible();
	await expect(page.getByTestId("system-settings-link")).toHaveCount(0);
});

test("the System gear fits beside header actions on a phone (#932)", async ({
	page,
}) => {
	await page.setViewportSize({ width: 390, height: 844 });
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
		await page.goto(path);
		const gear = page.getByTestId("system-settings-link");
		await expect(gear, path).toBeVisible();
		const box = await gear.boundingBox();
		if (!box) throw new Error(`${path}: no gear box`);
		expect(box.x + box.width, path).toBeLessThanOrEqual(390);
		const refresh = page.getByRole("button", { name: /^(Refresh|Loading)/ });
		if (await refresh.count()) {
			const r = await refresh.first().boundingBox();
			if (!r) throw new Error(`${path}: no Refresh box`);
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
