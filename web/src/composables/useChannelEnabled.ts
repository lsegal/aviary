import { reactive } from "vue";
import { channelName } from "../lib/channels";
import { useSettingsStore } from "../stores/settings";
import { useMCP } from "./useMCP";
import { useToasts } from "./useToasts";

// The channels whose Enable or Disable is in flight, by "<agent>/<channel>".
const pending = reactive(new Set<string>());

/**
 * Enables and disables channels from the sidebar's "…" menu. The settings
 * store flips first, so the sidebar and the channel's settings show the new
 * state at once; a failed call flips it back and says why in a toast.
 */
export function useChannelEnabled() {
	const settings = useSettingsStore();
	const { callTool } = useMCP();
	const { show } = useToasts();

	function isPending(agent: string, name: string): boolean {
		return pending.has(`${agent}/${name}`);
	}

	/** Sets a channel's enabled state; resolves false when it failed. */
	async function setChannelEnabled(
		agent: string,
		name: string,
		enabled: boolean,
	): Promise<boolean> {
		const id = `${agent}/${name}`;
		if (pending.has(id)) return false;
		pending.add(id);
		const channel = settings.config?.agents
			.find((a) => a.name === agent)
			?.channels.find((c) => channelName(c) === name);
		const before = channel?.enabled;
		if (channel) channel.enabled = enabled;
		try {
			await callTool("channel_update", { agent, channel: name, enabled });
			await settings.fetchConfig();
			return true;
		} catch (e) {
			if (channel) {
				if (before === undefined) delete channel.enabled;
				else channel.enabled = before;
			}
			const reason = e instanceof Error ? e.message : String(e);
			show(
				`Couldn't ${enabled ? "enable" : "disable"} ${name}: ${reason}`,
				"error",
			);
			return false;
		} finally {
			pending.delete(id);
		}
	}

	return { setChannelEnabled, isPending };
}
