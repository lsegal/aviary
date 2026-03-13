import modelCatalog from "../../../internal/models/catalog.json";

export interface ModelCatalogEntry {
	id: string;
	input_tokens: number;
	output_tokens: number;
	supports_image_input: boolean;
}

export const MODEL_CATALOG = modelCatalog as ModelCatalogEntry[];
export const SUPPORTED_MODELS = MODEL_CATALOG.map((entry) => entry.id);

export function providerOf(model: string): string {
	const idx = model.indexOf("/");
	return idx > 0 ? model.slice(0, idx) : "";
}

export function modelNameOf(model: string): string {
	const idx = model.indexOf("/");
	return idx > 0 ? model.slice(idx + 1) : model;
}

export const SUPPORTED_PROVIDERS = Array.from(
	new Set(MODEL_CATALOG.map((entry) => providerOf(entry.id)).filter(Boolean)),
);

export const MODEL_CATALOG_BY_ID = new Map(
	MODEL_CATALOG.map((entry) => [entry.id, entry] as const),
);

export function lookupModel(model: string): ModelCatalogEntry | undefined {
	return MODEL_CATALOG_BY_ID.get(model);
}

export function formatTokenCount(tokens: number): string {
	if (tokens >= 1_000_000) {
		return `${(tokens / 1_000_000).toFixed(2)}M`;
	}
	if (tokens >= 1_000) {
		if (tokens % 1_000 === 0) return `${tokens / 1_000}k`;
		return `${(tokens / 1_000).toFixed(1)}k`;
	}
	return `${tokens}`;
}

export function modelSupportLabel(model: string): string {
	const entry = lookupModel(model);
	if (!entry) return "";
	return entry.supports_image_input ? "Text+image" : "Text";
}

export function modelDetailLabel(model: string): string {
	const entry = lookupModel(model);
	if (!entry) return "";
	return `In ${formatTokenCount(entry.input_tokens)} • Out ${formatTokenCount(entry.output_tokens)} • ${modelSupportLabel(model)}`;
}
