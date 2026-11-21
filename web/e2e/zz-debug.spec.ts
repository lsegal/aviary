import { test } from "@playwright/test";
import { mockMCP, setAuthToken } from "./helpers/mockMCP";
test("dbg", async ({ page }) => {
	await setAuthToken(page);
	await mockMCP(page, { agent_list: [], job_list: [], config_validate: [], session_list: [], agent_file_list: [], config_get: { agents: [{ name: "assistant", model: "x" }] } });
	page.on("console", (m) => console.log("CONSOLE", m.text()));
	await page.goto("/system/overview");
	await page.getByTestId("sidebar-collapse").click();
	const f = page.getByTestId("sidebar-rail-files");
	console.log(await f.evaluate((e) => e.outerHTML));
	await f.hover(); await page.waitForTimeout(300); await f.click();
	await page.waitForTimeout(1000);
	console.log(page.url());
});
