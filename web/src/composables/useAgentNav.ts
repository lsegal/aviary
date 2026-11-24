import { computed } from "vue";
import { AGENT_PAGES, agentPagePath } from "../lib/agentPages";
import { MINIMAL_AGENT_PAGES } from "../lib/minimalMode";
import { arrangeNav, type NavList } from "../lib/navPrefs";
import { SYSTEM_PAGES } from "../lib/systemNav";
import { useAuthStore } from "../stores/auth";
import { useNavPrefs } from "./useNavPrefs";

// The user's navigation order and visibility (#1003) applied to the pages a
// session can show, shared by the sidebar and every page's agent crumb, so
// they all agree on where an agent's crumb leads (#1091).
export function useAgentNav() {
	const auth = useAuthStore();
	const nav = useNavPrefs();
	// Pages that never show whatever the preferences say: those a minimal
	// `aviary watch` session leaves out (#991).
	const forcedHidden = computed<ReadonlySet<string>>(() =>
		auth.minimal
			? new Set([
					...AGENT_PAGES.map((p) => p.key).filter(
						(k) => !MINIMAL_AGENT_PAGES.has(k),
					),
					...SYSTEM_PAGES.map((p) => p.key),
				])
			: new Set(),
	);
	function arranged<T extends { key: string }>(items: T[], list: NavList): T[] {
		return arrangeNav(items, list, nav.prefs.value, {
			forcedHidden: forcedHidden.value,
			showHidden: nav.showHidden.value,
		});
	}
	/** The agent's first page in the user's sidebar order, else its Chats. */
	function agentHome(agent: string): string {
		const first = arranged([...AGENT_PAGES], "agentPages")[0];
		return agentPagePath(agent, first?.key ?? "chat");
	}
	return { nav, forcedHidden, arranged, agentHome };
}
