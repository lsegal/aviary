<script setup lang="ts">
import { siDiscord, siSignal } from "simple-icons";
import { useData, withBase } from "vitepress";
import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import LandingInstallBlock from "./LandingInstallBlock.vue";

interface ShowcaseItem {
	title: string;
	description: string;
	bullets: string[];
	lightSrc: string;
	darkSrc: string;
	alt: string;
}

const showcaseItems: ShowcaseItem[] = [
	{
		title: "Plan the morning",
		description:
			"Keep recurring planning threads in one shared chat view with tool calls and context inline.",
		bullets: [
			"Separate tabs for recurring threads.",
			"Tool calls stay attached to the answer.",
			"Good for daily briefs and handoffs.",
		],
		lightSrc: "/screenshots/chat-workspace-light.png",
		darkSrc: "/screenshots/chat-workspace-dark.png",
		alt: "Shared chat workspace with tool calls",
	},
	{
		title: "Configure everything",
		description:
			"Set up channels, sender rules, permissions, and model overrides from one screen.",
		bullets: [
			"Per-channel model overrides.",
			"Sender allowlists and mention-only routing.",
			"Agent-level permissions without guesswork.",
		],
		lightSrc: "/screenshots/configure-everything-light.png",
		darkSrc: "/screenshots/configure-everything-dark.png",
		alt: "Configure channels and model overrides",
	},
	{
		title: "Usage analytics",
		description:
			"Watch live spend, provider mix, and session activity as jobs run.",
		bullets: [
			"Provider-level token breakdowns.",
			"Session activity timelines.",
			"Live cost visibility while jobs are running.",
		],
		lightSrc: "/screenshots/usage-analytics-light.png",
		darkSrc: "/screenshots/usage-analytics-dark.png",
		alt: "Usage analytics",
	},
	{
		title: "Security minded",
		description:
			"Review low-privilege defaults, allowlists, and scoped paths at a glance.",
		bullets: [
			"Filesystem access is explicitly scoped.",
			"Tool access is opt-in, not accidental.",
			"Exec allowlists keep shell usage narrow.",
		],
		lightSrc: "/screenshots/security-minded-light.png",
		darkSrc: "/screenshots/security-minded-dark.png",
		alt: "Permissions and allowlists",
	},
	{
		title: "Jobs and compile attempts",
		description:
			"See queue health, scheduled runs, retries, and compile attempts in one place.",
		bullets: [
			"Scheduled jobs and retries in one view.",
			"Compile status visible per task.",
			"Failure details without leaving the browser.",
		],
		lightSrc: "/screenshots/system-jobs-light.png",
		darkSrc: "/screenshots/system-jobs-dark.png",
		alt: "Jobs and compile attempts",
	},
];

const docsLinks = [
	{
		number: "01",
		title: "Getting started",
		description:
			"Install Aviary, start the server, log into the dashboard, and launch your first agent.",
		href: "/guide/getting-started",
	},
	{
		number: "02",
		title: "Configuration",
		description:
			"Everything in aviary.yaml - models, permissions, agents, and channels.",
		href: "/guide/configuration",
	},
	{
		number: "03",
		title: "Channels",
		description:
			"Connect Slack, Discord, or Signal and set sender rules, mentions, and delivery targets.",
		href: "/guide/channels",
	},
	{
		number: "04",
		title: "Security and permissions",
		description:
			"Lock down agent access and choose sensible host isolation for safer deployments.",
		href: "/guide/security-permissions",
	},
	{
		number: "05",
		title: "Scheduled tasks",
		description:
			"Timers, file triggers, and how Aviary compiles tasks into free scripts.",
		href: "/guide/scheduled-tasks",
	},
	{
		number: "06",
		title: "Day-to-day ops",
		description:
			"Manage chats, check jobs, read logs, and handle the everyday while Aviary is running.",
		href: "/guide/operations",
	},
	{
		number: "07",
		title: "MCP tool reference",
		description:
			"Exact tool names, inputs, and behavior for building automations.",
		href: "/reference/mcp/",
	},
	{
		number: "08",
		title: "CLI reference",
		description: "Every command and flag the dashboard exposes, at the prompt.",
		href: "/reference/cli",
	},
];

const configSnippet = [
	"server:",
	"  port: 16677",
	"  external_access: false",
	"",
	"models:",
	"  defaults:",
	"    model: anthropic/claude-sonnet-4-6",
	"",
	"agents:",
	"  - name: assistant",
	"    model: anthropic/claude-sonnet-4-6",
	"    memory: private",
	"    memory_tokens: 2048",
	"    working_dir: ~/workspace",
	"    permissions:",
	"      preset: standard",
].join("\n");

const showcaseIndex = ref(0);
const { isDark } = useData();

const promptTokensPerRun = 5000;
const compiledTokensPerRun = 100;
const tokenSavingsPerRun = promptTokensPerRun - compiledTokensPerRun;

const activeShowcase = computed(
	() => showcaseItems[showcaseIndex.value] ?? showcaseItems[0],
);
const wholeNumberFormatter = new Intl.NumberFormat("en-US");

function formatWholeNumber(value: number) {
	return wholeNumberFormatter.format(value);
}

function resolveShowcaseSrc(item: ShowcaseItem) {
	return withBase(isDark.value ? item.darkSrc : item.lightSrc);
}

onMounted(() => {
	if (typeof document !== "undefined") {
		document.body.classList.add("landing-home-active");
	}
});

onBeforeUnmount(() => {
	if (typeof document !== "undefined") {
		document.body.classList.remove("landing-home-active");
	}
});
</script>

<template>
	<div class="landing-home">
		<section class="landing-hero">
			<div class="landing-wrap landing-hero-inner">
				<div class="landing-hero-copy">
					<div class="landing-hero-eyebrow reveal">
						<span>Open Source</span>
						<span class="sep" aria-hidden="true">&#183;</span>
						<span>MIT Licensed</span>
					</div>

					<h1 class="landing-hero-title reveal">
						Give your AI<br />
						a place to <span class="accent">roost.</span>
					</h1>
					<p class="landing-hero-lede reveal">
						Aviary is the nest for your agents. Connect them to Slack,
						Signal, and Discord, hold long conversations, schedule repeat
						work, and run it all from a single binary with a built-in dashboard.
					</p>

					<div class="reveal">
						<LandingInstallBlock id="install" />
					</div>

					<div class="landing-hero-cta reveal">
						<a class="landing-btn landing-btn-ghost" :href="withBase('/guide/')">
							Read the guide
							<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"
								stroke-linecap="round" aria-hidden="true">
								<path d="M5 12h14M13 5l7 7-7 7" />
							</svg>
						</a>
						<a class="landing-btn landing-btn-primary" href="https://github.com/lsegal/aviary" target="_blank"
							rel="noreferrer">
							<svg width="14" height="14" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true"
								style="flex-shrink:0">
								<path
									d="M12 .5C5.648.5.5 5.648.5 12c0 5.085 3.292 9.387 7.86 10.91.575.107.786-.25.786-.554 0-.274-.01-1.177-.016-2.13-3.197.695-3.872-1.377-3.872-1.377-.522-1.327-1.274-1.68-1.274-1.68-1.043-.713.08-.698.08-.698 1.152.08 1.757 1.18 1.757 1.18 1.024 1.752 2.686 1.245 3.342.951.103-.74.4-1.246.727-1.532-2.553-.29-5.237-1.277-5.237-5.686 0-1.256.448-2.283 1.182-3.09-.119-.289-.512-1.456.112-3.034 0 0 .963-.308 3.157 1.18a10.98 10.98 0 012.874-.387c.975.005 1.957.132 2.874.387 2.193-1.488 3.155-1.18 3.155-1.18.624 1.578.232 2.745.113 3.034.737.807 1.181 1.834 1.181 3.09 0 4.42-2.69 5.392-5.253 5.676.412.355.78 1.056.78 2.13 0 1.538-.014 2.776-.014 3.155 0 .307.208.667.793.554C20.21 21.384 23.5 17.083 23.5 12c0-6.352-5.148-11.5-11.5-11.5z" />
							</svg>
							lsegal/aviary
						</a>
					</div>

					<div class="landing-hero-meta reveal">
						<span class="item">
							<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"
								stroke-linecap="round" aria-hidden="true">
								<path d="M20 7L9 18l-5-5" />
							</svg>
							Single binary
						</span>
						<span class="item">
							<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"
								stroke-linecap="round" aria-hidden="true">
								<path d="M20 7L9 18l-5-5" />
							</svg>
							Optional Docker support
						</span>
						<span class="item">
							<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"
								stroke-linecap="round" aria-hidden="true">
								<path d="M20 7L9 18l-5-5" />
							</svg>
							Bring your own models
						</span>
					</div>
				</div>

				<div class="landing-flock-stage reveal" aria-hidden="true">
					<svg width="0" height="0" class="landing-defs">
						<defs>
							<symbol id="landing-bird" viewBox="0 0 120 80" overflow="visible">
								<path fill="var(--brand)"
									d="M18 44 C 32 36, 54 34, 78 38 C 92 40, 102 44, 110 48 C 100 52, 88 52, 74 50 C 70 54, 64 56, 58 56 C 48 56, 38 52, 30 48 C 24 46, 20 45, 18 44 Z" />
								<path fill="var(--brand-3)" d="M18 44 L 2 38 L 8 46 L 2 54 Z" />
								<g class="wings">
									<path fill="var(--brand-2)"
										d="M38 40 C 46 14, 62 8, 78 10 C 72 22, 62 32, 52 38 C 46 40, 42 41, 38 40 Z" />
									<path fill="var(--brand)"
										d="M52 38 C 60 22, 74 18, 88 20 C 82 30, 74 36, 66 40 C 60 40, 56 40, 52 38 Z" opacity="0.88" />
								</g>
								<circle fill="var(--brand)" cx="98" cy="46" r="6" />
								<path fill="var(--brand-3)" d="M104 46 L 116 44 L 104 50 Z" />
								<circle fill="#fff" cx="100" cy="45" r="1.2" />
							</symbol>
						</defs>
					</svg>
					<div class="sun"></div>
					<div class="bird bird-1"><svg>
							<use href="#landing-bird" />
						</svg></div>
					<div class="bird bird-2"><svg>
							<use href="#landing-bird" />
						</svg></div>
					<div class="bird bird-3"><svg>
							<use href="#landing-bird" />
						</svg></div>
					<div class="horizon"></div>

					<div class="agent-pod pod-1">
						<div class="avatar">A</div>
						<div>
							<div class="name">assistant</div>
							<div class="sub">slack &#183; #eng</div>
						</div>
						<div class="bip"></div>
					</div>
					<div class="agent-pod pod-2">
						<div class="avatar accent-blue">R</div>
						<div>
							<div class="name">researcher</div>
							<div class="sub">signal &#183; daily 8a</div>
						</div>
						<div class="bip"></div>
					</div>
					<div class="agent-pod pod-3">
						<div class="avatar accent-indigo">O</div>
						<div>
							<div class="name">ops-bot</div>
							<div class="sub">discord &#183; on-file</div>
						</div>
						<div class="bip"></div>
					</div>
				</div>
			</div>

		</section>

		<section class="landing-band" id="compare">
			<div class="landing-wrap">
				<div class="landing-eyebrow reveal">Built to run lean</div>
				<h2 class="landing-section-title reveal">
					Lean by default.<br />
					<em>Cheaper on repeat work.</em>
				</h2>
				<p class="landing-section-sub reveal">
					Aviary runs as a single service, reuses shared browser state across
					agents, and can compile repeatable scheduled tasks into scripts so
					they stop paying full model cost every time they fire.
				</p>

				<div class="landing-stats-grid">
					<div class="landing-stat-card reveal">
						<div class="landing-stat-head">
							<h4>Memory footprint</h4>
						</div>
						<div class="bar-row">
							<div class="bar-label">Aviary</div>
							<div class="bar-track">
								<div class="bar-fill us" style="--target: 0.125"></div>
							</div>
							<div class="bar-value pop">128 MB</div>
						</div>
						<div class="bar-row">
							<div class="bar-label them">OpenClaw</div>
							<div class="bar-track">
								<div class="bar-fill them" style="--target: 1"></div>
							</div>
							<div class="bar-value">1 GB</div>
						</div>
						<div class="stat-figure">
							<div class="stat-figure-label">
								Recommended footprint including Slack, Signal, and Discord channel daemons. Lower when no channels are
								configured.
							</div>
						</div>
					</div>

					<div class="landing-stat-card reveal">
						<div class="landing-stat-head">
							<h4>Token usage optimization</h4>
						</div>
						<div class="bar-row">
							<div class="bar-label them">Prompt task</div>
							<div class="bar-track">
								<div class="bar-fill them" style="--target: 1"></div>
							</div>
							<div class="bar-value">~5,000/run</div>
						</div>
						<div class="bar-row">
							<div class="bar-label">Compiled script</div>
							<div class="bar-track">
								<div class="bar-fill us" style="--target: 0.02"></div>
							</div>
							<div class="bar-value pop">~100/run</div>
						</div>
						<div class="stat-figure">
							<div class="stat-figure-label">
								Measured from real Aviary usage data. Simple tasks (URL checks, API polls) average ~5,000 tokens/run.
								Research tasks run higher. Average of 100 is based on minimal overhead from non-deterministic operations
								in compiled scripts.
							</div>
						</div>
					</div>
				</div>
			</div>
		</section>

		<section class="landing-band" id="product">
			<div class="landing-wrap">
				<div class="landing-eyebrow reveal">Control panel</div>
				<h2 class="landing-section-title reveal">
					One tab.<br />
					Every <em>agent</em>, task, and token.
				</h2>
				<p class="landing-section-sub reveal">
					Live status, long-running conversations, and token-level usage data
					without leaving the browser.
				</p>

				<div class="landing-showcase reveal">
					<div class="landing-showcase-head">
						<div class="landing-showcase-tabs" role="tablist">
							<button v-for="(item, index) in showcaseItems" :key="item.title" type="button"
								class="landing-showcase-tab" :class="{ active: showcaseIndex === index }" role="tab"
								:aria-selected="showcaseIndex === index" @click="showcaseIndex = index">
								<span class="num">{{ String(index + 1).padStart(2, "0") }}</span>
								<span>
									<span class="tit">{{ item.title }}</span>
									<span class="sub">{{ item.description }}</span>
								</span>
							</button>
						</div>
						<div class="landing-showcase-panel">
							<div class="landing-showcase-image">
								<img :src="resolveShowcaseSrc(activeShowcase)" :alt="activeShowcase.alt" />
							</div>
							<div class="landing-showcase-copy">
								<h3>{{ activeShowcase.title }}</h3>
								<p>{{ activeShowcase.description }}</p>
								<ul class="bullets">
									<li v-for="bullet in activeShowcase.bullets" :key="bullet">
										<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"
											stroke-linecap="round" aria-hidden="true">
											<path d="M20 7L9 18l-5-5" />
										</svg>
										{{ bullet }}
									</li>
								</ul>
							</div>
						</div>
					</div>
				</div>
			</div>
		</section>

		<section class="landing-band" id="workflows">
			<div class="landing-wrap">
				<div class="landing-eyebrow reveal">Core workflows</div>
				<h2 class="landing-section-title reveal">
					What you <em>actually</em> use Aviary for.
				</h2>
				<p class="landing-section-sub reveal">
					Seven day-to-day jobs the nest handles out of the box.
				</p>

				<div class="landing-bento">
					<div class="card c-wide-3 reveal">
						<div class="kind">Channels &#183; 01</div>
						<h3>Put agents in the chats you already use.</h3>
						<p>
							Run Aviary inside Signal, Slack, or Discord so the work stays in
							the thread instead of moving to another dashboard.
						</p>
						<div class="channels-vis">
							<div class="channels-stage" aria-hidden="true">
								<div class="channels-stage-glow signal"></div>
								<div class="channels-stage-glow slack"></div>
								<div class="channels-stage-glow discord"></div>
								<div class="channel-logo-card signal">
									<div class="channel-logo-mark">
										<svg viewBox="0 0 24 24" role="presentation">
											<path :d="siSignal.path" fill="currentColor" />
										</svg>
									</div>
									<div>
										<strong>Signal</strong>
										<span>Private threads, direct answers.</span>
									</div>
								</div>
								<div class="channel-logo-card slack">
									<div class="channel-logo-mark">
										<svg viewBox="0 0 24 24" role="presentation">
											<path d="M9.2 2a2.2 2.2 0 0 0-2.2 2.2v5.5a2.2 2.2 0 1 0 4.4 0V4.2A2.2 2.2 0 0 0 9.2 2Z"
												fill="#36c5f0" />
											<path d="M20 9.2A2.2 2.2 0 0 0 17.8 7h-5.5a2.2 2.2 0 1 0 0 4.4h5.5A2.2 2.2 0 0 0 20 9.2Z"
												fill="#2eb67d" />
											<path d="M14.8 22a2.2 2.2 0 0 0 2.2-2.2v-5.5a2.2 2.2 0 1 0-4.4 0v5.5a2.2 2.2 0 0 0 2.2 2.2Z"
												fill="#ecb22e" />
											<path d="M4 14.8A2.2 2.2 0 0 0 6.2 17h5.5a2.2 2.2 0 1 0 0-4.4H6.2A2.2 2.2 0 0 0 4 14.8Z"
												fill="#e01e5a" />
											<path d="M12.6 4.2A2.2 2.2 0 1 1 17 4.2a2.2 2.2 0 0 1-4.4 0Z" fill="#36c5f0" />
											<path d="M17.8 12.6A2.2 2.2 0 1 1 17.8 17a2.2 2.2 0 0 1 0-4.4Z" fill="#2eb67d" />
											<path d="M7 19.8A2.2 2.2 0 1 1 11.4 19.8a2.2 2.2 0 0 1-4.4 0Z" fill="#ecb22e" />
											<path d="M4 7A2.2 2.2 0 1 1 4 11.4 2.2 2.2 0 0 1 4 7Z" fill="#e01e5a" />
										</svg>
									</div>
									<div>
										<strong>Slack</strong>
										<span>Channels, mentions, and async loops.</span>
									</div>
								</div>
								<div class="channel-logo-card discord">
									<div class="channel-logo-mark">
										<svg viewBox="0 0 24 24" role="presentation">
											<path :d="siDiscord.path" fill="currentColor" />
										</svg>
									</div>
									<div>
										<strong>Discord</strong>
										<span>Fast ops, support, and live handoffs.</span>
									</div>
								</div>
							</div>
							<span class="channel-chip signal">
								<span class="channel-chip-icon" aria-hidden="true">
									<svg viewBox="0 0 24 24" role="presentation">
										<path :d="siSignal.path" fill="currentColor" />
									</svg>
								</span>
								Signal
							</span>
							<span class="channel-chip slack">
								<span class="channel-chip-icon" aria-hidden="true">
									<svg viewBox="0 0 24 24" role="presentation">
										<path d="M9.2 2a2.2 2.2 0 0 0-2.2 2.2v5.5a2.2 2.2 0 1 0 4.4 0V4.2A2.2 2.2 0 0 0 9.2 2Z"
											fill="#36c5f0" />
										<path d="M20 9.2A2.2 2.2 0 0 0 17.8 7h-5.5a2.2 2.2 0 1 0 0 4.4h5.5A2.2 2.2 0 0 0 20 9.2Z"
											fill="#2eb67d" />
										<path d="M14.8 22a2.2 2.2 0 0 0 2.2-2.2v-5.5a2.2 2.2 0 1 0-4.4 0v5.5a2.2 2.2 0 0 0 2.2 2.2Z"
											fill="#ecb22e" />
										<path d="M4 14.8A2.2 2.2 0 0 0 6.2 17h5.5a2.2 2.2 0 1 0 0-4.4H6.2A2.2 2.2 0 0 0 4 14.8Z"
											fill="#e01e5a" />
										<path d="M12.6 4.2A2.2 2.2 0 1 1 17 4.2a2.2 2.2 0 0 1-4.4 0Z" fill="#36c5f0" />
										<path d="M17.8 12.6A2.2 2.2 0 1 1 17.8 17a2.2 2.2 0 0 1 0-4.4Z" fill="#2eb67d" />
										<path d="M7 19.8A2.2 2.2 0 1 1 11.4 19.8a2.2 2.2 0 0 1-4.4 0Z" fill="#ecb22e" />
										<path d="M4 7A2.2 2.2 0 1 1 4 11.4 2.2 2.2 0 0 1 4 7Z" fill="#e01e5a" />
									</svg>
								</span>
								Slack
							</span>
							<span class="channel-chip discord">
								<span class="channel-chip-icon" aria-hidden="true">
									<svg viewBox="0 0 24 24" role="presentation">
										<path :d="siDiscord.path" fill="currentColor" />
									</svg>
								</span>
								Discord
							</span>
						</div>
					</div>

					<div class="card c-wide-3 reveal">
						<div class="kind">Scheduled tasks &#183; 02</div>
						<h3>Run on a timer. Stop paying for it.</h3>
						<p>
							Set a task on a cron schedule or a file watch. With
							<code>precompute_tasks</code> enabled, Aviary can compile repeat
							work ahead of time instead of rebuilding it at run time.
						</p>
						<div class="sched-vis">
							<div class="sched-row">
								<span class="nm">morning-standup</span><span>0 9 * * 1-5</span><span class="st ok">schedule</span>
							</div>
							<div class="sched-row">
								<span class="nm">import-csv</span><span>./inbox/*.csv</span><span class="st ok">watch</span>
							</div>
							<div class="sched-row">
								<span class="nm">aviary task list</span><span>task status</span><span class="st">cli</span>
							</div>
							<div class="sched-row">
								<span class="nm">aviary task run morning-standup</span><span>manual trigger</span><span
									class="st ok">cli</span>
							</div>
						</div>
					</div>

					<div class="card c-wide-3 reveal">
						<div class="kind">Browser &#183; 03</div>
						<h3>A browser they can share.</h3>
						<p>CDP-backed browser control from the CLI and MCP tools.</p>
						<div class="browser-vis">
							<div class="browser-bar">
								<div class="browser-tab">
									<span class="browser-tab-mark">A</span>
									<span class="browser-tab-title">docs.stripe.com</span>
								</div>
								<div class="browser-address">
									<span class="browser-address-scheme">https</span>
									<span class="browser-address-url">docs.stripe.com</span>
								</div>
							</div>
							<div class="browser-body">
								<div class="line"><span class="step">1</span>aviary browser tabs<span class="ok">tab id</span></div>
								<div class="line"><span class="step">2</span>aviary browser click --selector .search<span
										class="ok">click</span></div>
								<div class="line"><span class="step">3</span>aviary browser type webhooks<span
										class="blink-cursor"></span>
								</div>
							</div>
						</div>
					</div>

					<div class="card c-wide-3 reveal">
						<div class="kind">Skills &#183; 04</div>
						<h3>Skills, bundled and downloadable.</h3>
						<p>
							Built-ins toggle in <code>aviary.yaml</code>. Aviary also detects
							disk-installed skills from
							<a href="https://skills.sh/" target="_blank" rel="noreferrer">skills.sh</a>
							and local installs automatically.
						</p>
						<div class="skills-vis">
							<span class="skill-pill on">gogcli</span>
							<span class="skill-pill on">himalaya</span>
							<span class="skill-pill">skill_gogcli</span>
							<span class="skill-pill">skill_himalaya</span>
							<span class="skill-pill on">skills_list</span>
							<span class="skill-pill">web_search</span>
						</div>
						<p class="skills-note">
							Downloadable picks like <code>gogcli</code> and <code>himalaya</code>
							sit next to built-ins once they are installed globally.
						</p>
					</div>

					<div class="card c-wide-4 reveal">
						<div class="kind">Security &#183; 05</div>
						<h3>Locked down by default.</h3>
						<p>
							Every agent starts with a low-privilege preset. Tools are
							allowed explicitly; filesystem paths and shell commands are
							scoped. If it is not on the list, it does not run.
						</p>
						<div class="security-grid">
							<div class="security-box">
								<div class="label">permissions.tools</div>
								<div>agent_run &#183; file_read</div>
							</div>
							<div class="security-box">
								<div class="label">filesystem.allowed_paths</div>
								<div>./workspace/**</div>
							</div>
							<div class="security-box">
								<div class="label">exec.allowed_commands</div>
								<div>git * &#183; !git push *</div>
							</div>
						</div>
					</div>

					<div class="card c-wide-2 reveal">
						<div class="kind">CLI &#183; 06</div>
						<h3>Keyboard-first, when you want it.</h3>
						<p>Every UI action has a terminal command.</p>
						<div class="cli-snippet">
							<div><span class="prompt">$</span> aviary agent run assistant "Summarize today's inbox"</div>
							<div class="muted">-&gt; streams the agent response</div>
							<div><span class="prompt">$</span> aviary job list --task morning-standup</div>
							<div><span class="prompt">$</span> aviary logs --follow<span class="blink-cursor"></span></div>
						</div>
					</div>
				</div>
			</div>
		</section>

		<section class="landing-band">
			<div class="landing-wrap">
				<div class="landing-config-row">
					<div>
						<div class="landing-eyebrow reveal">Config in one file</div>
						<h2 class="landing-section-title reveal">
							Readable config.<br />
							No <em>hand-holding.</em>
						</h2>
						<p class="landing-section-sub reveal">
							One <code>aviary.yaml</code> describes your agents, tasks,
							permissions, models, and enabled skills. Edit it in an editor or
							in the dashboard and they stay in sync.
						</p>
						<a class="landing-btn landing-btn-ghost reveal" :href="withBase('/guide/configuration')">
							Jump to configuration docs
							<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"
								aria-hidden="true">
								<path d="M5 12h14M13 5l7 7-7 7" />
							</svg>
						</a>
					</div>

					<div class="landing-terminal reveal">
						<div class="landing-terminal-bar">
							<span class="tl"><span></span><span></span><span></span></span>
							<span>~/.config/aviary/aviary.yaml</span>
						</div>
						<div class="landing-terminal-body">
							<pre>{{ configSnippet }}</pre>
						</div>
					</div>
				</div>
			</div>
		</section>

		<section class="landing-band" id="docs">
			<div class="landing-wrap">
				<div class="landing-eyebrow reveal">Read this first</div>
				<h2 class="landing-section-title reveal">
					Find your <em>way</em> around.
				</h2>
				<p class="landing-section-sub reveal">
					The shortest path from fresh install to a scheduled agent shipping
					work every morning.
				</p>

				<div class="landing-docs-grid">
					<a v-for="item in docsLinks" :key="item.title" class="landing-doc-card reveal" :href="withBase(item.href)">
						<div class="d-num">{{ item.number }}</div>
						<h3>{{ item.title }}</h3>
						<p>{{ item.description }}</p>
						<span class="arrow">
							<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"
								aria-hidden="true">
								<path d="M5 12h14M13 5l7 7-7 7" />
							</svg>
						</span>
					</a>
				</div>

				<div class="landing-final-cta reveal">
					<div class="mini-flock" aria-hidden="true">
						<div class="bird"><svg>
								<use href="#landing-bird" />
							</svg></div>
					</div>
					<h2>Ready to build your nest?</h2>
					<p>
						Run one command. In under a minute, your first agent is online,
						reachable from Slack, Signal, or Discord, with a dashboard waiting
						in your browser.
					</p>
					<div class="row">
						<LandingInstallBlock minimal />
						<a class="landing-btn landing-btn-primary" href="https://github.com/lsegal/aviary" target="_blank"
							rel="noreferrer">
							<svg width="14" height="14" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
								<path d="M12 2l3.09 6.26L22 9.27l-5 4.87L18.18 22 12 18.77 5.82 22 7 14.14 2 9.27l6.91-1.01L12 2z" />
							</svg>
							Star on GitHub
						</a>
					</div>
				</div>
			</div>
		</section>

		<footer class="landing-footer">
			<div class="landing-wrap landing-footer-row">
				<div>
					<div class="landing-footer-brand">
						<span class="mark" aria-hidden="true">
							<svg viewBox="0 0 40 40">
								<g fill="var(--brand)">
									<path d="M7 24 C 12 18, 18 15, 22 21 C 18 20, 12 22, 7 24 Z" />
									<path d="M22 21 C 26 15, 33 18, 37 24 C 32 23, 27 22, 22 21 Z" />
									<path d="M12 26 C 18 24, 26 27, 32 25 C 29 31, 23 34, 17 31 C 14 30, 12 28, 12 26 Z" />
								</g>
							</svg>
						</span>
						<span class="name">Aviary</span>
					</div>
					<div class="landing-footer-copy">Copyright 2026 Loren Segal · MIT License</div>
				</div>
				<div class="landing-footer-links">
					<a :href="withBase('/guide/')">Guide</a>
					<a :href="withBase('/reference/')">Reference</a>
					<a href="https://github.com/lsegal/aviary/releases" target="_blank" rel="noreferrer">Changelog</a>
					<a href="https://github.com/lsegal/aviary" target="_blank" rel="noreferrer">GitHub</a>
				</div>
			</div>
		</footer>
	</div>
</template>

<style scoped>
.landing-home {
	color: var(--ink);
}

.landing-wrap {
	width: min(1200px, calc(100% - 48px));
	margin: 0 auto;
}

.landing-hero {
	position: relative;
	padding: 72px 0 40px;
	overflow: hidden;
}

.landing-hero::before {
	content: "";
	position: absolute;
	inset: -20% -10% auto;
	height: 80%;
	background:
		radial-gradient(600px 300px at 30% 30%, rgba(214, 87, 31, 0.18), transparent 70%),
		radial-gradient(500px 260px at 80% 10%, rgba(198, 140, 79, 0.2), transparent 70%);
	pointer-events: none;
}

.landing-hero-inner {
	position: relative;
	z-index: 1;
	display: grid;
	grid-template-columns: minmax(0, 1.15fr) minmax(0, 1fr);
	gap: 48px;
	align-items: center;
}

.landing-hero-copy {
	min-width: 0;
}

.landing-hero-eyebrow,
.landing-eyebrow {
	display: inline-flex;
	align-items: center;
	gap: 10px;
	margin-bottom: 24px;
	color: var(--ink-2);
}

.landing-hero-eyebrow {
	padding: 6px 12px;
	border: 1px solid var(--rule-strong);
	border-radius: 999px;
	background: color-mix(in oklab, var(--paper) 60%, transparent);
	font-size: 12px;
	font-weight: 600;
	letter-spacing: 0.02em;
}

.landing-hero-eyebrow svg {
	color: var(--brand);
}

.landing-hero-eyebrow .sep {
	opacity: 0.4;
}

.landing-hero-title,
.landing-section-title,
.landing-showcase-copy h3,
.landing-final-cta h2 {
	font-family: var(--serif);
	font-variation-settings: "opsz" 72;
}

.landing-hero-title {
	margin: 0 0 24px;
	font-size: clamp(44px, 6.4vw, 84px);
	font-weight: 400;
	line-height: 0.98;
	letter-spacing: -0.03em;
}

.landing-hero-title .accent,
.landing-section-title em {
	background: linear-gradient(135deg, var(--brand-2), var(--brand-3) 60%, var(--ember));
	-webkit-background-clip: text;
	background-clip: text;
	color: transparent;
	font-style: italic;
}

.landing-section-title em {
	display: inline-block;
	padding-inline-end: 0.08em;
	padding-block-end: 0.08em;
}

.landing-hero-lede,
.landing-section-sub {
	margin: 0 0 32px;
	color: var(--ink-2);
	line-height: 1.6;
	text-wrap: pretty;
}

.landing-hero-lede {
	max-width: 540px;
	font-size: 18px;
}

.landing-hero-cta,
.landing-final-cta .row {
	display: flex;
	flex-wrap: wrap;
	gap: 12px;
	align-items: center;
	margin-top: 18px;
}

.landing-btn {
	display: inline-flex;
	align-items: center;
	gap: 8px;
	padding: 9px 16px;
	border: 1px solid transparent;
	border-radius: 999px;
	font-size: 14px;
	font-weight: 600;
	text-decoration: none;
	transition:
		transform 0.15s ease,
		background-color 0.15s ease,
		border-color 0.15s ease,
		color 0.15s ease;
}

.landing-btn-primary {
	background: linear-gradient(180deg, var(--brand-2), var(--brand));
	box-shadow:
		inset 0 1px 0 rgba(255, 255, 255, 0.25),
		0 6px 14px -4px rgba(184, 58, 29, 0.45);
	color: #fff2e4;
}

.landing-btn-primary:hover,
.landing-btn-primary:focus-visible,
.landing-btn-primary:visited {
	color: #fff2e4;
	text-decoration: none;
}

.landing-btn-primary:hover {
	transform: translateY(-1px);
	background: linear-gradient(180deg, var(--brand-2), var(--brand));
}

.landing-btn-ghost {
	border-color: var(--rule-strong);
	background: transparent;
	color: var(--ink);
}

.landing-btn-ghost:hover {
	background: var(--paper);
}

.landing-hero-meta {
	display: flex;
	flex-wrap: wrap;
	gap: 16px;
	margin-top: 28px;
	color: var(--ink-3);
	font-size: 13px;
}

.landing-hero-meta .item {
	display: inline-flex;
	align-items: center;
	gap: 6px;
}

.landing-hero-meta svg {
	color: var(--ink-2);
}

.landing-defs {
	position: absolute;
}

.landing-flock-stage {
	position: relative;
	aspect-ratio: 1.05 / 1;
	border: 1px solid var(--rule-strong);
	border-radius: var(--radius-lg);
	background:
		radial-gradient(circle at 30% 30%, rgba(255, 240, 220, 0.5), transparent 60%),
		linear-gradient(160deg, #ffe9cd, #f7c9a0 50%, #e09563);
	box-shadow: var(--shadow-lg);
	overflow: hidden;
}

.landing-flock-stage .sun {
	position: absolute;
	top: 18%;
	left: 62%;
	width: 160px;
	height: 160px;
	border-radius: 999px;
	background: radial-gradient(circle, rgba(255, 220, 170, 0.9), rgba(255, 200, 130, 0.35) 60%, transparent 72%);
}

.landing-flock-stage .horizon {
	position: absolute;
	right: 0;
	bottom: 0;
	left: 0;
	height: 30%;
	background: linear-gradient(180deg, transparent, rgba(150, 60, 20, 0.25));
}

.bird {
	position: absolute;
	width: 96px;
	height: 64px;
	transform-origin: center;
	filter: drop-shadow(0 4px 10px rgba(130, 30, 10, 0.35));
}

.bird svg {
	width: 100%;
	height: 100%;
	overflow: visible;
}

.bird .wings {
	transform-origin: center 60%;
	animation: flap 0.5s ease-in-out infinite;
}

.bird-1 {
	top: 20%;
	left: 8%;
	animation: drift-1 14s ease-in-out infinite;
}

.bird-2 {
	top: 40%;
	left: 28%;
	width: 76px;
	height: 50px;
	animation: drift-2 16s ease-in-out infinite 0.3s;
}

.bird-3 {
	top: 30%;
	left: 50%;
	width: 60px;
	height: 40px;
	opacity: 0.9;
	animation: drift-3 18s ease-in-out infinite 0.6s;
}

.agent-pod {
	position: absolute;
	display: flex;
	align-items: center;
	gap: 10px;
	min-width: 180px;
	padding: 10px 12px;
	border: 1px solid var(--rule);
	border-radius: 14px;
	background: color-mix(in oklab, var(--paper) 96%, transparent);
	box-shadow: 0 10px 28px -12px rgba(93, 53, 31, 0.4);
	font-size: 12px;
	animation: float-pod 6s ease-in-out infinite;
}

.agent-pod .avatar {
	display: grid;
	place-items: center;
	width: 28px;
	height: 28px;
	border-radius: 8px;
	background: var(--brand-soft);
	color: var(--brand);
	font-family: var(--mono);
	font-size: 12px;
	font-weight: 600;
}

.agent-pod .avatar.accent-blue {
	background: rgba(58, 118, 240, 0.16);
	color: #3a76f0;
}

.agent-pod .avatar.accent-indigo {
	background: rgba(88, 101, 242, 0.16);
	color: #5865f2;
}

.agent-pod .name {
	color: var(--ink);
	font-weight: 600;
}

.agent-pod .sub {
	color: var(--ink-3);
	font-family: var(--mono);
	font-size: 10.5px;
}

.agent-pod .bip {
	margin-left: auto;
	width: 8px;
	height: 8px;
	border-radius: 999px;
	background: #2eb67d;
	box-shadow: 0 0 0 3px rgba(46, 182, 125, 0.18);
}

.pod-1 {
	top: 12%;
	right: 6%;
}

.pod-2 {
	top: 48%;
	left: 6%;
}

.pod-3 {
	right: 12%;
	bottom: 10%;
}

.landing-band {
	position: relative;
	padding: 120px 0 40px;
}

.landing-eyebrow {
	color: var(--brand);
	font-family: var(--mono);
	font-size: 12px;
	font-weight: 500;
	letter-spacing: 0.12em;
	text-transform: uppercase;
}

.landing-eyebrow::before {
	content: "";
	display: inline-block;
	width: 16px;
	height: 1px;
	background: currentColor;
}

.landing-section-title {
	max-width: 22ch;
	margin: 0 0 16px;
	font-size: clamp(32px, 4.2vw, 52px);
	font-weight: 400;
	line-height: 1.04;
	letter-spacing: -0.02em;
}

.landing-section-sub {
	margin-bottom: 48px;
	font-size: 17px;
}

.landing-stats-grid {
	display: grid;
	grid-template-columns: repeat(2, 1fr);
	gap: 20px;
}

.landing-stat-card,
.landing-showcase,
.landing-bento .card,
.landing-doc-card,
.landing-final-cta {
	border: 1px solid var(--rule);
	background: var(--paper);
	box-shadow: var(--shadow-sm);
}

.landing-stat-card {
	overflow: hidden;
	padding: 24px;
	border-radius: var(--radius-md);
}

.landing-stat-head {
	display: flex;
	align-items: center;
	justify-content: space-between;
	margin-bottom: 20px;
	padding-bottom: 12px;
	border-bottom: 1px solid var(--rule);
}

.landing-stat-head h4 {
	margin: 0;
	color: var(--brand);
	font-family: var(--mono);
	font-size: 12px;
	font-weight: 500;
	letter-spacing: 0.12em;
	text-transform: uppercase;
}

.stat-figure {
	margin-top: 18px;
}

.stat-figure-value {
	color: var(--ink);
	font-family: var(--mono);
	font-size: clamp(28px, 4vw, 40px);
	font-weight: 600;
	letter-spacing: -0.04em;
	line-height: 1;
}

.stat-figure-label {
	margin-top: 8px;
	color: var(--ink-3);
	font-size: 13px;
	line-height: 2;
}

.bar-row {
	display: grid;
	grid-template-columns: 128px 1fr 100px;
	align-items: center;
	gap: 12px;
	margin: 12px 0;
}

.bar-label {
	color: var(--ink);
	font-size: 13px;
	font-weight: 600;
	white-space: nowrap;
}

.bar-label.them {
	color: var(--ink-2);
	font-weight: 500;
}

.bar-track {
	height: 32px;
	overflow: hidden;
}

.bar-fill {
	height: 100%;
	width: 0;
	max-width: 100%;
	border-radius: 8px;
	transition: width 900ms cubic-bezier(0.2, 0.8, 0.2, 1);
}

.landing-stat-card.in .bar-fill {
	width: calc(var(--target) * 100%);
}

.bar-fill.us {
	background: linear-gradient(90deg, var(--brand), var(--brand-2));
	box-shadow: 0 4px 14px -4px rgba(184, 58, 29, 0.45);
}

.bar-fill.them {
	background: color-mix(in oklab, var(--ink-3) 30%, transparent);
}

.bar-value {
	color: var(--ink);
	font-family: var(--mono);
	font-size: 12.5px;
	font-weight: 600;
	text-align: right;
}

.bar-value.pop {
	color: var(--brand);
}

.stat-figure-label strong,
.bar-note strong {
	color: var(--brand);
}

.landing-showcase {
	overflow: hidden;
	border-radius: var(--radius-lg);
	box-shadow: var(--shadow-md);
}

.landing-showcase-head {
	display: grid;
	grid-template-columns: 260px 1fr;
	border-bottom: 1px solid var(--rule);
}

.landing-showcase-tabs {
	padding: 20px;
	border-right: 1px solid var(--rule);
}

.landing-showcase-tab {
	display: flex;
	align-items: flex-start;
	gap: 12px;
	width: 100%;
	margin-bottom: 2px;
	padding: 12px;
	border: 0;
	border-radius: 12px;
	background: transparent;
	color: var(--ink-2);
	text-align: left;
	cursor: pointer;
	font: inherit;
	transition:
		background-color 0.15s ease,
		color 0.15s ease;
}

.landing-showcase-tab:hover {
	background: color-mix(in oklab, var(--bg-soft) 60%, transparent);
	color: var(--ink);
}

.landing-showcase-tab.active {
	background: color-mix(in oklab, var(--brand-soft) 80%, var(--paper));
	color: var(--ink);
}

.landing-showcase-tab .num {
	min-width: 22px;
	padding-top: 2px;
	color: var(--ink-3);
	font-family: var(--mono);
	font-size: 11px;
}

.landing-showcase-tab.active .num {
	color: var(--brand);
}

.landing-showcase-tab .tit {
	display: block;
	font-size: 14.5px;
	font-weight: 600;
	line-height: 1.3;
}

.landing-showcase-tab .sub {
	display: block;
	margin-top: 4px;
	color: var(--ink-3);
	font-size: 12.5px;
	line-height: 1.45;
	display: -webkit-box;
	overflow: hidden;
	line-clamp: 2;
	-webkit-line-clamp: 2;
	-webkit-box-orient: vertical;
}

.landing-showcase-panel {
	display: grid;
	grid-template-columns: 1.3fr 1fr;
	gap: 24px;
	align-items: center;
	padding: 24px;
}

.landing-showcase-image {
	position: relative;
	aspect-ratio: 16 / 14;
	overflow: hidden;
	border: 1px solid var(--rule);
	border-radius: 16px;
	background: var(--bg-soft);
	box-shadow: 0 20px 40px -20px rgba(93, 53, 31, 0.35);
}

/* Gradient overlay removed */

.landing-showcase-image .dots {
	position: absolute;
	top: 12px;
	left: 14px;
	z-index: 3;
	display: flex;
	gap: 6px;
}

.landing-showcase-image .dots span {
	width: 10px;
	height: 10px;
	border-radius: 999px;
}

.landing-showcase-image .dots span:nth-child(1) {
	background: #ec6a5f;
}

.landing-showcase-image .dots span:nth-child(2) {
	background: #f5bd4f;
}

.landing-showcase-image .dots span:nth-child(3) {
	background: #62c554;
}

.landing-showcase-image img {
	width: 100%;
	height: 100%;
	object-fit: cover;
	object-position: top left;
}

.landing-showcase-copy h3 {
	margin: 0 0 10px;
	font-size: 28px;
	font-weight: 500;
}

.landing-showcase-copy p {
	margin: 0 0 20px;
	color: var(--ink-2);
	font-size: 15.5px;
	line-height: 1.6;
}

.landing-showcase-copy .bullets {
	margin: 0;
	padding: 0;
	list-style: none;
}

.landing-showcase-copy .bullets li {
	display: flex;
	align-items: flex-start;
	gap: 10px;
	padding: 10px 0;
	border-top: 1px dashed var(--rule);
	font-size: 14px;
}

.landing-showcase-copy .bullets li:first-child {
	border-top: 0;
}

.landing-showcase-copy .bullets svg {
	margin-top: 2px;
	color: var(--brand);
	flex: none;
}

.landing-bento {
	display: grid;
	grid-template-columns: repeat(6, 1fr);
	grid-auto-rows: minmax(180px, auto);
	gap: 16px;
}

.landing-bento .card {
	display: flex;
	flex-direction: column;
	gap: 10px;
	padding: 22px;
	border-radius: var(--radius-md);
	overflow: hidden;
	transition:
		transform 0.2s ease,
		box-shadow 0.2s ease,
		border-color 0.2s ease;
}

.landing-bento .card:hover,
.landing-doc-card:hover {
	transform: translateY(-2px);
	box-shadow: var(--shadow-md);
	border-color: var(--rule);
}

.landing-bento .card h3 {
	margin: 0;
	font-size: 17px;
	font-weight: 600;
	letter-spacing: -0.01em;
}

.landing-bento .card p {
	margin: 0;
	color: var(--ink-2);
	font-size: 14px;
	line-height: 1.55;
}

.landing-bento .card p a {
	color: var(--brand);
	text-decoration: underline;
	text-decoration-color: color-mix(in oklab, var(--brand) 45%, transparent);
	text-underline-offset: 0.18em;
}

.landing-bento .card p a:hover {
	color: var(--brand-2);
}

.landing-bento .kind {
	color: var(--ink-3);
	font-family: var(--mono);
	font-size: 10.5px;
	letter-spacing: 0.12em;
	text-transform: uppercase;
}

.c-wide-2 {
	grid-column: span 2;
}

.c-wide-3 {
	grid-column: span 3;
}

.c-wide-4 {
	grid-column: span 4;
}


.channels-vis,
.skills-vis {
	display: flex;
	flex-wrap: wrap;
	gap: 8px;
	margin-top: auto;
}

.channels-vis {
	align-items: flex-end;
}

.channels-stage {
	position: relative;
	flex: 1 1 100%;
	min-height: 172px;
	padding: 18px;
	border: 1px solid var(--rule);
	border-radius: 18px;
	overflow: hidden;
	background:
		radial-gradient(circle at 18% 24%, rgba(58, 118, 240, 0.16), transparent 26%),
		radial-gradient(circle at 50% 82%, rgba(74, 21, 75, 0.16), transparent 28%),
		radial-gradient(circle at 84% 24%, rgba(88, 101, 242, 0.18), transparent 30%),
		linear-gradient(145deg, color-mix(in oklab, var(--paper) 84%, white 16%), color-mix(in oklab, var(--bg-soft) 84%, transparent));
	box-shadow: inset 0 1px 0 rgba(255, 255, 255, 0.55);
	display: grid;
	align-items: end;
}

.channels-stage-glow {
	position: absolute;
	width: 180px;
	height: 180px;
	border-radius: 999px;
	filter: blur(12px);
	opacity: 0.85;
	pointer-events: none;
	transform: translateZ(0);
}

.channels-stage-glow.signal {
	top: -70px;
	left: -28px;
	background: radial-gradient(circle, rgba(58, 118, 240, 0.22), transparent 70%);
}

.channels-stage-glow.slack {
	bottom: -88px;
	left: 28%;
	background: radial-gradient(circle, rgba(74, 21, 75, 0.18), transparent 70%);
}

.channels-stage-glow.discord {
	top: -62px;
	right: -18px;
	background: radial-gradient(circle, rgba(88, 101, 242, 0.22), transparent 70%);
}

.channel-logo-card {
	position: absolute;
	display: flex;
	align-items: center;
	gap: 12px;
	min-width: 190px;
	max-width: 220px;
	padding: 12px 14px;
	border: 1px solid color-mix(in oklab, var(--rule-strong) 70%, transparent);
	border-radius: 18px;
	background: rgba(255, 250, 240, 0.84);
	backdrop-filter: blur(10px);
	-webkit-backdrop-filter: blur(10px);
	box-shadow: var(--shadow-md);
	color: var(--ink);
	transform: rotate(-2deg);
}

.dark .channel-logo-card {
	background: rgba(29, 19, 17, 0.82);
}

.channel-logo-card.signal {
	top: 16px;
	left: 16px;
	color: #3a76f0;
	transform: rotate(-5deg);
}

.channel-logo-card.slack {
	bottom: 18px;
	left: 26%;
	color: #4a154b;
	transform: rotate(2deg);
	z-index: 2;
}

.channel-logo-card.discord {
	top: 22px;
	right: 18px;
	color: #5865f2;
	transform: rotate(5deg);
}

.channel-logo-card strong,
.channel-logo-card span {
	display: block;
}

.channel-logo-card strong {
	font-size: 13px;
	line-height: 1.1;
	color: currentColor;
}

.channel-logo-card span {
	margin-top: 3px;
	font-size: 11.5px;
	line-height: 1.35;
	color: var(--ink-2);
}

.channel-logo-mark {
	flex: none;
	display: grid;
	place-items: center;
	width: 42px;
	height: 42px;
	border-radius: 14px;
	background: color-mix(in oklab, currentColor 12%, white 88%);
	box-shadow: inset 0 1px 0 rgba(255, 255, 255, 0.72);
}

.dark .channel-logo-mark {
	background: color-mix(in oklab, currentColor 18%, var(--paper) 82%);
}

.channel-logo-mark svg {
	width: 24px;
	height: 24px;
	display: block;
}

.channel-chip,
.skill-pill {
	border: 1px solid var(--rule);
	border-radius: 999px;
	background: color-mix(in oklab, var(--paper) 70%, var(--bg-soft) 30%);
}

.channel-chip {
	display: inline-flex;
	align-items: center;
	gap: 8px;
	padding: 8px 12px;
	font-size: 12.5px;
	font-weight: 600;
}

.channel-chip-icon {
	display: inline-flex;
	align-items: center;
	justify-content: center;
	width: 14px;
	height: 14px;
	flex: none;
}

.channel-chip-icon svg {
	width: 100%;
	height: 100%;
	display: block;
}

.channel-chip.signal {
	color: #3a76f0;
}

.channel-chip.slack {
	color: #4a154b;
}

.channel-chip.discord {
	color: #5865f2;
}

.channel-chip.muted {
	color: var(--ink-3);
}

.sched-vis,
.cli-snippet {
	margin-top: auto;
	padding: 12px;
	border: 1px solid var(--rule);
	border-radius: 12px;
	background: color-mix(in oklab, var(--bg-soft) 80%, transparent);
	font-family: var(--mono);
	font-size: 12px;
	color: var(--ink-2);
}

.sched-row {
	display: grid;
	grid-template-columns: minmax(0, 1.7fr) minmax(0, 1fr) auto;
	align-items: center;
	padding: 4px 0;
	gap: 12px;
}

.sched-row .nm {
	min-width: 0;
	color: var(--ink);
}

.sched-row span:nth-child(2) {
	min-width: 0;
	text-align: right;
}

.sched-row .st {
	justify-self: end;
	color: var(--brand);
}

.sched-row .st.ok {
	color: #2f9c6c;
}

.browser-vis {
	margin-top: auto;
	overflow: hidden;
	border: 1px solid var(--rule);
	border-radius: 10px;
	background: var(--paper);
}

.browser-bar {
	display: flex;
	align-items: center;
	gap: 8px;
	padding: 8px;
	border-bottom: 1px solid var(--rule);
	background: color-mix(in oklab, var(--bg-soft) 80%, transparent);
	color: var(--ink-2);
	font-size: 10px;
	line-height: 1;
}

.browser-tab {
	display: flex;
	align-items: center;
	gap: 6px;
	min-width: 0;
	padding: 6px 10px;
	border: 1px solid var(--rule);
	border-radius: 10px;
	background: color-mix(in oklab, var(--paper) 88%, transparent);
	box-shadow: inset 0 1px 0 rgba(255, 255, 255, 0.45);
}

.browser-tab-mark {
	display: inline-grid;
	place-items: center;
	width: 14px;
	height: 14px;
	border-radius: 4px;
	background: var(--brand-soft);
	color: var(--brand);
	font-family: var(--mono);
	font-size: 9px;
	font-weight: 600;
}

.browser-tab-title {
	min-width: 0;
	overflow: hidden;
	text-overflow: ellipsis;
	white-space: nowrap;
	color: var(--ink);
	font-size: 10px;
	font-weight: 500;
}

.browser-address {
	display: flex;
	align-items: center;
	gap: 6px;
	min-width: 0;
	flex: 1;
	height: 28px;
	padding: 0 10px;
	border: 1px solid var(--rule);
	border-radius: 999px;
	background: color-mix(in oklab, var(--bg) 84%, transparent);
}

.browser-address-scheme {
	flex: 0 0 auto;
	padding: 2px 5px;
	border-radius: 999px;
	background: var(--brand-soft);
	color: var(--brand);
	font-family: var(--mono);
	font-size: 8.5px;
	font-weight: 600;
	letter-spacing: 0.02em;
	text-transform: uppercase;
	line-height: 1.1;
}

.browser-address-url {
	min-width: 0;
	overflow: hidden;
	text-overflow: ellipsis;
	white-space: nowrap;
	color: var(--ink-2);
	font-family: var(--mono);
	font-size: 10px;
}

.browser-body {
	display: flex;
	flex-direction: column;
	gap: 4px;
	padding: 10px;
	color: var(--ink-2);
	font-family: var(--mono);
	font-size: 10.5px;
}

.browser-body .line {
	display: flex;
	align-items: center;
	gap: 10px;
	min-width: 0;
	overflow: hidden;
	white-space: nowrap;
}

.browser-body .step,
.cli-snippet .prompt {
	color: var(--brand);
	font-weight: 600;
}

.browser-body .ok {
	margin-left: auto;
	padding-left: 8px;
	color: #2f9c6c;
	font-size: 9.5px;
}

.skill-pill {
	padding: 4px 10px;
	color: var(--ink-2);
	font-family: var(--mono);
	font-size: 11px;
}

.skill-pill.on {
	border-color: color-mix(in oklab, var(--brand) 40%, transparent);
	background: var(--brand-soft);
	color: var(--brand);
}

.skills-note {
	margin-top: auto;
	padding-top: 6px;
	color: var(--ink-3);
	font-size: 12.5px;
	line-height: 1.5;
}

.security-grid {
	display: grid;
	grid-template-columns: repeat(3, 1fr);
	gap: 10px;
	margin-top: auto;
}

.security-box {
	padding: 10px;
	border: 1px solid var(--rule);
	border-radius: 10px;
	font-family: var(--mono);
	font-size: 11.5px;
}

.security-box .label {
	margin-bottom: 4px;
	color: var(--ink-3);
}

.cli-snippet .muted {
	color: var(--ink-3);
}

.blink-cursor {
	display: inline-block;
	width: 6px;
	height: 1em;
	margin-left: 2px;
	background: currentColor;
	vertical-align: -2px;
	animation: blink 1s step-end infinite;
}

.landing-config-row {
	display: grid;
	grid-template-columns: 1fr 1.05fr;
	gap: 48px;
	align-items: center;
}

.landing-section-sub code {
	padding: 2px 6px;
	border-radius: 6px;
	background: var(--brand-soft);
	color: var(--brand);
	font-family: var(--mono);
	font-size: 0.95em;
}

.landing-bento .card p code {
	padding: 1px 5px;
	border-radius: 6px;
	background: var(--brand-soft);
	color: var(--brand);
	font-family: var(--mono);
	font-size: 0.9em;
}

.landing-terminal {
	overflow: hidden;
	border: 1px solid #2a1612;
	border-radius: var(--radius-md);
	background: #140b09;
	box-shadow: var(--shadow-md);
	color: #ffe1c5;
	font-family: var(--mono);
	font-size: 13px;
}

.landing-terminal-bar {
	display: flex;
	align-items: center;
	gap: 8px;
	padding: 10px 14px;
	border-bottom: 1px solid #2a1612;
	background: #1d100d;
	color: #a08476;
	font-size: 12px;
}

.landing-terminal-bar .tl {
	display: flex;
	gap: 6px;
}

.landing-terminal-bar .tl span {
	width: 10px;
	height: 10px;
	border-radius: 999px;
	background: #4a3a33;
}

.landing-terminal-body {
	padding: 18px 18px 22px;
	line-height: 1.65;
}

.landing-terminal-body pre {
	margin: 0;
	white-space: pre-wrap;
	font-family: inherit;
}

.landing-terminal-body .cmt {
	color: #a98063;
}

.landing-terminal-body .k {
	color: #e9b567;
}

.landing-terminal-body .p {
	color: #e06430;
}

.landing-terminal-body .s {
	color: #86d0a5;
}

.landing-terminal-body .warn {
	color: #f2c94c;
}

.landing-docs-grid {
	display: grid;
	grid-template-columns: repeat(4, 1fr);
	gap: 12px;
}

.landing-doc-card {
	position: relative;
	display: block;
	padding: 22px;
	border-radius: 16px;
	color: inherit;
	text-decoration: none;
	transition:
		transform 0.2s ease,
		border-color 0.2s ease,
		box-shadow 0.2s ease;
}

.landing-doc-card .d-num {
	margin-bottom: 10px;
	color: var(--ink-3);
	font-family: var(--mono);
	font-size: 11px;
	letter-spacing: 0.1em;
}

.landing-doc-card h3 {
	margin: 0 0 6px;
	font-size: 17px;
	font-weight: 600;
}

.landing-doc-card p {
	margin: 0;
	color: var(--ink-2);
	font-size: 13.5px;
	line-height: 1.55;
}

.landing-doc-card .arrow {
	position: absolute;
	top: 22px;
	right: 22px;
	display: grid;
	place-items: center;
	width: 28px;
	height: 28px;
	border: 1px solid var(--rule);
	border-radius: 999px;
	color: var(--ink-3);
	transition:
		transform 0.2s ease,
		border-color 0.2s ease,
		color 0.2s ease;
}

.landing-doc-card:hover .arrow {
	transform: translate(2px, -2px);
	border-color: var(--brand);
	color: var(--brand);
}

.landing-final-cta {
	position: relative;
	overflow: hidden;
	margin: 60px 0 80px;
	padding: 56px 40px;
	border: 1px solid var(--rule-strong);
	border-radius: var(--radius-lg);
	background:
		radial-gradient(600px 240px at 80% 10%, rgba(214, 87, 31, 0.22), transparent 70%),
		radial-gradient(500px 220px at 15% 90%, rgba(198, 140, 79, 0.24), transparent 70%),
		linear-gradient(180deg, color-mix(in oklab, var(--paper) 80%, transparent), var(--paper));
	box-shadow: var(--shadow-lg);
}

.landing-final-cta .mini-flock {
	position: absolute;
	top: 30px;
	right: 40px;
	display: flex;
	gap: 4px;
	opacity: 0.8;
}

.landing-final-cta .mini-flock .bird {
	position: static;
	width: 34px;
	height: 26px;
}

.landing-final-cta h2 {
	max-width: 20ch;
	margin: 0 0 12px;
	font-size: clamp(28px, 3.6vw, 44px);
	font-weight: 400;
	line-height: 1.05;
	letter-spacing: -0.02em;
}

.landing-final-cta p {
	max-width: 52ch;
	margin: 0 0 28px;
	color: var(--ink-2);
	font-size: 16px;
	line-height: 1.55;
}

.landing-footer {
	padding: 36px 0 48px;
	border-top: 1px solid var(--rule);
	color: var(--ink-3);
	font-size: 13px;
}

.landing-footer-row {
	display: flex;
	flex-wrap: wrap;
	justify-content: space-between;
	gap: 20px;
}

.landing-footer-brand {
	display: inline-flex;
	align-items: center;
	gap: 10px;
	margin-bottom: 10px;
}

.landing-footer-brand .mark {
	display: grid;
	place-items: center;
	width: 36px;
	height: 36px;
}

.landing-footer-brand .name {
	color: var(--ink);
	font-size: 20px;
	font-weight: 700;
	letter-spacing: -0.01em;
}

.landing-footer-links {
	display: flex;
	flex-wrap: wrap;
	gap: 20px;
}

.landing-footer-links a {
	color: var(--ink-2);
	text-decoration: none;
}

.landing-footer-links a:hover {
	color: var(--brand);
}

.landing-install {
	max-width: 560px;
}

.landing-final-cta :deep(.landing-install) {
	max-width: 520px;
	width: 100%;
}

.landing-home :deep(.landing-install) {
	overflow: hidden;
	border: 1px solid var(--rule-strong);
	border-radius: 14px;
	background: var(--paper);
	box-shadow: var(--shadow-sm);
}

.landing-home :deep(.landing-install-tabs) {
	display: flex;
	gap: 2px;
	padding: 6px;
	border-bottom: 1px solid var(--rule);
	background: color-mix(in oklab, var(--bg-soft) 60%, transparent);
}

.landing-home :deep(.landing-install-tab) {
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

.landing-home :deep(.landing-install-tab:hover) {
	color: var(--ink);
}

.landing-home :deep(.landing-install-tab.active) {
	background: var(--paper);
	color: var(--ink);
	box-shadow: 0 1px 2px rgba(0, 0, 0, 0.06);
}

.landing-home :deep(.landing-install-command) {
	display: flex;
	align-items: center;
	gap: 10px;
	padding: 14px 16px;
	color: var(--ink);
	font-family: var(--mono);
	font-size: 13.5px;
}

.landing-home :deep(.landing-install-command .prompt) {
	color: var(--brand);
	font-weight: 600;
}

.landing-home :deep(.landing-install-command .command) {
	flex: 1;
	min-width: 0;
	overflow: hidden;
	text-overflow: ellipsis;
	white-space: nowrap;
}

.landing-home :deep(.landing-install-command .caret) {
	display: inline-block;
	width: 8px;
	height: 1em;
	margin-left: 2px;
	background: var(--brand);
	vertical-align: -2px;
	animation: blink 1.05s step-end infinite;
}

.landing-home :deep(.landing-install-copy) {
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

.landing-home :deep(.landing-install-copy:hover) {
	border-color: var(--rule-strong);
	background: color-mix(in oklab, var(--bg-soft) 45%, transparent);
	color: var(--ink);
}

.landing-home :deep(.landing-install-copy.copied) {
	border-color: #2eb67d;
	color: #2eb67d;
}

.landing-home :deep(.landing-install-minimal .landing-install-command) {
	padding-block: 12px;
}

.reveal {
	opacity: 0;
	transform: translateY(14px);
	transition:
		opacity 0.6s ease,
		transform 0.6s ease;
}

.reveal.in {
	opacity: 1;
	transform: translateY(0);
}

@keyframes flap {

	0%,
	100% {
		transform: scaleY(1) skewX(-3deg);
	}

	50% {
		transform: scaleY(0.55) skewX(3deg);
	}
}

@keyframes drift-1 {
	0% {
		transform: translate(0, 0) rotate(-4deg);
	}

	50% {
		transform: translate(30px, -12px) rotate(-8deg);
	}

	100% {
		transform: translate(0, 0) rotate(-4deg);
	}
}

@keyframes drift-2 {
	0% {
		transform: translate(0, 0) rotate(-2deg);
	}

	50% {
		transform: translate(-18px, 14px) rotate(-6deg);
	}

	100% {
		transform: translate(0, 0) rotate(-2deg);
	}
}

@keyframes drift-3 {
	0% {
		transform: translate(0, 0) rotate(0deg);
	}

	50% {
		transform: translate(22px, 18px) rotate(-5deg);
	}

	100% {
		transform: translate(0, 0) rotate(0deg);
	}
}

@keyframes float-pod {

	0%,
	100% {
		transform: translateY(0);
	}

	50% {
		transform: translateY(-6px);
	}
}

@keyframes blink {
	50% {
		opacity: 0;
	}
}

@media (max-width: 960px) {
	.landing-bento {
		grid-template-columns: repeat(2, 1fr);
	}

	.c-wide-2,
	.c-wide-3,
	.c-wide-4 {
		grid-column: span 2;
	}

	.landing-docs-grid {
		grid-template-columns: repeat(2, 1fr);
	}
}

@media (max-width: 900px) {
	.landing-install {
		max-width: none;
		width: 100%;
	}

	.landing-showcase-panel {
		grid-template-columns: 1fr;
	}

	.landing-config-row {
		grid-template-columns: 1fr;
	}
}

@media (max-width: 860px) {

	.landing-hero-inner,
	.landing-stats-grid,
	.landing-showcase-head {
		grid-template-columns: 1fr;
	}

	.landing-flock-stage {
		display: none;
	}

	.landing-showcase-tabs {
		border-right: 0;
		border-bottom: 1px solid var(--rule);
	}
}

@media (max-width: 780px) {
	.security-grid {
		grid-template-columns: 1fr;
	}
}

@media (max-width: 540px) {
	.landing-bento,
	.landing-docs-grid {
		grid-template-columns: 1fr;
	}

	.c-wide-2,
	.c-wide-3,
	.c-wide-4 {
		grid-column: span 1;
	}

	.bar-row {
		grid-template-columns: 96px 1fr 92px;
	}

	.bar-savings {
		flex-direction: column;
	}

	.run-chart {
		gap: 4px;
	}

	.landing-final-cta {
		padding: 40px 24px;
	}

	.channels-stage {
		min-height: 236px;
	}

	.channel-logo-card {
		min-width: 0;
		max-width: none;
	}

	.channel-logo-card.signal,
	.channel-logo-card.discord {
		top: 16px;
	}

	.channel-logo-card.signal {
		left: 12px;
		right: 86px;
	}

	.channel-logo-card.discord {
		left: 84px;
		right: 12px;
	}

	.channel-logo-card.slack {
		left: 18px;
		right: 18px;
		bottom: 16px;
	}

	.landing-final-cta .mini-flock {
		display: none;
	}
}

@media (prefers-reduced-motion: reduce) {
	* {
		animation: none !important;
		transition: none !important;
	}
}
</style>
