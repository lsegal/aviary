import { computed, ref } from "vue";
import { providerOf, SUPPORTED_MODELS } from "../constants/models";
import { useMCP } from "./useMCP";

type ProviderConfig = {
	auth?: string;
	base_uri?: string;
	region?: string;
	profile?: string;
};

type AppConfigLike = {
	agents?: Array<{
		model?: string;
		fallbacks?: string[];
	}>;
	models?: {
		providers?: Record<string, ProviderConfig>;
		defaults?: {
			model?: string;
			fallbacks?: string[];
		};
	};
};

function credentialToProvider(credKey: string): string {
	switch (credKey) {
		case "gemini:oauth":
			return "google-gemini";
		case "gemini:default":
			return "google";
		case "openai:oauth":
			return "openai-codex";
		default:
			return credKey.split(":", 1)[0]?.trim() ?? "";
	}
}

export function useAvailableModels() {
	const credentials = ref<string[]>([]);
	const endpointModelOptions = ref<string[]>([]);
	const configuredModelOptions = ref<string[]>([]);
	const { callTool } = useMCP();

	function parseStringArrayPayload(raw: string): string[] | null {
		const trimmed = raw.trim();
		if (!trimmed) {
			return [];
		}
		try {
			const parsed = JSON.parse(trimmed) as string[] | null;
			return parsed ?? [];
		} catch (error) {
			if (error instanceof SyntaxError) {
				return null;
			}
			throw error;
		}
	}

	async function delay(ms: number): Promise<void> {
		await new Promise((resolve) => setTimeout(resolve, ms));
	}

	const regionProviders = ref<Set<string>>(new Set());

	const authenticatedProviders = computed(() => {
		const providers = new Set<string>();
		for (const cred of credentials.value) {
			const provider = credentialToProvider(cred);
			if (provider) {
				providers.add(provider);
			}
		}
		for (const rp of regionProviders.value) {
			providers.add(rp);
		}
		return providers;
	});

	const availableModelOptions = computed(() =>
		[
			...SUPPORTED_MODELS.filter((model) => {
				const provider = providerOf(model);
				return !provider || authenticatedProviders.value.has(provider);
			}),
			...endpointModelOptions.value,
			...configuredModelOptions.value,
		].filter((model, index, all) => all.indexOf(model) === index),
	);

	function parseConfigPayload(raw: string): AppConfigLike | null {
		const trimmed = raw.trim();
		if (!trimmed) {
			return null;
		}
		try {
			return (JSON.parse(trimmed) as AppConfigLike) ?? null;
		} catch (error) {
			if (error instanceof SyntaxError) {
				return null;
			}
			throw error;
		}
	}

	function collectConfiguredModels(cfg: AppConfigLike | null): string[] {
		if (!cfg) {
			return [];
		}
		const out = new Set<string>();
		const add = (model: string | undefined) => {
			const trimmed = model?.trim();
			if (trimmed) {
				out.add(trimmed);
			}
		};

		add(cfg.models?.defaults?.model);
		for (const fb of cfg.models?.defaults?.fallbacks ?? []) {
			add(fb);
		}
		for (const agent of cfg.agents ?? []) {
			add(agent.model);
			for (const fb of agent.fallbacks ?? []) {
				add(fb);
			}
		}

		return Array.from(out);
	}

	async function refreshEndpointModels(cfg: AppConfigLike | null) {
		const providers = cfg?.models?.providers ?? {};

		const rp = new Set<string>();
		for (const [name, providerCfg] of Object.entries(providers)) {
			if (providerCfg?.region?.trim()) {
				rp.add(name);
			}
		}
		regionProviders.value = rp;

		const endpointProviders = Object.entries(providers).filter(
			([name, providerCfg]) =>
				(name === "vllm" || name === "ollama") &&
				(Boolean(providerCfg?.base_uri?.trim()) ||
					Boolean(providerCfg?.auth?.trim())),
		);

		if (endpointProviders.length === 0) {
			endpointModelOptions.value = [];
			return;
		}

		const results = await Promise.allSettled(
			endpointProviders.map(async ([provider, providerCfg]) => {
				const raw = await callTool("models_list", {
					provider,
					base_uri: providerCfg.base_uri?.trim() || undefined,
					auth: providerCfg.auth?.trim() || undefined,
				});
				const parsed = parseStringArrayPayload(raw);
				return parsed ?? [];
			}),
		);

		const merged: string[] = [];
		for (const result of results) {
			if (result.status === "fulfilled") {
				merged.push(...result.value);
			}
		}
		endpointModelOptions.value = merged.filter(
			(model, index, all) => all.indexOf(model) === index,
		);
	}

	async function refreshConfigBackedModels() {
		try {
			const raw = await callTool("config_get");
			const cfg = parseConfigPayload(raw);
			configuredModelOptions.value = collectConfiguredModels(cfg);
			await refreshEndpointModels(cfg);
		} catch {
			configuredModelOptions.value = [];
			endpointModelOptions.value = [];
		}
	}

	async function refreshCredentials() {
		let lastError: unknown = null;
		for (let attempt = 0; attempt < 3; attempt += 1) {
			try {
				const raw = await callTool("auth_list");
				const parsed = parseStringArrayPayload(raw);
				if (parsed !== null) {
					credentials.value = parsed;
					await refreshConfigBackedModels();
					return;
				}
				lastError = new Error("auth_list returned invalid JSON");
			} catch (error) {
				lastError = error;
			}
			if (attempt < 2) {
				await delay(150 * (attempt + 1));
			}
		}
		console.warn(
			"Failed to refresh credentials; keeping previous state.",
			lastError,
		);
		await refreshConfigBackedModels();
	}

	return {
		availableModelOptions,
		credentials,
		refreshCredentials,
	};
}
