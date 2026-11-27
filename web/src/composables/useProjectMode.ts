// A project's issue discovery mode (#1307, #1461): Watch picks up issues
// matching the filter, Manual never searches for issues. The checked mode is
// the effective one; picking the global default clears the project's
// override and picking the other mode stores it. A change applies at once
// through project_update, to this project only. Shared by the header's eye
// menu and the board's "…" menu, which takes the eye's place on narrow
// headers (#1488).

import { computed, ref } from "vue";
import {
	type ProjectMode,
	type ProjectModeChoice,
	projectModeLabel,
	projectModeOverride,
} from "../lib/projects";
import { useMCP } from "./useMCP";

export function useProjectMode(
	props: { target: string; manual: boolean; defaultMode: ProjectMode },
	onChanged: (mode: ProjectModeChoice) => void,
) {
	const { callTool } = useMCP();
	const saving = ref(false);
	const error = ref("");
	const choice = computed<ProjectMode>(() =>
		props.manual ? "manual" : "watch",
	);
	const label = computed(() => projectModeLabel(choice.value));

	async function pick(value: string) {
		const picked = value as ProjectMode;
		if (picked === choice.value) return;
		const mode = projectModeOverride(picked, props.defaultMode);
		saving.value = true;
		error.value = "";
		try {
			await callTool("project_update", { target: props.target, mode });
			onChanged(mode);
		} catch (e) {
			error.value = e instanceof Error ? e.message : String(e);
		} finally {
			saving.value = false;
		}
	}

	return { saving, error, choice, label, pick };
}
