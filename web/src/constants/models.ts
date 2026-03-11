import modelCatalog from "../../../internal/models/catalog.json";

export const SUPPORTED_MODELS = modelCatalog as string[];

export function providerOf(model: string): string {
	const idx = model.indexOf("/");
	return idx > 0 ? model.slice(0, idx) : "";
}

export const SUPPORTED_PROVIDERS = Array.from(
	new Set(SUPPORTED_MODELS.map(providerOf).filter(Boolean)),
);
