import ChatBubbleLeftRightIcon from "@heroicons/vue/24/outline/ChatBubbleLeftRightIcon";
import ClockIcon from "@heroicons/vue/24/outline/ClockIcon";
import Cog6ToothIcon from "@heroicons/vue/24/outline/Cog6ToothIcon";
import DocumentTextIcon from "@heroicons/vue/24/outline/DocumentTextIcon";
import HashtagIcon from "@heroicons/vue/24/outline/HashtagIcon";
import ServerStackIcon from "@heroicons/vue/24/outline/ServerStackIcon";
import UsersIcon from "@heroicons/vue/24/outline/UsersIcon";
import { type Component, markRaw } from "vue";
import CodeLoopIcon from "../components/CodeLoopIcon.vue";
import {
	ACCENTS,
	isSettingsPath,
	type SystemPageIdentity,
	systemPageIdentity,
	WATCH_IDENTITY,
} from "./systemNav";

/** An agent page's icon, in the sidebar rail and in its page header. */
export const AGENT_PAGE_ICONS: Record<string, Component> = {
	chat: markRaw(ChatBubbleLeftRightIcon),
	projects: markRaw(CodeLoopIcon),
	channels: markRaw(HashtagIcon),
	tasks: markRaw(ClockIcon),
	files: markRaw(DocumentTextIcon),
	settings: markRaw(Cog6ToothIcon),
};

const muted = (icon: Component): SystemPageIdentity => ({
	icon,
	...ACCENTS.gray,
});

/**
 * The icon tile of the section the page at path belongs to, shown in its
 * page header (#1147): a System page keeps its accent (#896); an agent's
 * pages and the rest use their sidebar icon on a muted gray tile.
 */
export function pageIdentity(path: string): SystemPageIdentity {
	const agent = /^\/agents\/[^/]+\/([^/]+)/.exec(path);
	if (agent) return muted(AGENT_PAGE_ICONS[agent[1]] ?? Cog6ToothIcon);
	if (path.startsWith("/agents")) return muted(markRaw(UsersIcon));
	if (path.startsWith("/projects")) return muted(AGENT_PAGE_ICONS.projects);
	if (path.startsWith("/system/watches/")) return WATCH_IDENTITY;
	if (isSettingsPath(path)) return muted(AGENT_PAGE_ICONS.settings);
	const page = /^\/system\/([^/]+)/.exec(path)?.[1] ?? "";
	return systemPageIdentity(page) ?? muted(markRaw(ServerStackIcon));
}
