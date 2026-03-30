import { chromium } from "@playwright/test";
import { mkdir } from "node:fs/promises";
import path from "node:path";
import process from "node:process";

const repoRoot = process.cwd();
const outputDir = path.join(
	repoRoot,
	"docs",
	"site",
	"public",
	"screenshots",
);
const baseURL = "http://127.0.0.1:5173";

function iso(daysAgo, hour, minute = 0) {
	const date = new Date();
	date.setDate(date.getDate() - daysAgo);
	date.setHours(hour, minute, 0, 0);
	return date.toISOString();
}

function usageRecord({
	daysAgo,
	hour,
	session,
	agent,
	model,
	provider,
	input,
	output,
	cacheRead = 0,
	cacheWrite = 0,
	toolCalls = 0,
	hasError = false,
	hasThrottle = false,
}) {
	return {
		timestamp: iso(daysAgo, hour),
		session_id: session,
		agent_id: agent,
		model,
		provider,
		input_tokens: input,
		output_tokens: output,
		cache_read_tokens: cacheRead,
		cache_write_tokens: cacheWrite,
		tool_calls: toolCalls,
		has_error: hasError,
		has_throttle: hasThrottle,
	};
}

const versionPayload = {
	latestVersion: "0.3.1",
	upgradeAvailable: false,
	message: "",
};

const overviewFixtures = {
	agent_list: [
		{
			id: "a1",
			name: "assistant",
			model: "anthropic/claude-sonnet-4-5",
			fallbacks: ["openai/gpt-5-mini"],
			state: "idle",
		},
		{
			id: "a2",
			name: "researcher",
			model: "openai/gpt-5",
			fallbacks: [],
			state: "running",
		},
		{
			id: "a3",
			name: "ops",
			model: "google/gemini-2.5-pro",
			fallbacks: [],
			state: "idle",
		},
	],
	job_list: [
		{
			id: "job-1",
			task_id: "daily-inbox-triage",
			agent_id: "assistant",
			status: "completed",
			attempts: 1,
			max_retries: 3,
			created_at: iso(0, 7, 30),
			updated_at: iso(0, 7, 31),
		},
		{
			id: "job-2",
			task_id: "calendar-prep",
			agent_id: "assistant",
			status: "in_progress",
			attempts: 1,
			max_retries: 3,
			created_at: iso(0, 8, 15),
			updated_at: iso(0, 8, 16),
		},
		{
			id: "job-3",
			task_id: "competitor-watch",
			agent_id: "researcher",
			status: "failed",
			attempts: 2,
			max_retries: 3,
			created_at: iso(0, 6, 0),
			updated_at: iso(0, 6, 2),
		},
	],
	config_validate: [],
};

const chatFixtures = {
	agent_list: [
		{
			id: "a1",
			name: "assistant",
			model: "anthropic/claude-sonnet-4-5",
			fallbacks: ["openai/gpt-5-mini"],
			state: "idle",
		},
		{
			id: "a2",
			name: "researcher",
			model: "openai/gpt-5",
			fallbacks: [],
			state: "idle",
		},
	],
	session_list: [
		{
			id: "sess-main",
			agent_id: "agent_assistant",
			name: "main",
			created_at: iso(1, 10),
			is_processing: false,
		},
		{
			id: "sess-triage",
			agent_id: "agent_assistant",
			name: "morning planning",
			created_at: iso(0, 8),
			is_processing: false,
		},
	],
	session_messages: [
		{
			id: "m1",
			role: "user",
			content:
				"Plan my morning: search for today’s OpenAI docs updates, check my calendar, and summarize unread release email.",
			timestamp: iso(0, 8, 10),
		},
		{
			id: "m2",
			role: "tool",
			content: JSON.stringify({
				name: "web_search",
				args: { query: "OpenAI docs updates March 2026", count: 3 },
				result:
					"1. GPT-5.4 prompt upgrade guide\n2. Responses API updates\n3. Docs changelog entry for tool calling",
			}),
			timestamp: iso(0, 8, 10),
		},
		{
			id: "m3",
			role: "tool",
			content: JSON.stringify({
				name: "calendar_events_today",
				args: { date: "2026-03-29" },
				result:
					"09:00 Docs review\n11:30 Customer sync\n14:00 Launch checklist\n16:30 Inbox zero block",
			}),
			timestamp: iso(0, 8, 11),
		},
		{
			id: "m4",
			role: "tool",
			content: JSON.stringify({
				name: "email_inbox_list",
				args: { mailbox: "inbox", unread_only: true, max_results: 5 },
				result:
					"3 unread: landing page screenshots, release notes draft, calendar hold from product",
			}),
			timestamp: iso(0, 8, 11),
		},
		{
			id: "m5",
			role: "assistant",
			content:
				"I took a quick pass through everything.\n\nThe docs updates look pretty minor, mostly prompt and tool-calling changes, so nothing feels urgent there. Your first meeting is the docs review at 9, which gives you a clean block right now if you want to knock out the screenshot work first. In email, the thread that matters most is the one about landing page screenshots, then the release-notes draft after that.\n\nIf you want, I can turn this into a short morning brief and sketch the screenshot to-do list next.",
			timestamp: iso(0, 8, 12),
			model: "anthropic/claude-sonnet-4-5",
		},
	],
};

const usageFixtures = {
	usage_query: [
		usageRecord({
			daysAgo: 0,
			hour: 8,
			session: "sess-main",
			agent: "assistant",
			model: "anthropic/claude-sonnet-4-5",
			provider: "anthropic",
			input: 1800,
			output: 2200,
			cacheRead: 300,
			toolCalls: 3,
		}),
		usageRecord({
			daysAgo: 0,
			hour: 9,
			session: "sess-triage",
			agent: "assistant",
			model: "anthropic/claude-sonnet-4-5",
			provider: "anthropic",
			input: 1400,
			output: 1700,
			cacheRead: 250,
			toolCalls: 2,
		}),
		usageRecord({
			daysAgo: 0,
			hour: 10,
			session: "sess-research",
			agent: "researcher",
			model: "openai/gpt-5",
			provider: "openai",
			input: 2500,
			output: 2100,
			cacheRead: 600,
			cacheWrite: 120,
			toolCalls: 5,
		}),
		usageRecord({
			daysAgo: 1,
			hour: 11,
			session: "sess-ops",
			agent: "ops",
			model: "google/gemini-2.5-pro",
			provider: "google",
			input: 900,
			output: 600,
			toolCalls: 1,
		}),
		usageRecord({
			daysAgo: 1,
			hour: 13,
			session: "sess-ops",
			agent: "ops",
			model: "google/gemini-2.5-pro",
			provider: "google",
			input: 1100,
			output: 850,
			cacheRead: 180,
			toolCalls: 2,
			hasThrottle: true,
		}),
		usageRecord({
			daysAgo: 2,
			hour: 7,
			session: "sess-main",
			agent: "assistant",
			model: "anthropic/claude-sonnet-4-5",
			provider: "anthropic",
			input: 1500,
			output: 1300,
			cacheRead: 220,
			toolCalls: 2,
		}),
		usageRecord({
			daysAgo: 2,
			hour: 15,
			session: "sess-research-2",
			agent: "researcher",
			model: "openai/gpt-5",
			provider: "openai",
			input: 3200,
			output: 2800,
			cacheRead: 700,
			cacheWrite: 150,
			toolCalls: 6,
			hasError: true,
		}),
		usageRecord({
			daysAgo: 3,
			hour: 9,
			session: "sess-main",
			agent: "assistant",
			model: "anthropic/claude-sonnet-4-5",
			provider: "anthropic",
			input: 1000,
			output: 1200,
			cacheRead: 200,
			toolCalls: 2,
		}),
		usageRecord({
			daysAgo: 4,
			hour: 14,
			session: "sess-support",
			agent: "assistant",
			model: "anthropic/claude-sonnet-4-5",
			provider: "anthropic",
			input: 1600,
			output: 1500,
			cacheRead: 240,
			toolCalls: 4,
		}),
		usageRecord({
			daysAgo: 5,
			hour: 16,
			session: "sess-ops-2",
			agent: "ops",
			model: "google/gemini-2.5-pro",
			provider: "google",
			input: 800,
			output: 950,
			cacheRead: 100,
			toolCalls: 1,
		}),
		usageRecord({
			daysAgo: 6,
			hour: 12,
			session: "sess-research-3",
			agent: "researcher",
			model: "openai/gpt-5",
			provider: "openai",
			input: 2900,
			output: 2500,
			cacheRead: 640,
			cacheWrite: 140,
			toolCalls: 5,
		}),
	],
};

const settingsConfig = {
	server: {
		port: 16677,
		tls: { cert: "", key: "" },
		external_access: false,
		no_tls: false,
	},
	agents: [
		{
			name: "assistant",
			model: "anthropic/claude-sonnet-4-5",
			fallbacks: ["openai/gpt-5-mini"],
			working_dir: "/workspace/aviary",
			verbose: true,
			permissions: {
				preset: "minimal",
				tools: [
					"web_search",
					"calendar_events_today",
					"email_inbox_list",
					"session_list",
					"task_run",
				],
				disabledTools: ["browser_open"],
				filesystem: {
					allowedPaths: ["./docs/**", "!./docs/private/**", "./README.md"],
				},
				exec: {
					allowedCommands: ["git status", "pnpm lint", "!rm *", "!git push *"],
					shellInterpolate: false,
					shell: "/bin/bash -lc",
				},
			},
			channels: [
				{
					enabled: true,
					type: "slack",
					id: "design-ops-bot",
					primary: "U123PRIMARY",
					url: "xapp-1-live-example",
					token: "xoxb-1-live-example",
					model: "openai/gpt-5-mini",
					fallbacks: ["anthropic/claude-sonnet-4-5"],
					disabledTools: ["browser_open"],
					showTyping: true,
					replyToReplies: true,
					reactToEmoji: true,
					sendReadReceipts: false,
					group_chat_history: 30,
					allowFrom: [
						{
							enabled: true,
							from: "U123PRIMARY, U456PM",
							allowedGroups: "CENG, CDOCS",
							mentionPrefixes: ["aviary", "assistant"],
							excludePrefixes: ["!"],
							respondToMentions: true,
							mentionPrefixGroupOnly: true,
							model: "anthropic/claude-sonnet-4-5",
							fallbacks: ["openai/gpt-5-mini"],
							restrictTools: [
								"web_search",
								"calendar_events_today",
								"email_inbox_list",
							],
						},
						{
							enabled: true,
							from: "*",
							allowedGroups: "CDOCS",
							mentionPrefixes: ["aviary"],
							excludePrefixes: ["/"],
							respondToMentions: false,
							mentionPrefixGroupOnly: false,
							restrictTools: ["web_search"],
						},
					],
				},
			],
			tasks: [],
		},
	],
	models: {
		providers: {},
		defaults: {
			model: "anthropic/claude-sonnet-4-5",
			fallbacks: ["openai/gpt-5-mini"],
		},
	},
	browser: { binary: "", cdp_port: 9222, headless: true },
	search: { web: { brave_api_key: "" } },
	scheduler: { concurrency: "", precompute_tasks: true },
	skills: {
		calendar: { enabled: true },
		email: { enabled: true },
	},
};

const settingsToolList = [
	{
		name: "web_search",
		description: "Search the web and return the top matching pages.",
		inputSchema: {
			type: "object",
			required: ["query"],
			properties: {
				query: { type: "string" },
				count: { type: "integer", default: 3 },
			},
		},
	},
	{
		name: "calendar_events_today",
		description: "Return the user’s events for the current day.",
		inputSchema: {
			type: "object",
			properties: {
				date: { type: "string" },
			},
		},
	},
	{
		name: "email_inbox_list",
		description: "List the newest inbox messages for a mailbox.",
		inputSchema: {
			type: "object",
			properties: {
				mailbox: { type: "string" },
				unread_only: { type: "boolean" },
			},
		},
	},
	{
		name: "browser_open",
		description: "Open a page in the shared browser session.",
		inputSchema: {
			type: "object",
			required: ["url"],
			properties: {
				url: { type: "string" },
			},
		},
	},
	{
		name: "session_list",
		description: "List sessions for an agent.",
		inputSchema: {
			type: "object",
			properties: {
				agent: { type: "string" },
			},
		},
	},
	{
		name: "task_run",
		description: "Run a scheduled task immediately.",
		inputSchema: {
			type: "object",
			properties: {
				name: { type: "string" },
			},
		},
	},
	{
		name: "config_get",
		description: "Read the active Aviary configuration.",
		inputSchema: { type: "object", properties: {} },
	},
	{
		name: "config_save",
		description: "Persist the active Aviary configuration.",
		inputSchema: {
			type: "object",
			properties: {
				config: { type: "string" },
			},
		},
	},
];

const settingsFixtures = {
	config_get: settingsConfig,
	auth_list: ["anthropic:default", "openai:default", "google:default"],
	tool_list: settingsToolList,
};

const systemSkillsList = [
	{
		name: "calendar",
		description: "Read upcoming events and create short day plans.",
		path: "skills/calendar/SKILL.md",
		source: "builtin",
		enabled: true,
	},
	{
		name: "email",
		description: "Triage inboxes and summarize unread threads into action items.",
		path: "skills/email/SKILL.md",
		source: "builtin",
		enabled: true,
	},
	{
		name: "notion",
		description: "Look up notes, docs, and project pages from your workspace.",
		path: "skills/notion/SKILL.md",
		source: "builtin",
		enabled: false,
	},
	{
		name: "gogcli",
		description: "Search GOG libraries and metadata for game-related workflows.",
		path: "skills/gogcli/SKILL.md",
		source: "disk",
		enabled: false,
	},
];

const systemToolsFixtures = {
	tool_list: settingsToolList,
	skills_list: systemSkillsList,
	calendar_events_today:
		"09:00 Docs review\n11:30 Customer sync\n14:00 Launch checklist\n16:30 Inbox zero block",
	web_search:
		"1. aviary.bot landing page draft\n2. docs screenshot checklist\n3. customer feedback thread about docs images",
};

const systemSkillsFixtures = {
	skills_list: systemSkillsList,
	config_get: {
		server: {
			port: 16677,
			tls: { cert: "", key: "" },
			external_access: false,
			no_tls: false,
		},
		agents: [],
		models: { providers: {}, defaults: { model: "", fallbacks: [] } },
		browser: { binary: "", cdp_port: 9222, headless: true },
		search: { web: { brave_api_key: "" } },
		scheduler: { concurrency: "", precompute_tasks: true },
		skills: {
			calendar: { enabled: true },
			email: { enabled: true },
			notion: { enabled: false },
			gogcli: { enabled: false },
		},
	},
};

const compileDetail = {
	id: "compile-1",
	agent_id: "assistant",
	task_name: "daily-inbox-triage",
	requested_task_type: "prompt",
	result_task_type: "script",
	trigger: "cron",
	target: "email+calendar",
	prompt:
		"Check unread email, compare against today’s meetings, and draft a morning brief.",
	validated: true,
	deterministic_steps: 4,
	status: "succeeded",
	reason: "Workflow is deterministic and safe to compile into a script.",
	created_at: iso(0, 7, 0),
	updated_at: iso(0, 7, 1),
	stages: [
		{
			name: "Discovery",
			status: "succeeded",
			system_prompt: "Analyze the task for deterministic steps.",
			user_prompt: "Check unread email and calendar to prepare a morning brief.",
			response:
				"Calendar lookup, inbox listing, and templated summary are deterministic.",
			started_at: iso(0, 7, 0),
		},
		{
			name: "Compilation",
			status: "succeeded",
			system_prompt: "Generate a runnable script.",
			user_prompt: "Produce the final script.",
			response: "Compiled script produced successfully.",
			started_at: iso(0, 7, 1),
		},
	],
	script:
		"events = calendar_events_today()\nmail = email_inbox_list(unread_only=true)\nprint(render_morning_brief(events, mail))",
};

const jobsFixtures = {
	job_query: [
		{
			id: "job-run-1",
			task_id: "daily-inbox-triage",
			agent_id: "assistant",
			session_id: "sess-triage",
			prompt:
				"Check unread email, compare against today’s meetings, and draft a morning brief.",
			status: "completed",
			attempts: 1,
			max_retries: 3,
			scheduled_for: iso(0, 7, 0),
			created_at: iso(0, 7, 0),
			updated_at: iso(0, 7, 1),
		},
		{
			id: "job-run-2",
			task_id: "calendar-prep",
			agent_id: "assistant",
			session_id: "sess-main",
			prompt: "Summarize today’s meetings and prep notes.",
			status: "in_progress",
			attempts: 1,
			max_retries: 3,
			scheduled_for: iso(0, 8, 30),
			created_at: iso(0, 8, 30),
			updated_at: iso(0, 8, 31),
		},
		{
			id: "job-run-3",
			task_id: "competitor-watch",
			agent_id: "researcher",
			session_id: "sess-research",
			prompt: "Track competitor launches and pricing changes.",
			status: "failed",
			attempts: 2,
			max_retries: 3,
			scheduled_for: iso(1, 15, 0),
			created_at: iso(1, 15, 0),
			updated_at: iso(1, 15, 2),
		},
	],
	task_list: [
		{
			id: "daily-inbox-triage",
			agent_id: "assistant",
			agent_name: "assistant",
			name: "daily-inbox-triage",
			type: "script",
			trigger_type: "cron",
			schedule: "0 7 * * 1-5",
			prompt:
				"Check unread email, compare against today’s meetings, and draft a morning brief.",
		},
		{
			id: "competitor-watch",
			agent_id: "researcher",
			agent_name: "researcher",
			name: "competitor-watch",
			type: "prompt",
			trigger_type: "cron",
			schedule: "0 */4 * * *",
			prompt: "Track competitor launches and pricing changes.",
		},
	],
	task_compile_query: [compileDetail],
	task_compile_get: compileDetail,
};

async function waitForServer(url, timeoutMs = 30_000) {
	const started = Date.now();
	for (;;) {
		try {
			const response = await fetch(url);
			if (response.ok) return;
		} catch {
			// Keep waiting.
		}
		if (Date.now() - started > timeoutMs) {
			throw new Error(`Timed out waiting for ${url}`);
		}
		await new Promise((resolve) => setTimeout(resolve, 500));
	}
}

async function configurePage(page, fixtures) {
	await page.addInitScript(() => {
		localStorage.setItem("aviary_token", "docs-shot-token");

		class MockWebSocket {
			static CONNECTING = 0;
			static OPEN = 1;
			static CLOSING = 2;
			static CLOSED = 3;

			constructor(url) {
				this.url = url;
				this.readyState = MockWebSocket.CONNECTING;
				this.onopen = null;
				this.onmessage = null;
				this.onerror = null;
				this.onclose = null;
				setTimeout(() => {
					this.readyState = MockWebSocket.OPEN;
					this.onopen?.({ type: "open", target: this });
					this.onmessage?.({
						type: "message",
						data: JSON.stringify({
							ok: true,
							version: "0.3.0",
							goos: "linux",
						}),
						target: this,
					});
				}, 20);
			}

			addEventListener() {}
			removeEventListener() {}
			send() {}
			close() {
				this.readyState = MockWebSocket.CLOSED;
				this.onclose?.({ type: "close", target: this });
			}
		}

		window.WebSocket = MockWebSocket;
	});

	await page.route("**/api/version", async (route) => {
		await route.fulfill({
			status: 200,
			contentType: "application/json",
			body: JSON.stringify(versionPayload),
		});
	});

	await page.route("**/api/login", async (route) => {
		await route.fulfill({
			status: 200,
			contentType: "application/json",
			body: "{}",
		});
	});

	await page.route("**/mcp", async (route) => {
		const body = route.request().postDataJSON();

		if (body.method === "initialize") {
			await route.fulfill({
				status: 200,
				headers: {
					"Content-Type": "application/json",
					"Mcp-Session-Id": "docs-shot-session",
				},
				body: JSON.stringify({
					jsonrpc: "2.0",
					id: body.id,
					result: {
						protocolVersion: "2024-11-05",
						capabilities: {},
						serverInfo: { name: "aviary-docs-shot", version: "0.3.0" },
					},
				}),
			});
			return;
		}

		if (body.method === "notifications/initialized") {
			await route.fulfill({ status: 200, body: "{}" });
			return;
		}

		if (body.method === "tools/list") {
			await route.fulfill({
				status: 200,
				contentType: "application/json",
				body: JSON.stringify({
					jsonrpc: "2.0",
					id: body.id,
					result: { tools: fixtures.tool_list ?? [] },
				}),
			});
			return;
		}

		if (body.method === "tools/call") {
			const name = body.params?.name ?? "";
			const result = name in fixtures ? fixtures[name] : [];
			await route.fulfill({
				status: 200,
				contentType: "application/json",
				body: JSON.stringify({
					jsonrpc: "2.0",
					id: body.id,
					result: {
						content: [
							{
								type: "text",
								text:
									typeof result === "string" ? result : JSON.stringify(result),
							},
						],
					},
				}),
			});
			return;
		}

		await route.fulfill({ status: 200, body: "{}" });
	});
}

async function openPage(page, pathname, readySelector) {
	await page.goto(`${baseURL}${pathname}`);
	await page.waitForLoadState("networkidle");
	await page.waitForFunction(() => document.fonts?.status === "loaded");
	await page.locator(readySelector).waitFor();
}

async function captureSimple({
	browser,
	fixtures,
	pathname,
	readySelector,
	outputName,
	viewport,
}) {
	const page = await browser.newPage({ viewport, colorScheme: "light" });
	try {
		await configurePage(page, fixtures);
		await openPage(page, pathname, readySelector);
		await page.screenshot({
			path: path.join(outputDir, outputName),
			fullPage: false,
		});
	} finally {
		await page.close();
	}
}

async function captureTools(browser) {
	const page = await browser.newPage({
		viewport: { width: 1040, height: 1120 },
		colorScheme: "light",
	});
	try {
		await configurePage(page, systemToolsFixtures);
		await openPage(page, "/system/tools", 'h2:has-text("System Tools")');
		await page.getByTestId("run-tool-calendar_events_today").click();
		await page.getByRole("button", { name: "Run Tool" }).click();
		await page.getByTestId("tool-run-output").getByText("Docs review").waitFor();
		await page.screenshot({
			path: path.join(outputDir, "system-tools.png"),
			fullPage: false,
		});
	} finally {
		await page.close();
	}
}

async function captureJobs(browser) {
	const page = await browser.newPage({
		viewport: { width: 1040, height: 1120 },
		colorScheme: "light",
	});
	try {
		await configurePage(page, jobsFixtures);
		await openPage(page, "/jobs", 'h2:has-text("Jobs")');
		await page
			.locator("tr")
			.filter({ hasText: "Workflow is deterministic and safe to compile" })
			.first()
			.click();
		await page.getByText("Generated Script").waitFor();
		await page.screenshot({
			path: path.join(outputDir, "system-jobs.png"),
			fullPage: false,
		});
	} finally {
		await page.close();
	}
}

async function captureSettingsPage({
	browser,
	pathname,
	outputName,
	readySelector,
}) {
	const page = await browser.newPage({
		viewport: { width: 1040, height: 1120 },
		colorScheme: "light",
	});
	try {
		await configurePage(page, settingsFixtures);
		await openPage(page, pathname, readySelector);
		await page.screenshot({
			path: path.join(outputDir, outputName),
			fullPage: false,
		});
	} finally {
		await page.close();
	}
}

async function main() {
	await mkdir(outputDir, { recursive: true });
	await waitForServer(baseURL);

	const browser = await chromium.launch();
	try {
		await captureSimple({
			browser,
			fixtures: overviewFixtures,
			pathname: "/overview",
			readySelector: 'h2:has-text("Overview")',
			outputName: "control-panel-overview.png",
			viewport: { width: 1040, height: 980 },
		});

		await captureSimple({
			browser,
			fixtures: chatFixtures,
			pathname: "/chat",
			readySelector: "text=I took a quick pass through everything.",
			outputName: "chat-workspace.png",
			viewport: { width: 1040, height: 980 },
		});

		await captureSimple({
			browser,
			fixtures: usageFixtures,
			pathname: "/usage",
			readySelector: 'h2:has-text("Usage")',
			outputName: "usage-analytics.png",
			viewport: { width: 1040, height: 1120 },
		});

		await captureSettingsPage({
			browser,
			pathname: "/settings/agents/assistant/channels",
			outputName: "configure-everything.png",
			readySelector: 'h5:has-text("Channel 1")',
		});

		await captureSettingsPage({
			browser,
			pathname: "/settings/agents/assistant/permissions",
			outputName: "security-minded.png",
			readySelector: 'label:has-text("Filesystem Allowed Paths")',
		});

		await captureSimple({
			browser,
			fixtures: systemSkillsFixtures,
			pathname: "/system/skills",
			readySelector: 'h2:has-text("Skill Marketplace")',
			outputName: "system-skills.png",
			viewport: { width: 1040, height: 1120 },
		});

		await captureSimple({
			browser,
			fixtures: {},
			pathname: "/system/models",
			readySelector: 'h2:has-text("Supported Models")',
			outputName: "system-models.png",
			viewport: { width: 1040, height: 1120 },
		});

		await captureTools(browser);
		await captureJobs(browser);
	} finally {
		await browser.close();
	}
}

main().catch((error) => {
	console.error(error);
	process.exitCode = 1;
});
