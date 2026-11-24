import { computed, type Ref, watch } from "vue";
import { useRoute } from "vue-router";
import { projectOwner } from "../lib/projectSettings";
import type { ProjectSummary } from "../lib/projects";
import {
	type CrumbSwitcher,
	channelItems,
	fileItems,
	projectItems,
	SWITCHER_TITLES,
	type SwitcherItem,
	taskItems,
} from "../lib/switcher";
import { useAgentFilesStore } from "../stores/agentFiles";
import { useSettingsStore } from "../stores/settings";

/**
 * The breadcrumb leaf's switcher (#1063): its title and sibling rows, from
 * the projects, tasks, channels or files the leaf's agent has, or null.
 */
export function useCrumbSwitcher(
	switcher: Ref<CrumbSwitcher | undefined>,
	projects: Ref<ProjectSummary[]>,
) {
	const route = useRoute();
	const settings = useSettingsStore();
	const files = useAgentFilesStore();

	// The sidebar lists an agent's files only while it is expanded.
	watch(
		() => switcher.value?.kind === "file" && switcher.value.agent,
		(agent) => {
			if (agent && !files.files(agent).length) void files.load(agent);
		},
		{ immediate: true },
	);

	const items = computed<SwitcherItem[]>(() => {
		const sw = switcher.value;
		if (!sw) return [];
		const config = settings.config?.agents.find((a) => a.name === sw.agent);
		const query = Object.fromEntries(
			Object.entries(route.query).filter(
				(e): e is [string, string] => typeof e[1] === "string",
			),
		);
		switch (sw.kind) {
			case "project":
				return projectItems(
					sw,
					projects.value.filter(
						(p) => projectOwner(settings.config, p.name) === sw.agent,
					),
					query,
				);
			case "task":
				return taskItems(sw, config?.tasks ?? []);
			case "channel":
				return channelItems(sw, config?.channels ?? []);
			case "file":
				return fileItems(sw, files.files(sw.agent));
		}
		return [];
	});

	// Nothing to switch to until the siblings have loaded.
	return computed(() =>
		switcher.value && items.value.length
			? { title: SWITCHER_TITLES[switcher.value.kind], items: items.value }
			: null,
	);
}
