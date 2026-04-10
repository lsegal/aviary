<template>
	<div ref="shellEl" class="install-shell">
		<div class="install-tabs" role="tablist" aria-label="Installation methods">
			<button v-for="option in installOptions" :key="option.key" class="install-tab"
				:class="{ active: option.key === activeKey }" type="button" role="tab" :aria-selected="option.key === activeKey"
				@click="activeKey = option.key">
				{{ option.label }}
			</button>
		</div>

		<div ref="blockEl" class="install-block">
			<span class="install-prompt" aria-hidden="true">{{ activeOption.prompt }}</span>
			<span class="install-command" data-no-typing="true">{{ displayedCommand }}<span v-if="showCaret"
					class="install-caret"></span></span>
			<button class="install-copy" :class="{ copied }" @click="copy" :title="copied ? 'Copied!' : 'Copy'">
				<svg v-if="!copied" xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none"
					stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
					<rect x="9" y="9" width="13" height="13" rx="2" />
					<path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1" />
				</svg>
				<svg v-else xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none"
					stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
					<polyline points="20 6 9 17 4 12" />
				</svg>
			</button>
		</div>
	</div>
</template>

<script setup lang="ts">
import {
	computed,
	nextTick,
	onBeforeUnmount,
	onMounted,
	ref,
	watch,
} from "vue";

type InstallOptionKey = "curl" | "pwsh" | "brew" | "scoop";

const installOptions = [
	{
		key: "curl",
		label: "Bash",
		prompt: "$",
		command: "curl -fsSL https://aviary.bot/install.sh | sh",
	},
	{
		key: "pwsh",
		label: "Powershell",
		prompt: ">",
		command: "iwr https://aviary.bot/install.ps1 | iex",
	},
	{
		key: "brew",
		label: "Homebrew",
		prompt: "$",
		command:
			"brew tap lsegal/aviary https://github.com/lsegal/aviary && brew install aviary",
	},
	{
		key: "scoop",
		label: "Scoop",
		prompt: ">",
		command:
			"scoop bucket add aviary https://github.com/lsegal/aviary && scoop install aviary/aviary",
	},
] as const satisfies ReadonlyArray<{
	key: InstallOptionKey;
	label: string;
	prompt: string;
	command: string;
}>;

const isWindows = computed(() => {
	if (typeof navigator === "undefined") return false;
	const uaPlatform =
		(navigator as unknown as { userAgentData?: { platform?: string } })
			.userAgentData?.platform ||
		navigator.platform ||
		navigator.userAgent ||
		"";
	return /win/i.test(String(uaPlatform));
});

const activeKey = ref<InstallOptionKey>("curl");
const activeOption = computed(
	() =>
		installOptions.find((option) => option.key === activeKey.value) ||
		installOptions[0],
);

const copied = ref(false);
const shellEl = ref<HTMLElement | null>(null);
const blockEl = ref<HTMLElement | null>(null);
const hasMounted = ref(false);
const displayedCommand = ref(installOptions[0].command);
const showCaret = ref(false);
let typingTimer: ReturnType<typeof setTimeout> | null = null;
let typingRunId = 0;

function copy() {
	navigator.clipboard?.writeText(activeOption.value.command).then(() => {
		copied.value = true;
		setTimeout(() => {
			copied.value = false;
		}, 2000);
	});
}

function isCompactViewport() {
	return (
		typeof window !== "undefined" &&
		window.matchMedia("(max-width: 449px)").matches
	);
}

function clearTypingTimer() {
	if (typingTimer) {
		clearTimeout(typingTimer);
		typingTimer = null;
	}
}

function measureNaturalWidth(el: HTMLElement) {
	const previousWidth = el.style.width;
	el.style.width = "fit-content";
	const width = el.getBoundingClientRect().width;
	el.style.width = previousWidth;
	return width;
}

function measureTargetWidth(command: string) {
	const el = blockEl.value;
	if (!el || typeof document === "undefined") return 0;

	const clone = el.cloneNode(true) as HTMLElement;
	clone.style.position = "absolute";
	clone.style.visibility = "hidden";
	clone.style.left = "-9999px";
	clone.style.top = "0";
	clone.style.width = "fit-content";
	clone.style.maxWidth = "none";
	clone.style.transition = "none";

	const commandEl = clone.querySelector(".install-command");
	if (commandEl) {
		commandEl.textContent = command;
	}

	document.body.appendChild(clone);
	const width = clone.getBoundingClientRect().width;
	document.body.removeChild(clone);
	return width;
}

async function syncInstallWidth(
	animate: boolean,
	targetCommand = activeOption.value.command,
) {
	const el = blockEl.value;
	const shell = shellEl.value;
	if (!el || !shell || typeof window === "undefined") return;

	if (isCompactViewport()) {
		el.style.width = "100%";
		return;
	}

	const startWidth = el.getBoundingClientRect().width;
	if (startWidth > 0) {
		el.style.width = `${Math.ceil(startWidth)}px`;
	}

	await nextTick();

	const shellParentWidth =
		shell.parentElement?.getBoundingClientRect().width ?? window.innerWidth;
	const naturalWidth =
		measureTargetWidth(targetCommand) || measureNaturalWidth(el);
	const targetWidth = Math.min(naturalWidth, shellParentWidth);

	if (!animate || !hasMounted.value) {
		el.style.width = `${Math.ceil(targetWidth)}px`;
		return;
	}

	requestAnimationFrame(() => {
		el.style.width = `${Math.ceil(targetWidth)}px`;
	});
}

function typeCommand(command: string, animateWidth: boolean) {
	const runId = ++typingRunId;
	clearTypingTimer();
	showCaret.value = true;
	displayedCommand.value = "";

	void syncInstallWidth(animateWidth, command);

	const beginTyping = () => {
		let index = 0;
		const step = () => {
			if (runId !== typingRunId) return;
			if (index < command.length) {
				displayedCommand.value += command[index];
				index += 1;
				typingTimer = setTimeout(step, 14);
				return;
			}

			showCaret.value = false;
			typingTimer = null;
		};

		step();
	};

	typingTimer = setTimeout(beginTyping, animateWidth ? 90 : 40);
}

function handleResize() {
	clearTypingTimer();
	showCaret.value = false;
	displayedCommand.value = activeOption.value.command;
	void syncInstallWidth(false, activeOption.value.command);
}

onMounted(() => {
	if (isWindows.value) {
		activeKey.value = "pwsh";
	}

	window.addEventListener("resize", handleResize);
	typeCommand(activeOption.value.command, false);
	void syncInstallWidth(false, activeOption.value.command).then(() => {
		hasMounted.value = true;
	});
});

onBeforeUnmount(() => {
	clearTypingTimer();
	if (typeof window !== "undefined") {
		window.removeEventListener("resize", handleResize);
	}
});

watch(activeKey, async () => {
	typeCommand(activeOption.value.command, hasMounted.value);
});
</script>
