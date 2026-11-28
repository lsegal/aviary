// @vitest-environment happy-dom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
	FakeWebSocket,
	fakes,
	makeConfig,
	mountSettings,
	resetFakes,
} from "./settingsViewHarness";

vi.mock("../composables/useMCP", () => ({
	useMCP: () => ({ callTool: fakes.callTool, listTools: fakes.listTools }),
}));
vi.mock("../composables/useAvailableModels", async () => {
	const { ref } = await import("vue");
	return {
		useAvailableModels: () => ({
			availableModelOptions: ref([]),
			credentials: fakes.credentials,
			refreshCredentials: fakes.refreshCredentials,
		}),
	};
});
vi.mock("../composables/useProviderAuth", async (importOriginal) => ({
	...(await importOriginal<object>()),
	useProviderAuth: () => fakes.providerAuth,
}));
vi.mock("../composables/useTriggerPreview", () => ({
	useTriggerPreview: () => fakes.previewTrigger,
}));
vi.mock("../composables/useAgentNav", () => ({
	useAgentNav: () => ({ agentHome: (id: string) => `/agents/${id}/chat` }),
}));
vi.mock("../composables/useAgentItemDialogs", () => ({
	useAgentItemDialogs: () => ({ open: fakes.openItemDialog }),
}));
vi.mock("../stores/settings", async (importOriginal) => ({
	...(await importOriginal<object>()),
	useSettingsStore: () => fakes.settings,
}));
vi.mock("../stores/agentFiles", async (importOriginal) => ({
	...(await importOriginal<object>()),
	useAgentFilesStore: () => fakes.agentFiles,
}));
vi.mock("../stores/auth", () => ({ useAuthStore: () => fakes.auth }));
vi.mock("../stores/chatSessions", async (importOriginal) => ({
	...(await importOriginal<object>()),
	useChatSessionsStore: () => fakes.chatSessions,
}));

beforeEach(() => {
	resetFakes();
	vi.stubGlobal("WebSocket", FakeWebSocket);
});

afterEach(() => {
	document.body.innerHTML = "";
	vi.unstubAllGlobals();
	vi.useRealTimers();
});

describe("SettingsView", () => {
	it("loads the config and shows the General tab", async () => {
		const { wrapper, ctx } = await mountSettings("/system/settings");
		expect(fakes.settings.fetchConfig).toHaveBeenCalled();
		expect(ctx.activeTab).toBe("general");
		expect(wrapper.find('[data-testid="settings-tabs"]').exists()).toBe(true);
		wrapper.unmount();
	});
});
