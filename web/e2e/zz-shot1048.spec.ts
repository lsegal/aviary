import { test } from "@playwright/test";
import { analysisCache } from "./helpers/analysisCache";
import { mockMCP, setAuthToken } from "./helpers/mockMCP";

test("shot", async ({ page }) => {
	const PARTIAL = {
		verdict: "partial", will_precompile: false, reason: "summarizing needs judgment",
		model_steps: ["summarizing the results"],
		estimate: { available: true, min_tokens: 4600, max_tokens: 12400, min_cost_usd: 0.014, max_cost_usd: 0.05 },
	};
	const cache = analysisCache(() => PARTIAL);
	cache.seed({ agent: "assistant", content: "fetch the release notes and summarize them", type: "prompt" }, new Date(Date.now() - 2 * 3600_000).toISOString());
	const config = { agents: [{ name: "assistant", model: "x/y", tasks: [{ name: "release-notes", prompt: "fetch the release notes and summarize them", every: "1h" }] }] };
	await page.setViewportSize({ width: 1280, height: 1100 });
	await setAuthToken(page);
	await mockMCP(page, {
		agent_list: [{ id: "assistant", name: "assistant", model: "x/y", state: "idle" }],
		config_get: () => config, config_save: () => "ok", config_validate: [],
		task_preview: { description: "every hour", next: [], runs_per_30_days: 720 },
		task_analyze: (a?: Record<string, unknown>) => cache.handler(a ?? {}),
		job_list: [], session_list: [], auth_list: [], tool_list: [], agent_file_list: [],
	});
	await page.goto("/agents/assistant/tasks?task=release-notes");
	const fb = page.getByTestId("task-cost-feedback");
	await fb.getByTestId("task-cost-cached").waitFor();
	await fb.scrollIntoViewIfNeeded();
	await page.waitForTimeout(500);
	const prompt = page.getByLabel("Prompt");
	const box = (await prompt.boundingBox())!;
	const fbox = (await fb.boundingBox())!;
	const y = fbox.y - 140;
	await page.screenshot({ path: "/tmp/shot1048-cached.png", clip: { x: box.x - 16, y, width: box.width + 32, height: fbox.y + fbox.height - y + 16 } });
});
