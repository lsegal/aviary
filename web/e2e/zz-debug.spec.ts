import { test } from "@playwright/test";
import { mockMCP, setAuthToken } from "./helpers/mockMCP";
test("dbg", async ({ page }) => {
	await setAuthToken(page);
	await mockMCP(page, { agent_list: [], config_get: { agents: [{ name: "Aviary", model: "x/y" }] }, config_validate: [], job_list: [], session_list: [] });
	await page.setViewportSize({ width: 375, height: 700 });
	await page.goto("/agents/Aviary/projects/lsegal%2Fa-repository-with-a-very-long-name/jobs/1013");
	await page.getByTestId("page-breadcrumb").waitFor();
	console.log(JSON.stringify(await page.evaluate(() => {
		const ol = document.querySelector('[data-testid="page-breadcrumb"] ol');
		return { ol: ol.getBoundingClientRect().width, olScroll: ol.scrollWidth, lis: [...ol.children].map((li) => { const cs = getComputedStyle(li); return [li.getBoundingClientRect().width, cs.flexShrink, cs.minWidth, cs.maxWidth, li.scrollWidth]; }) };
	})));
});
