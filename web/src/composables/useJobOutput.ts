import { ref, watch } from "vue";

// How job output shows on board cards: the structured transcript ("rich") or
// the terminal scrollback ("raw"). One choice applies to every card and is
// remembered per browser.

export type JobOutputMode = "rich" | "raw";

const MODE_KEY = "aviary.projects.outputMode";

const mode = ref<JobOutputMode>(
	localStorage.getItem(MODE_KEY) === "raw" ? "raw" : "rich",
);

watch(mode, (value) => localStorage.setItem(MODE_KEY, value));

export function useJobOutputMode() {
	return mode;
}
