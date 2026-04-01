import type { InjectionKey } from "vue";

// biome-ignore lint/suspicious/noExplicitAny: shared injected UI context is intentionally broad during this view split
export const settingsViewContextKey: InjectionKey<any> = Symbol(
	"settings-view-context",
);
