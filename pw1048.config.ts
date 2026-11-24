import base from "./playwright.config";
export default {
	...base,
	testDir: "/private/var/folders/_3/6nm7yxhj0v3716vn7dyl134h0000gn/T/aviary-gh-fix-1036/web/e2e",
	use: { ...base.use, baseURL: "https://localhost:5297" },
	webServer: undefined,
};
