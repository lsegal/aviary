import { computed, ref } from "vue";
import { providerOf, SUPPORTED_MODELS } from "../constants/models";
import { useMCP } from "./useMCP";

function canonicalProvider(provider: string): string {
	switch (provider) {
		case "gemini":
			return "google";
		default:
			return provider;
	}
}

export function useAvailableModels() {
	const credentials = ref<string[]>([]);
	const { callTool } = useMCP();

	const authenticatedProviders = computed(() => {
		const providers = new Set<string>();
		for (const cred of credentials.value) {
			const provider = canonicalProvider(cred.split(":", 1)[0]?.trim() ?? "");
			if (provider) {
				providers.add(provider);
			}
		}
		return providers;
	});

	const availableModelOptions = computed(() =>
		SUPPORTED_MODELS.filter((model) => {
			const provider = canonicalProvider(providerOf(model));
			return !provider || authenticatedProviders.value.has(provider);
		}),
	);

	async function refreshCredentials() {
		try {
			const raw = await callTool("auth_list");
			credentials.value = (JSON.parse(raw) as string[] | null) ?? [];
		} catch {
			credentials.value = [];
		}
	}

	return {
		availableModelOptions,
		credentials,
		refreshCredentials,
	};
}
