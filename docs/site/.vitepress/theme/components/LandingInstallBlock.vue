<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";

type InstallKey = "curl" | "pwsh" | "docker";

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
		command:
			"curl -fsSL https://github.com/lsegal/aviary/releases/latest/download/install.sh | sh",
	},
	{
		key: "pwsh",
		label: "PowerShell",
		prompt: ">",
		command:
			"irm https://github.com/lsegal/aviary/releases/latest/download/install.ps1 | iex",
	},
	{
		key: "docker",
		label: "Docker",
		prompt: "$",
		command: "docker run -p 16677:16677 gchcr.io/lsegal/aviary",
	},
] satisfies ReadonlyArray<{
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
		<div v-if="!minimal" class="landing-install-tabs" role="tablist" aria-label="Install methods">
			<button v-for="option in installOptions" :key="option.key" type="button" class="landing-install-tab"
				:class="{ active: option.key === activeKey }" role="tab" :aria-selected="option.key === activeKey"
				@click="activeKey = option.key">
				{{ option.label }}
			</button>
		</div>

		<div class="landing-install-command">
			<span class="prompt">{{ activeOption.prompt }}</span>
			<span class="command">
				{{ displayedCommand }}
				<span v-if="showCaret" class="caret" aria-hidden="true"></span>
			</span>
			<button type="button" class="landing-install-copy" :class="{ copied }"
				:aria-label="copied ? 'Copied install command' : 'Copy install command'" @click="copyCommand">
				<svg v-if="!copied" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor"
					stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
					<rect x="9" y="9" width="13" height="13" rx="2" />
					<path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1" />
				</svg>
				<svg v-else width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"
					stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
					<polyline points="20 6 9 17 4 12" />
				</svg>
			</button>
		</div>
	</div>
</template>

<style scoped>
.landing-install {
	overflow: hidden;
	border: 1px solid var(--rule-strong);
	border-radius: 14px;
	background: var(--paper);
	box-shadow: var(--shadow-sm);
}

.landing-install-tabs {
	display: flex;
	gap: 2px;
	padding: 6px;
	border-bottom: 1px solid var(--rule);
	background: color-mix(in oklab, var(--bg-soft) 60%, transparent);
}

.landing-install-tab {
	flex: 1;
	padding: 8px 12px;
	border: 0;
	border-radius: 8px;
	background: transparent;
	color: var(--ink-2);
	font: inherit;
	font-size: 13px;
	font-weight: 500;
	cursor: pointer;
	transition:
		background-color 0.15s ease,
		color 0.15s ease;
}

.landing-install-tab:hover {
	color: var(--ink);
}

.landing-install-tab.active {
	background: var(--paper);
	color: var(--ink);
	box-shadow: 0 1px 2px rgba(0, 0, 0, 0.06);
}

.landing-install-command {
	display: flex;
	align-items: center;
	gap: 10px;
	padding: 14px 16px;
	color: var(--ink);
	font-family: var(--mono);
	font-size: 13.5px;
}

.landing-install-command .prompt {
	align-self: flex-start;
	line-height: 1.5;
	color: var(--brand);
	font-weight: 600;
}

/* Commands wrap instead of truncating, so the full install URL stays readable. */
.landing-install-command .command {
	flex: 1;
	min-width: 0;
	line-height: 1.5;
	overflow-wrap: anywhere;
}

.landing-install-command .caret {
	display: inline-block;
	width: 8px;
	height: 1em;
	margin-left: 2px;
	background: var(--brand);
	vertical-align: -2px;
	animation: blink 1.05s step-end infinite;
}

.landing-install-copy {
	display: grid;
	place-items: center;
	width: 32px;
	height: 32px;
	border: 1px solid var(--rule);
	border-radius: 8px;
	background: transparent;
	color: var(--ink-2);
	cursor: pointer;
	transition:
		color 0.15s ease,
		border-color 0.15s ease,
		background-color 0.15s ease;
}

.landing-install-copy:hover {
	border-color: var(--rule-strong);
	background: color-mix(in oklab, var(--bg-soft) 45%, transparent);
	color: var(--ink);
}

.landing-install-copy.copied {
	border-color: #2eb67d;
	color: #2eb67d;
}

.landing-install-minimal .landing-install-command {
	padding-block: 12px;
}

@keyframes blink {
	50% {
		opacity: 0;
	}
}
</style>
