<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";

type InstallKey = "curl" | "pwsh" | "brew" | "scoop";

const props = withDefaults(
	defineProps<{
		minimal?: boolean;
	}>(),
	{
		minimal: false,
	},
);

const installOptions = [
	{
		key: "curl",
		label: "Bash",
		prompt: "$",
		command: "curl -fsSL https://aviary.bot/install.sh | sh",
	},
	{
		key: "pwsh",
		label: "PowerShell",
		prompt: ">",
		command: "iwr https://aviary.bot/install.ps1 | iex",
	},
	{
		key: "brew",
		label: "Homebrew",
		prompt: "$",
		command: "brew tap lsegal/aviary && brew install aviary",
	},
	{
		key: "scoop",
		label: "Scoop",
		prompt: ">",
		command: "scoop bucket add aviary && scoop install aviary",
	},
] as const satisfies ReadonlyArray<{
	key: InstallKey;
	label: string;
	prompt: string;
	command: string;
}>;

const activeKey = ref<InstallKey>("curl");
const displayedCommand = ref(installOptions[0].command);
const copied = ref(false);
const showCaret = ref(false);

let typingTimer: ReturnType<typeof setTimeout> | null = null;

const activeOption = computed(
	() =>
		installOptions.find((option) => option.key === activeKey.value) ??
		installOptions[0],
);

function clearTypingTimer() {
	if (!typingTimer) return;
	clearTimeout(typingTimer);
	typingTimer = null;
}

function writeCommand(animate: boolean) {
	clearTypingTimer();
	const command = activeOption.value.command;

	if (props.minimal || !animate) {
		displayedCommand.value = command;
		showCaret.value = false;
		return;
	}

	displayedCommand.value = "";
	showCaret.value = true;
	let index = 0;

	const step = () => {
		if (index >= command.length) {
			showCaret.value = false;
			typingTimer = null;
			return;
		}
		displayedCommand.value += command[index] ?? "";
		index += 1;
		typingTimer = setTimeout(step, 16);
	};

	typingTimer = setTimeout(step, 80);
}

function setDefaultOption() {
	if (typeof navigator === "undefined") return;
	const platform =
		(navigator as Navigator & { userAgentData?: { platform?: string } })
			.userAgentData?.platform ??
		navigator.platform ??
		navigator.userAgent ??
		"";
	activeKey.value = /win/i.test(String(platform)) ? "pwsh" : "curl";
}

function copyCommand() {
	navigator.clipboard?.writeText(activeOption.value.command).then(() => {
		copied.value = true;
		setTimeout(() => {
			copied.value = false;
		}, 1500);
	});
}

onMounted(() => {
	setDefaultOption();
	writeCommand(false);
});

onBeforeUnmount(() => {
	clearTypingTimer();
});

watch(activeKey, (_value, oldValue) => {
	writeCommand(oldValue !== undefined);
});
</script>

<template>
	<div class="landing-install" :class="{ 'landing-install-minimal': minimal }">
		<div
			v-if="!minimal"
			class="landing-install-tabs"
			role="tablist"
			aria-label="Install methods"
		>
			<button
				v-for="option in installOptions"
				:key="option.key"
				type="button"
				class="landing-install-tab"
				:class="{ active: option.key === activeKey }"
				role="tab"
				:aria-selected="option.key === activeKey"
				@click="activeKey = option.key"
			>
				{{ option.label }}
			</button>
		</div>

		<div class="landing-install-command">
			<span class="prompt">{{ activeOption.prompt }}</span>
			<span class="command">
				{{ displayedCommand }}
				<span v-if="showCaret" class="caret" aria-hidden="true"></span>
			</span>
			<button
				type="button"
				class="landing-install-copy"
				:class="{ copied }"
				:aria-label="copied ? 'Copied install command' : 'Copy install command'"
				@click="copyCommand"
			>
				<svg
					v-if="!copied"
					width="14"
					height="14"
					viewBox="0 0 24 24"
					fill="none"
					stroke="currentColor"
					stroke-width="2"
					stroke-linecap="round"
					stroke-linejoin="round"
					aria-hidden="true"
				>
					<rect x="9" y="9" width="13" height="13" rx="2" />
					<path
						d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"
					/>
				</svg>
				<svg
					v-else
					width="14"
					height="14"
					viewBox="0 0 24 24"
					fill="none"
					stroke="currentColor"
					stroke-width="2"
					stroke-linecap="round"
					stroke-linejoin="round"
					aria-hidden="true"
				>
					<polyline points="20 6 9 17 4 12" />
				</svg>
			</button>
		</div>
	</div>
</template>
