<template>
	<section data-testid="settings-notifications" class="space-y-6 pb-8">
		<div :class="card">
			<div class="flex items-start gap-4">
				<div class="min-w-0 flex-1">
					<h3 :class="heading">Notifications</h3>
					<p :class="hint" class="!mb-0">Turn every notification on or off at once.</p>
				</div>
				<ToggleSwitch data-testid="notifications-enabled" label="Notifications" :model-value="prefs.enabled"
					@update:model-value="set({ enabled: $event })" />
			</div>
		</div>

		<fieldset :disabled="!prefs.enabled" class="min-w-0 space-y-6 disabled:opacity-60">
			<div :class="card">
				<h3 :class="heading">Delivery</h3>
				<p :class="hint">Where notifications appear. Every notification also goes to the bell.</p>
				<div class="divide-y divide-gray-100 dark:divide-gray-800">
					<div class="flex items-start gap-4 py-3">
						<div class="min-w-0 flex-1">
							<p :class="rowLabel">In-app banners and toasts</p>
							<p :class="rowHint">Shown in Aviary while it's open.</p>
						</div>
						<ToggleSwitch data-testid="notifications-in-app" label="In-app banners and toasts" :model-value="prefs.inApp"
							@update:model-value="set({ inApp: $event })" />
					</div>
					<div class="flex items-start gap-4 py-3">
						<div class="min-w-0 flex-1">
							<p :class="rowLabel">System notifications</p>
							<p :class="rowHint">From the browser or the installed app, also while Aviary is in the background.</p>
							<p data-testid="notifications-permission" :data-state="permission" class="mt-1 text-xs"
								:class="permission === 'granted' ? 'text-emerald-700 dark:text-emerald-400' : 'text-amber-700 dark:text-amber-400'">
								{{ permissionText }}
							</p>
							<button v-if="permission === 'default'" type="button" data-testid="notifications-allow" :class="smallButton" class="mt-2"
								@click="notifications.requestPermission()">Allow notifications…</button>
						</div>
						<ToggleSwitch data-testid="notifications-system" label="System notifications" :model-value="prefs.system"
							@update:model-value="set({ system: $event })" />
					</div>
					<div class="flex items-start gap-4 py-3">
						<div class="min-w-0 flex-1">
							<p :class="rowLabel">macOS Notification Center when no browser is open</p>
							<p :class="rowHint">
								A server running on a Mac shows notifications itself (with terminal-notifier when installed, otherwise
								osascript) while no Aviary page is open. Banner or alert style is set in macOS System Settings →
								Notifications.
							</p>
						</div>
						<ToggleSwitch data-testid="notifications-server" label="macOS Notification Center when no browser is open"
							:model-value="prefs.server" @update:model-value="set({ server: $event })" />
					</div>
				</div>
			</div>

			<div :class="card">
				<h3 :class="heading">Events</h3>
				<p :class="hint">Which events notify you, how they appear, and their sound. Events on the page you're viewing never notify.</p>
				<div v-for="group in groups" :key="group.section" class="mt-4 first:mt-0">
					<h4 class="mb-1 text-xs font-semibold text-gray-700 dark:text-gray-300">{{ group.section }}</h4>
					<div class="divide-y divide-gray-100 dark:divide-gray-800">
						<div v-for="t in group.types" :key="t.type" :data-testid="`notification-event-${t.type}`"
							class="flex flex-wrap items-center gap-x-4 gap-y-2 py-2">
							<label class="flex min-w-0 flex-1 basis-56 items-center gap-2 text-sm text-gray-800 dark:text-gray-200">
								<input type="checkbox" class="h-4 w-4 rounded border-gray-300" data-testid="notification-event-enabled"
									:checked="prefs.events[t.type]?.enabled" @change="setEvent(t.type, { enabled: ($event.target as HTMLInputElement).checked })" />
								<span class="min-w-0">{{ t.label }}</span>
							</label>
							<div class="flex flex-wrap items-center gap-2">
								<div role="radiogroup" :aria-label="`${t.label}: delivery`" data-testid="notification-event-delivery" :class="segmented">
									<button v-for="d in DELIVERIES" :key="d.value" type="button" role="radio" :data-value="d.value"
										:aria-checked="prefs.events[t.type]?.delivery === d.value" :class="segment(prefs.events[t.type]?.delivery === d.value)"
										@click="setEvent(t.type, { delivery: d.value })">{{ d.label }}</button>
								</div>
								<select :aria-label="`${t.label}: sound`" data-testid="notification-event-sound" :class="select"
									:disabled="!prefs.sound.enabled" :value="prefs.events[t.type]?.sound ?? ''"
									@change="setEvent(t.type, { sound: ($event.target as HTMLSelectElement).value })">
									<option value="">Default sound</option>
									<option value="none">No sound</option>
									<option v-for="s in SOUNDS" :key="s.value" :value="s.value">{{ s.label }}</option>
								</select>
							</div>
						</div>
					</div>
				</div>
			</div>

			<div :class="card">
				<div class="flex items-start gap-4">
					<div class="min-w-0 flex-1">
						<h3 :class="heading">Sound</h3>
						<p :class="hint">Off by default.</p>
					</div>
					<ToggleSwitch data-testid="notifications-sound" label="Sound" :model-value="prefs.sound.enabled"
						@update:model-value="setSound({ enabled: $event })" />
				</div>
				<div class="grid gap-4 sm:grid-cols-2" :class="prefs.sound.enabled ? '' : 'opacity-60'">
					<label class="block">
						<span class="field-label">Sound type</span>
						<div class="flex items-center gap-2">
							<select data-testid="notifications-sound-type" aria-label="Sound type" :class="select" class="flex-1"
								:disabled="!prefs.sound.enabled" :value="prefs.sound.type"
								@change="setSound({ type: ($event.target as HTMLSelectElement).value })">
								<option v-for="s in SOUNDS" :key="s.value" :value="s.value">{{ s.label }}</option>
							</select>
							<button type="button" data-testid="notifications-sound-preview" :class="smallButton" :disabled="!prefs.sound.enabled"
								@click="playSound(prefs.sound.type, prefs.sound.volume)">Play</button>
						</div>
					</label>
					<label class="block">
						<span class="field-label">Volume <span class="text-gray-500">{{ prefs.sound.volume }}%</span></span>
						<input type="range" min="0" max="100" step="5" data-testid="notifications-volume" aria-label="Volume"
							class="mt-2 w-full" :disabled="!prefs.sound.enabled" :value="prefs.sound.volume"
							@change="setSound({ volume: Number(($event.target as HTMLInputElement).value) })" />
					</label>
				</div>
			</div>

			<div :class="card">
				<div class="flex items-start gap-4">
					<div class="min-w-0 flex-1">
						<h3 :class="heading">Quiet hours</h3>
						<p :class="hint">Hold back banners, toasts, system notifications and sound; notifications still reach the bell. Times are the server's.</p>
					</div>
					<ToggleSwitch data-testid="notifications-quiet" label="Quiet hours" :model-value="prefs.quietHours.enabled"
						@update:model-value="setQuiet({ enabled: $event })" />
				</div>
				<div class="flex flex-wrap items-end gap-4" :class="prefs.quietHours.enabled ? '' : 'opacity-60'">
					<label class="block">
						<span class="field-label">From</span>
						<input type="time" data-testid="notifications-quiet-start" class="field-input" :disabled="!prefs.quietHours.enabled"
							:value="prefs.quietHours.start" @change="setQuiet({ start: ($event.target as HTMLInputElement).value })" />
					</label>
					<label class="block">
						<span class="field-label">To</span>
						<input type="time" data-testid="notifications-quiet-end" class="field-input" :disabled="!prefs.quietHours.enabled"
							:value="prefs.quietHours.end" @change="setQuiet({ end: ($event.target as HTMLInputElement).value })" />
					</label>
				</div>
			</div>
		</fieldset>

		<div class="flex flex-wrap items-center gap-3">
			<button type="button" data-testid="notifications-test" :class="smallButton" :disabled="!prefs.enabled" @click="sendTest">
				Test notification
			</button>
			<button type="button" data-testid="notifications-reset" :class="smallButton" @click="notifications.reset()">
				Reset to defaults
			</button>
			<p class="text-xs text-gray-500 dark:text-gray-400">Changes apply at once and follow you to your other devices.</p>
		</div>
	</section>
</template>

<script setup lang="ts">
// Settings → Notifications (#1215): the master switch, each delivery
// method with the browser's permission state, each event type's on/off,
// style and sound, sound itself (off by default), quiet hours, and a test.
// Like Display, every change saves at once, on the server.
import { computed, onMounted } from "vue";
import { useNotifications } from "../../composables/useNotifications";
import {
	DELIVERIES,
	EVENT_TYPES,
	type NotificationEventPrefs,
	type NotificationPrefs,
	SOUNDS,
	systemPermission,
} from "../../lib/notifications";
import { playSound } from "../../lib/notificationSounds";
import ToggleSwitch from "../ui/ToggleSwitch.vue";

const notifications = useNotifications();
const { permission } = notifications;
const prefs = computed(() => notifications.prefs.value);

onMounted(() => {
	permission.value = systemPermission();
});

function set(change: Partial<NotificationPrefs>) {
	notifications.set(change);
}
function setSound(change: Partial<NotificationPrefs["sound"]>) {
	set({ sound: { ...prefs.value.sound, ...change } });
}
function setQuiet(change: Partial<NotificationPrefs["quietHours"]>) {
	set({ quietHours: { ...prefs.value.quietHours, ...change } });
}
function setEvent(type: string, change: Partial<NotificationEventPrefs>) {
	set({
		events: {
			...prefs.value.events,
			[type]: { ...prefs.value.events[type], ...change },
		},
	});
}

async function sendTest() {
	// Asking here too means the test's system notification can show.
	if (prefs.value.system && permission.value === "default")
		await notifications.requestPermission();
	await notifications.test();
}

const groups = computed(() => {
	const out: { section: string; types: typeof EVENT_TYPES }[] = [];
	for (const t of EVENT_TYPES) {
		const group = out.find((g) => g.section === t.section);
		if (group) group.types.push(t);
		else out.push({ section: t.section, types: [t] });
	}
	return out;
});

const permissionText = computed(() => {
	switch (permission.value) {
		case "granted":
			return "Allowed in this browser.";
		case "denied":
			return "Blocked in this browser. Allow notifications for this site in the browser's settings; until then they appear in Aviary instead.";
		case "unsupported":
			return "This browser can't show system notifications; they appear in Aviary instead.";
		case "insecure":
			return "System notifications need a secure (HTTPS) connection; they appear in Aviary instead.";
		default:
			return "Not allowed yet.";
	}
});

const card =
	"min-w-0 rounded-xl border border-gray-200 bg-white p-5 dark:border-gray-800 dark:bg-gray-900";
const heading =
	"text-sm font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400";
const hint = "mb-3 mt-1 text-xs text-gray-500 dark:text-gray-400";
const rowLabel = "text-sm font-medium text-gray-900 dark:text-gray-100";
const rowHint = "mt-0.5 text-xs text-gray-500 dark:text-gray-400";
const segmented =
	"inline-flex max-w-full flex-wrap gap-1 rounded-lg border border-gray-200 bg-gray-50 p-0.5 dark:border-gray-700 dark:bg-gray-950";
const select =
	"rounded-lg border border-gray-200 bg-white px-2 py-1 text-sm text-gray-800 disabled:opacity-50 dark:border-gray-700 dark:bg-gray-900 dark:text-gray-200";
const smallButton =
	"rounded-lg border border-gray-200 px-3 py-2 text-sm text-gray-700 transition hover:bg-gray-100 disabled:opacity-40 dark:border-gray-700 dark:text-gray-300 dark:hover:bg-gray-800";

function segment(active: boolean): string {
	const base = "rounded-md px-2 py-1 text-xs font-medium transition-colors";
	return active
		? `${base} bg-white text-gray-950 shadow-sm dark:bg-gray-800 dark:text-white`
		: `${base} text-gray-600 hover:text-gray-900 dark:text-gray-400 dark:hover:text-white`;
}
</script>
