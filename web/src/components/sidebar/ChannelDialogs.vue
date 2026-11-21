<template>
	<!-- Add a channel: its name and type. Its settings open next. -->
	<DialogRoot :open="dialog?.mode === 'create'" @update:open="(v) => { if (!v && !busy) close() }">
		<DialogPortal>
			<DialogOverlay class="fixed inset-0 z-50 bg-black/50" />
			<DialogContent data-testid="channel-dialog"
				class="fixed left-1/2 top-1/2 z-50 max-h-[90dvh] w-[calc(100%-1.5rem)] max-w-lg -translate-x-1/2 -translate-y-1/2 overflow-y-auto rounded-xl bg-white p-6 shadow-2xl dark:bg-gray-900">
				<form @submit.prevent="create">
					<DialogTitle class="text-base font-bold text-gray-900 dark:text-white">Add channel</DialogTitle>
					<DialogDescription class="mt-1 text-sm text-gray-600 dark:text-gray-400">
						The channel gets its own chat session. It starts disabled until you fill in its connection settings.
					</DialogDescription>

					<label for="channel-name" class="field-label mt-4">Channel name</label>
					<input id="channel-name" v-model="form.name" type="text" autocomplete="off" spellcheck="false"
						class="field-input" placeholder="Team Slack" />

					<label for="channel-type" class="field-label mt-3">Type</label>
					<select id="channel-type" v-model="form.type" class="field-input">
						<option v-for="t in CHANNEL_TYPES" :key="t.value" :value="t.value">{{ t.label }}</option>
					</select>

					<p v-if="error" role="alert" class="mt-3 text-sm text-red-600 dark:text-red-400">{{ error }}</p>

					<div class="mt-6 flex justify-end gap-3">
						<DialogClose type="button" :disabled="busy"
							class="rounded-lg border border-gray-200 px-4 py-2 text-sm text-gray-600 hover:bg-gray-50 dark:border-gray-700 dark:text-gray-400 dark:hover:bg-gray-800">
							Cancel
						</DialogClose>
						<button type="submit" :disabled="busy || !form.name.trim()"
							class="rounded-lg bg-blue-600 px-4 py-2 text-sm font-semibold text-white hover:bg-blue-500 disabled:opacity-50">
							Create
						</button>
					</div>
				</form>
			</DialogContent>
		</DialogPortal>
	</DialogRoot>

	<!-- Remove a channel, archiving or deleting its sessions. -->
	<AlertDialogRoot :open="dialog?.mode === 'remove'" @update:open="(v) => { if (!v && !busy) close() }">
		<AlertDialogPortal>
			<AlertDialogOverlay class="fixed inset-0 z-50 bg-black/50" />
			<AlertDialogContent data-testid="channel-remove-dialog"
				class="fixed left-1/2 top-1/2 z-50 w-[calc(100%-1.5rem)] max-w-lg -translate-x-1/2 -translate-y-1/2 rounded-xl bg-white p-6 shadow-2xl dark:bg-gray-900">
				<AlertDialogTitle class="text-base font-bold text-gray-900 dark:text-white">
					Remove channel?
				</AlertDialogTitle>
				<AlertDialogDescription class="mt-2 text-sm text-gray-600 dark:text-gray-400">
					This removes
					<span class="break-all font-medium text-gray-900 dark:text-white">{{ dialog?.channel }}</span>
					and its settings. Tasks and reports that deliver to it stop delivering.
				</AlertDialogDescription>
				<fieldset class="mt-4 space-y-2">
					<legend class="text-sm font-medium text-gray-900 dark:text-white">Its chat sessions</legend>
					<label class="flex items-start gap-3 text-sm text-gray-700 dark:text-gray-300">
						<input v-model="sessions" type="radio" value="archive" name="channel-sessions" data-testid="channel-remove-archive"
							class="mt-0.5 h-4 w-4 border-gray-300 text-blue-600 focus:ring-blue-500 dark:border-gray-600 dark:bg-gray-800" />
						<span>Archive<span class="block text-xs text-gray-500 dark:text-gray-400">Keep them, with their history, as regular chats under Chats.</span></span>
					</label>
					<label class="flex items-start gap-3 text-sm text-gray-700 dark:text-gray-300">
						<input v-model="sessions" type="radio" value="delete" name="channel-sessions" data-testid="channel-remove-delete"
							class="mt-0.5 h-4 w-4 border-gray-300 text-blue-600 focus:ring-blue-500 dark:border-gray-600 dark:bg-gray-800" />
						<span>Delete<span class="block text-xs text-gray-500 dark:text-gray-400">Permanently delete them and all their messages. This cannot be undone.</span></span>
					</label>
				</fieldset>
				<p v-if="error" role="alert" class="mt-3 text-sm text-red-600 dark:text-red-400">{{ error }}</p>
				<div class="mt-6 flex justify-end gap-3">
					<AlertDialogCancel :disabled="busy"
						class="rounded-lg border border-gray-200 px-4 py-2 text-sm text-gray-600 hover:bg-gray-50 dark:border-gray-700 dark:text-gray-400 dark:hover:bg-gray-800">
						Cancel
					</AlertDialogCancel>
					<button type="button" :disabled="busy"
						class="rounded-lg bg-red-600 px-4 py-2 text-sm font-semibold text-white hover:bg-red-500 disabled:opacity-50"
						@click="remove">
						Remove
					</button>
				</div>
			</AlertDialogContent>
		</AlertDialogPortal>
	</AlertDialogRoot>
</template>

<script setup lang="ts">
import {
	AlertDialogCancel,
	AlertDialogContent,
	AlertDialogDescription,
	AlertDialogOverlay,
	AlertDialogPortal,
	AlertDialogRoot,
	AlertDialogTitle,
	DialogClose,
	DialogContent,
	DialogDescription,
	DialogOverlay,
	DialogPortal,
	DialogRoot,
	DialogTitle,
} from "radix-vue";
import { reactive, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { useChannelDialogs } from "../../composables/useChannelDialogs";
import { useMCP } from "../../composables/useMCP";
import {
	CHANNEL_TYPES,
	channelPath,
	channelSettingsPath,
} from "../../lib/channels";
import { useSettingsStore } from "../../stores/settings";

const { dialog, close } = useChannelDialogs();
const { callTool } = useMCP();
const settings = useSettingsStore();
const route = useRoute();
const router = useRouter();

const busy = ref(false);
const error = ref("");
const form = reactive({ name: "", type: "slack" });
const sessions = ref<"archive" | "delete">("archive");

watch(dialog, () => {
	error.value = "";
	form.name = "";
	form.type = "slack";
	sessions.value = "archive";
});

function message(e: unknown): string {
	return e instanceof Error ? e.message : String(e);
}

async function create() {
	const d = dialog.value;
	const name = form.name.trim();
	if (!d || !name) return;
	busy.value = true;
	error.value = "";
	try {
		await callTool("channel_create", { agent: d.agent, name, type: form.type });
		await settings.fetchConfig();
		close();
		await router.push(channelSettingsPath(d.agent, name));
	} catch (e) {
		error.value = message(e);
	} finally {
		busy.value = false;
	}
}

async function remove() {
	const d = dialog.value;
	if (!d?.channel) return;
	busy.value = true;
	error.value = "";
	try {
		await callTool("channel_remove", {
			agent: d.agent,
			channel: d.channel,
			sessions: sessions.value,
		});
		await settings.fetchConfig();
		close();
		// Leave the removed channel's pages.
		const removed = decodeURIComponent(channelPath(d.agent, d.channel));
		const current = decodeURIComponent(route.path);
		if (current === removed || current.startsWith(`${removed}/`))
			await router.push(channelPath(d.agent));
	} catch (e) {
		error.value = message(e);
	} finally {
		busy.value = false;
	}
}
</script>
