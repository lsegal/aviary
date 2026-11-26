<script setup lang="ts">
import { useData, withBase } from "vitepress";
import { onBeforeUnmount, onMounted } from "vue";
import LandingInstallBlock from "./LandingInstallBlock.vue";

const { isDark } = useData();

const loopSteps = [
	{
		title: "Watch",
		description: "A repository, a Project board, or a Discussions board",
		position: "left: 50%; top: 8%",
	},
	{
		title: "Claim",
		description: "A signed comment, so one instance owns each issue",
		position: "left: 92%; top: 50%",
	},
	{
		title: "Implement",
		description: "A draft pull request, then the fix and its tests",
		position: "left: 50%; top: 92%",
	},
	{
		title: "Merge",
		description: "CI repaired until green, merged, issue closed",
		position: "left: 8%; top: 50%",
	},
];

const skills = [
	{
		key: "fix",
		command: "/gh-fix",
		title: "Issue to merged fix",
		description:
			"Draft pull request, implementation, tests, changelog, CI repair, and merge. Run for every issue that is ready for work.",
		href: "/guide/skills/gh-fix",
	},
	{
		key: "discuss",
		command: "/gh-discuss",
		title: "One useful answer",
		description:
			"Researches a new Discussion and posts a single answer, or stays silent when it is not sure. Read-only.",
		href: "/guide/skills/gh-discuss",
	},
	{
		key: "create",
		command: "/gh-create",
		title: "Issues filed the same way",
		description:
			"Expands a short request into a structured issue: labeled, assigned, media attached, original prompt quoted.",
		href: "/guide/skills/gh-create",
	},
	{
		key: "safe",
		command: "/gh-safe-merge",
		title: "Contributions merged safely",
		description:
			"A safety check on an outside pull request gates everything, then CI goes green and it merges. Not a code review.",
		href: "/guide/skills/gh-safe-merge",
	},
];

const prerequisites = [
	{
		title: "GitHub CLI (gh)",
		description:
			"Signed in with access to every repository Aviary watches. Project boards need the project scopes.",
	},
	{
		title: "A CLI provider",
		description:
			"Claude Code, Codex, Gemini CLI, or any other CLI with a provider definition, on your PATH.",
	},
	{
		title: "Node.js and npx",
		description:
			"Aviary installs gh-fix, gh-discuss, gh-create, and gh-safe-merge into each CLI through skills.sh.",
	},
];

function screenshot(name: string) {
	return withBase(
		`/screenshots/${name}-${isDark.value ? "dark" : "light"}.png`,
	);
}

onMounted(() => {
	document.body.classList.add("landing-home-active");
});

onBeforeUnmount(() => {
	document.body.classList.remove("landing-home-active");
});
</script>

<template>
	<div class="projects-page">
		<section class="pp-hero pp-wrap">
			<div>
				<p class="pp-eyebrow reveal">Aviary · Code Loop</p>
				<h1 class="reveal">Write the issue.<br />Aviary ships the fix.</h1>
				<p class="pp-lede reveal">
					Aviary watches your GitHub issues. Describe a problem, a proposal, or
					a thing that should exist, and it puts one of your CLI providers on
					it until the pull request merges.
				</p>
				<p class="pp-sub reveal">
					The queue is your issue list. The scheduling rules are labels,
					filters, and dependencies. The progress report is the thread and the
					board. Nothing new to learn, nowhere new to look. Rather pick the work
					yourself? Put a project in
					<a :href="withBase('/guide/projects#manual-mode')">manual mode</a> and
					Aviary works only what you queue.
				</p>
				<p class="pp-sub reveal">
					Rather pick the work yourself? Switch a project to
					<strong>Manual</strong> and Aviary works only what you queue, with no
					issue discovery. Switch between Watch and Manual live from the header's Issue
					discovery menu or the project's settings, or start with
					<code>aviary watch --manual</code>.
				</p>
				<div class="pp-actions reveal">
					<a class="pp-button" href="#install">Install Aviary</a>
					<code class="pp-command">aviary watch owner/repo</code>
				</div>
			</div>

			<div class="pp-github reveal" aria-label="Sample GitHub issue worked by Aviary">
				<div class="pp-github-top">
					<span>owner/repo</span>
					<span class="dot">·</span>
					<span>Issues</span>
					<span class="dots">•••</span>
				</div>
				<div class="pp-github-body">
					<div class="meta">
						<span class="open-pill">Open</span>
						<span class="byline">opened by <b>you</b> · assigned to <b>you</b></span>
					</div>
					<h3>Retry failed webhook deliveries <span class="num">#212</span></h3>
					<p>
						Deliveries that time out are dropped. Retry them with backoff and
						show the attempt count in the log. Blocked until #209 lands.
					</p>
					<div class="tags">
						<span class="tag-a">enhancement</span>
						<span class="tag-b">webhooks</span>
					</div>
					<div class="comment">
						<b>aviary</b><span>just now</span>
						<p>Starting work on this issue <code>/aviary:3F9A21C0</code></p>
					</div>
					<div class="comment">
						<b>aviary</b><span>3 minutes later</span>
						<p>Opened draft #213 with a plan: backoff, attempt counter, tests.</p>
					</div>
					<div class="comment">
						<b>aviary</b><span>21 minutes later</span>
						<p>CI repaired a flaky timeout test. Merged #213 and closed this issue.</p>
					</div>
					<div class="checks">
						<span class="pp-pulse" />
						<b class="ok">All checks have passed</b>
						<b class="merged">#213 merged</b>
					</div>
				</div>
			</div>
		</section>

		<section id="loop" class="pp-band pp-loop-band">
			<div class="pp-wrap pp-loop">
				<div>
					<p class="pp-eyebrow">The loop</p>
					<h2>It keeps going after you stop looking</h2>
					<p>
						Aviary reads each watched target in its own browser, signed in as
						you, or through the GitHub API by polling or webhook deliveries, and
						picks up issues that match the filter (<code>assignee:@me</code>
						by default), <code>critical</code> and <code>high</code> first.
						It claims each one with a signed comment so no two instances work
						the same issue, then dispatches <code>/gh-fix</code> to the first
						provider with quota left.
					</p>
					<p class="pp-muted">
						The skill opens a draft pull request before writing code, pushes
						checkpoints while it works, repairs CI until the build is green, and
						merges. An interrupted job finds its pull request and carries on
						instead of starting over.
					</p>
					<p class="pp-muted">
						Issues with open blockers wait, or stack on the blocker's pull
						request. Merge holds, forks, and questions only you can answer end
						the job ready and green, with a comment saying why.
					</p>
				</div>
				<div class="pp-circle">
					<svg viewBox="0 0 520 520" aria-hidden="true">
						<circle cx="260" cy="260" r="176" class="ring" />
						<circle cx="260" cy="260" r="176" class="ring-spin" />
						<circle cx="260" cy="260" r="122" class="ring-dashed" />
					</svg>
					<ol>
						<li v-for="(step, index) in loopSteps" :key="step.title" class="pp-node" :style="step.position">
							<i>{{ String(index + 1).padStart(2, "0") }}</i>
							<b>{{ step.title }}</b>
							<span>{{ step.description }}</span>
						</li>
					</ol>
					<div class="pp-circle-center" aria-hidden="true">
						<img :src="withBase('/logo.png')" alt="" />
						<span>aviary watch</span>
					</div>
				</div>
			</div>
		</section>

		<section id="board" class="pp-band">
			<div class="pp-wrap pp-board">
				<div class="pp-window reveal">
					<div class="pp-window-bar">
						<span class="tl"><span /><span /><span /></span>
						<span>localhost:16677 · Code Loop</span>
					</div>
					<img :src="screenshot('projects-board')" alt="Code Loop board for two projects with starting, running, paused, done, and failed jobs" />
				</div>
				<div>
					<p class="pp-eyebrow">The board</p>
					<h2>Watch it work, or don't</h2>
					<p>
						Every agent gets a board of its projects' jobs: what is starting,
						running, paused on CI or a blocker, done, or failed. Retry, stop, or
						summarize a job from its card.
					</p>
					<p class="pp-muted">
						Open a job to follow it from claim to merge: the plan, tool calls,
						diffs, test runs, and CI repairs as they happen, with token usage
						and cost.
					</p>
				</div>
			</div>
			<div class="pp-wrap pp-board pp-board-reverse">
				<div>
					<p class="pp-eyebrow">The run view</p>
					<h2>Steer it from the thread</h2>
					<p>
						Comments from allowed commenters on the issue or pull request reach
						the running job. Everyone else is just information.
					</p>
					<p class="pp-muted">
						When a job merges, fails, or starts waiting, a short report goes to
						the Slack, Discord, or Signal channel you pick. A failed or waiting
						job also raises an Aviary
						<a :href="withBase('/guide/notifications')">notification</a>, as a
						banner in the web UI or a system notification.
					</p>
				</div>
				<div class="pp-window reveal">
					<div class="pp-window-bar">
						<span class="tl"><span /><span /><span /></span>
						<span>localhost:16677 · job run</span>
					</div>
					<img :src="screenshot('projects-run')" alt="A Code Loop job's run view with plan, file changes, and test output" />
				</div>
			</div>
			<div class="pp-wrap pp-board pp-terminal">
				<div class="pp-window pp-terminal-window reveal">
					<div class="pp-window-bar">
						<span class="tl"><span /><span /><span /></span>
						<span>terminal · aviary watch owner/repo</span>
					</div>
					<img :src="withBase('/screenshots/watch-tui.png')" alt="The aviary watch terminal board with its one-row filter header and prompt chat open: a question, a collapsed tool call, the agent's reply, and the selected job's output" />
				</div>
				<div>
					<p class="pp-eyebrow">The terminal</p>
					<h2>Or never leave the terminal</h2>
					<p>
						<code>aviary watch owner/repo</code> opens the same board in your
						terminal. Press <kbd>c</kbd> or <kbd>/</kbd> for a prompt chat with
						the Code Loop agent: ask what is running, or tell it what to do
						next, and watch the reply and its tool calls stream in.
					</p>
					<p class="pp-muted">
						The conversation is the one the web board's chat shows, so you can
						start in one and pick it up in the other. Add
						<code>--no-webui</code> to skip the web UI entirely.
					</p>
				</div>
			</div>
		</section>

		<section id="skills" class="pp-wrap">
			<div class="pp-skills">
				<p class="pp-eyebrow">Four focused skills</p>
				<h2>Hand work to the right robot.</h2>
				<p class="pp-section-lede">
					All four install into every CLI provider Aviary finds, and work on
					their own with no Aviary at all.
				</p>
				<div class="pp-skills-grid">
					<a v-for="skill in skills" :key="skill.key" class="pp-skill-card" :class="skill.key"
						:href="withBase(skill.href)">
						<span class="cmd">{{ skill.command }}</span>
						<strong>{{ skill.title }}</strong>
						<span>{{ skill.description }}</span>
					</a>
				</div>
			</div>
		</section>

		<section id="install" class="pp-band pp-install-band">
			<div class="pp-wrap pp-install">
				<div>
					<h2>Install</h2>
					<p>
						The installer downloads the release for your OS and architecture.
						Aviary installs the skills on first launch.
					</p>
					<LandingInstallBlock />
					<p class="pp-label">Then watch a repository</p>
					<pre><code>aviary watch owner/repo</code></pre>
					<p class="pp-muted">
						To keep a project watched, add it to an agent with
						<code>aviary project add owner/repo --agent coder</code>.
					</p>
					<p class="pp-label">Or queue the work yourself</p>
					<pre><code>aviary watch --manual owner/repo</code></pre>
					<p class="pp-muted">
						In <a :href="withBase('/guide/projects#manual-mode')">manual mode</a>
						Aviary picks up no issues on its own. It works only what you queue
						(fix an issue, safe-merge a pull request, answer a discussion), plus
						the project's scheduled prompts.
					</p>
				</div>
				<div>
					<p class="pp-eyebrow">Prerequisites</p>
					<ul class="pp-prereqs">
						<li v-for="item in prerequisites" :key="item.title">
							<strong>{{ item.title }}</strong>
							<span>{{ item.description }}</span>
						</li>
					</ul>
					<div class="pp-links">
						<a class="pp-button" :href="withBase('/guide/projects')">Read the Code Loop guide</a>
						<a class="pp-button ghost" :href="withBase('/guide/code-loop')">Follow one issue through the loop</a>
					</div>
				</div>
			</div>
		</section>

		<footer class="pp-footer pp-wrap">
			<span>Licensed under the MIT License. Copyright © 2026 Loren Segal.</span>
			<a href="https://github.com/lsegal/aviary" target="_blank" rel="noreferrer">github.com/lsegal/aviary ↗</a>
		</footer>
	</div>
</template>

<style scoped>
.projects-page {
	color: var(--ink);
	font-family: var(--sans);
	font-size: 17px;
	line-height: 1.65;
}

.projects-page h1,
.projects-page h2,
.projects-page h3 {
	border: 0;
	color: var(--ink);
	letter-spacing: -0.025em;
	line-height: 1.1;
}

.projects-page h2 {
	margin: 0.2rem 0 1.1rem;
	padding: 0;
	font-size: clamp(2rem, 4vw, 2.8rem);
}

.projects-page p {
	margin: 0 0 1rem;
}

.projects-page code {
	padding: 0.15em 0.34em;
	border-radius: 0.3rem;
	background: var(--bg-soft);
	color: var(--ink);
	font-family: var(--mono);
	font-size: 0.86em;
}

.pp-wrap {
	width: min(100% - 2.5rem, 1180px);
	margin: 0 auto;
}

.pp-band {
	padding: 6rem 0;
}

.pp-eyebrow {
	margin: 0 !important;
	color: var(--brand);
	font-family: var(--mono);
	font-size: 0.72rem;
	font-weight: 700;
	letter-spacing: 0.1em;
	text-transform: uppercase;
}

.pp-muted {
	color: var(--ink-2);
}

.pp-button {
	display: inline-block;
	padding: 0.68rem 1rem;
	border-radius: 0.5rem;
	background: var(--brand);
	box-shadow: 5px 5px 0 color-mix(in oklab, var(--brand) 28%, transparent);
	color: #fff;
	font-weight: 700;
	text-decoration: none;
}

.pp-button:hover {
	background: var(--brand-2);
	color: #fff;
}

.pp-button.ghost {
	border: 1px solid var(--rule-strong);
	background: transparent;
	box-shadow: none;
	color: var(--ink);
}

.pp-button.ghost:hover {
	border-color: var(--brand);
	color: var(--brand);
}

/* hero */
.pp-hero {
	display: grid;
	grid-template-columns: minmax(0, 1.1fr) minmax(0, 0.95fr);
	gap: 4rem;
	align-items: center;
	padding: 4.8rem 0 5.8rem;
}

.pp-hero h1 {
	margin: 0.4rem 0 0;
	font-size: clamp(2.6rem, 4.6vw, 4rem);
	line-height: 1.02;
}

.pp-lede {
	max-width: 30em;
	margin: 1.75rem 0 0 !important;
	font-size: 1.25rem;
	line-height: 1.5;
}

.pp-sub {
	max-width: 32em;
	margin: 1.1rem 0 0 !important;
	color: var(--ink-2);
	font-size: 1.03rem;
}

.pp-actions {
	display: flex;
	flex-wrap: wrap;
	align-items: center;
	gap: 0.9rem;
	margin-top: 2.1rem;
}

.projects-page .pp-command {
	padding: 0.75rem 1rem;
	border: 1px solid var(--rule);
	border-radius: 0.3rem;
	font-size: 0.85rem;
}

/* GitHub issue mock */
.pp-github {
	overflow: hidden;
	border: 1px solid #d0d7de;
	border-radius: 0.75rem;
	background: #fff;
	box-shadow: 15px 17px 0 color-mix(in oklab, var(--brand) 12%, transparent);
	color: #24292f;
	font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
	font-size: 0.9rem;
}

.pp-github-top {
	display: flex;
	align-items: center;
	gap: 0.5rem;
	padding: 0.65rem 0.85rem;
	border-bottom: 1px solid #d8dee4;
	background: #f6f8fa;
	color: #57606a;
	font-size: 0.76rem;
	font-weight: 650;
}

.pp-github-top .dot {
	color: #8c959f;
}

.pp-github-top .dots {
	margin-left: auto;
	letter-spacing: 0.14em;
}

.pp-github-body {
	padding: 1.25rem;
}

.pp-github-body .meta {
	display: flex;
	flex-wrap: wrap;
	align-items: center;
	gap: 0.6rem;
}

.pp-github-body .open-pill {
	padding: 0.2rem 0.55rem;
	border-radius: 2rem;
	background: #1a7f37;
	color: #fff;
	font-size: 0.7rem;
	font-weight: 800;
}

.pp-github-body .byline {
	color: #57606a;
	font-size: 0.78rem;
}

.pp-github-body .byline b {
	color: #0969da;
	font-weight: 650;
}

.projects-page .pp-github-body h3 {
	margin: 0.75rem 0 0.25rem;
	color: #24292f;
	font-size: 1.3rem;
}

.pp-github-body h3 .num {
	color: #8c959f;
	font-weight: 400;
}

.pp-github-body > p {
	margin: 0;
	color: #57606a;
	font-size: 0.8rem;
}

.pp-github-body .tags {
	display: flex;
	flex-wrap: wrap;
	gap: 0.4rem;
	margin-top: 0.7rem;
}

.pp-github-body .tags span {
	padding: 0.14rem 0.55rem;
	border-radius: 2rem;
	font-size: 0.7rem;
	font-weight: 650;
}

.pp-github-body .tag-a {
	background: #ddf4ff;
	color: #0969da;
}

.pp-github-body .tag-b {
	background: #ffebe9;
	color: var(--brand);
}

.pp-github-body .comment {
	margin-top: 0.6rem;
	padding: 0.7rem;
	border: 1px solid #d8dee4;
	border-radius: 0.4rem;
	font-size: 0.78rem;
}

.pp-github-body .comment b {
	color: #b83a1d;
	font-family: var(--mono);
}

.pp-github-body .comment > span {
	margin-left: 0.4rem;
	color: #57606a;
	font-size: 0.72rem;
}

.pp-github-body .comment p {
	margin: 0.3rem 0 0;
	color: #1f2328;
	font-size: 0.8rem;
	line-height: 1.45;
}

.pp-github-body .comment code {
	background: #f6f8fa;
	color: #1f2328;
}

.pp-github-body .checks {
	display: flex;
	align-items: center;
	gap: 0.45rem;
	margin-top: 0.9rem;
	padding-top: 0.75rem;
	border-top: 1px solid #d8dee4;
	font-size: 0.79rem;
}

.pp-github-body .checks .ok {
	color: #1a7f37;
}

.pp-github-body .checks .merged {
	margin-left: auto;
	color: #b83a1d;
}

.pp-pulse {
	display: inline-block;
	width: 0.5rem;
	height: 0.5rem;
	border-radius: 999px;
	background: #1a7f37;
	animation: pp-pulse 2.2s ease-in-out infinite;
}

.dark .pp-github {
	border-color: #30363d;
	background: #0d1117;
	color: #e6edf3;
}

.dark .pp-github-top {
	border-color: #30363d;
	background: #161b22;
	color: #8d96a0;
}

.dark .pp-github-body .comment {
	border-color: #30363d;
}

.dark .pp-github-body .checks {
	border-color: #30363d;
}

.dark .projects-page .pp-github-body h3,
.dark .pp-github-body .comment p {
	color: #e6edf3;
}

.dark .pp-github-body > p,
.dark .pp-github-body .byline,
.dark .pp-github-body .comment > span {
	color: #8d96a0;
}

.dark .pp-github-body .comment b,
.dark .pp-github-body .checks .merged {
	color: var(--brand);
}

.dark .pp-github-body .comment code {
	background: #161b22;
	color: #e6edf3;
}

.dark .pp-github-body .tag-a {
	background: rgba(56, 139, 253, 0.15);
	color: #58a6ff;
}

.dark .pp-github-body .tag-b {
	background: var(--brand-soft);
}

/* loop */
.pp-loop-band {
	border-top: 1px solid var(--rule);
	border-bottom: 1px solid var(--rule);
	background: var(--bg-soft);
}

.pp-loop {
	display: grid;
	grid-template-columns: minmax(0, 0.9fr) minmax(0, 1.1fr);
	gap: 4rem;
	align-items: center;
}

.pp-loop > div:first-child p {
	font-size: 1.05rem;
}

.pp-circle {
	position: relative;
	width: 100%;
	max-width: 420px;
	aspect-ratio: 1;
	margin: 4.5rem auto;
}

.pp-circle svg {
	position: absolute;
	inset: 0;
	width: 100%;
	height: 100%;
}

.pp-circle .ring {
	fill: none;
	stroke: var(--rule-strong);
	stroke-width: 2;
}

.pp-circle .ring-spin {
	fill: none;
	stroke: var(--brand);
	stroke-dasharray: 90 1015;
	stroke-linecap: round;
	stroke-width: 3;
	animation: pp-dash 7s linear infinite;
}

.pp-circle .ring-dashed {
	fill: none;
	stroke: var(--rule-strong);
	stroke-dasharray: 3 7;
	stroke-width: 1;
}

.pp-circle ol {
	margin: 0;
	padding: 0;
	list-style: none;
}

.pp-node {
	position: absolute;
	display: grid;
	justify-items: center;
	gap: 0.3rem;
	width: 9.5rem;
	margin: 0 !important;
	padding: 0.85rem 0.7rem;
	border: 1px solid var(--rule);
	border-top: 3px solid var(--brand);
	border-radius: 0.4rem;
	background: var(--paper);
	box-shadow: var(--shadow-sm);
	text-align: center;
	transform: translate(-50%, -50%);
}

.pp-node i {
	color: var(--brand);
	font-family: var(--mono);
	font-size: 0.68rem;
	font-style: normal;
	font-weight: 700;
	letter-spacing: 0.1em;
}

.pp-node b {
	font-size: 0.95rem;
}

.pp-node span {
	color: var(--ink-2);
	font-size: 0.76rem;
	line-height: 1.3;
}

.pp-circle-center {
	position: absolute;
	top: 50%;
	left: 50%;
	display: grid;
	justify-items: center;
	gap: 0.3rem;
	transform: translate(-50%, -50%);
}

.pp-circle-center img {
	width: 3.4rem;
	height: 3.4rem;
	object-fit: contain;
}

.pp-circle-center span {
	color: var(--ink-2);
	font-family: var(--mono);
	font-size: 0.68rem;
	font-weight: 700;
	letter-spacing: 0.08em;
	text-transform: uppercase;
}

/* board */
.pp-board {
	display: grid;
	grid-template-columns: minmax(0, 1.25fr) minmax(0, 0.75fr);
	gap: 3.5rem;
	align-items: center;
}

.pp-board-reverse {
	grid-template-columns: minmax(0, 0.75fr) minmax(0, 1.25fr);
	margin-top: 5rem;
}

.pp-board > div:not(.pp-window) p {
	font-size: 1.05rem;
}

.pp-window {
	overflow: hidden;
	border: 1px solid var(--rule-strong);
	border-radius: 0.75rem;
	background: var(--paper);
	box-shadow: var(--shadow-lg);
}

.pp-window-bar {
	display: flex;
	align-items: center;
	gap: 0.8rem;
	padding: 0.55rem 0.85rem;
	border-bottom: 1px solid var(--rule);
	background: var(--bg-soft);
	color: var(--ink-3);
	font-family: var(--mono);
	font-size: 0.72rem;
}

.pp-window-bar .tl {
	display: flex;
	gap: 6px;
}

.pp-window-bar .tl span {
	width: 10px;
	height: 10px;
	border-radius: 999px;
	background: var(--rule-strong);
}

.pp-window img {
	display: block;
	width: 100%;
}

.pp-terminal {
	margin-top: 5rem;
}

.pp-terminal-window {
	background: #0f1115;
}

.pp-board kbd {
	padding: 0.05rem 0.35rem;
	border: 1px solid var(--rule-strong);
	border-bottom-width: 2px;
	border-radius: 0.25rem;
	background: var(--bg-soft);
	font-family: var(--mono);
	font-size: 0.85em;
}

/* skills */
.pp-skills {
	padding: 2rem;
	border: 1px solid var(--rule);
	border-radius: 1.2rem;
	background: linear-gradient(125deg, color-mix(in oklab, var(--brand) 5%, transparent), color-mix(in oklab, var(--ember) 7%, transparent));
}

.pp-section-lede {
	max-width: 44em;
	margin: -0.6rem 0 2rem !important;
	color: var(--ink-2);
}

.pp-skills-grid {
	display: grid;
	grid-template-columns: repeat(2, minmax(0, 1fr));
	gap: 1rem;
}

.pp-skill-card {
	display: grid;
	align-content: start;
	gap: 0.25rem;
	min-height: 10rem;
	padding: 1.25rem;
	border: 1px solid var(--rule);
	border-radius: 0.9rem;
	background: var(--paper);
	box-shadow: var(--shadow-sm);
	color: var(--ink);
	text-decoration: none;
	transition: transform 0.15s ease, border-color 0.15s ease;
}

.pp-skill-card:hover {
	border-color: var(--brand);
	color: var(--ink);
	transform: translateY(-3px);
}

.pp-skill-card .cmd {
	color: var(--brand);
	font-family: var(--mono);
	font-weight: 700;
}

.pp-skill-card strong {
	font-size: 1.35rem;
	letter-spacing: -0.015em;
}

.pp-skill-card > span:last-child {
	color: var(--ink-2);
	font-size: 0.92rem;
}

/* install */
.pp-install-band {
	margin-top: 6rem;
	border-top: 1px solid var(--rule);
	background: var(--bg-soft);
}

.pp-install {
	display: grid;
	grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
	gap: 4rem;
	align-items: start;
}

.pp-label {
	margin: 1.6rem 0 0.5rem !important;
	font-size: 0.82rem;
	font-weight: 700;
}

.projects-page pre {
	margin: 0 0 1rem;
	padding: 1rem 1.15rem;
	border-radius: 0.7rem;
	background: #151517;
	white-space: pre-wrap;
}

.projects-page pre code {
	padding: 0;
	background: transparent;
	color: #f1eeec;
}

.pp-prereqs {
	display: grid;
	gap: 1.1rem;
	margin: 1rem 0 0;
	padding: 0;
	list-style: none;
}

.pp-prereqs li {
	display: grid;
	gap: 0.25rem;
	margin: 0;
	padding-left: 1rem;
	border-left: 2px solid var(--rule-strong);
}

.pp-prereqs span {
	color: var(--ink-2);
	font-size: 0.92rem;
	line-height: 1.4;
}

.pp-links {
	display: flex;
	flex-wrap: wrap;
	gap: 0.8rem;
	margin-top: 2rem;
}

.pp-footer {
	display: flex;
	flex-wrap: wrap;
	justify-content: space-between;
	gap: 1.1rem;
	padding: 1.75rem 0 3.25rem;
	color: var(--ink-2);
	font-size: 0.9rem;
}

.pp-footer a {
	color: var(--ink);
	text-decoration: none;
}

@keyframes pp-pulse {
	0%,
	100% {
		opacity: 0.35;
		transform: scale(1);
	}

	50% {
		opacity: 1;
		transform: scale(1.25);
	}
}

@keyframes pp-dash {
	to {
		stroke-dashoffset: -1000;
	}
}

@media (prefers-reduced-motion: reduce) {
	.pp-pulse,
	.pp-circle .ring-spin {
		animation: none;
	}
}

@media (max-width: 860px) {
	.pp-wrap {
		width: min(100% - 1.5rem, 1180px);
	}

	.pp-band {
		padding: 3.5rem 0;
	}

	.pp-hero,
	.pp-loop,
	.pp-board,
	.pp-board-reverse,
	.pp-install {
		grid-template-columns: 1fr;
		gap: 2.5rem;
	}

	.pp-hero {
		padding: 3rem 0 4rem;
	}

	.pp-board-reverse > .pp-window {
		order: -1;
	}

	.pp-circle {
		max-width: 100%;
		aspect-ratio: auto;
		margin: 0;
	}

	.pp-circle svg,
	.pp-circle-center {
		display: none;
	}

	.pp-circle ol {
		display: grid;
		gap: 0.6rem;
	}

	.pp-node {
		position: static;
		width: 100%;
		transform: none;
	}

	.pp-skills-grid {
		grid-template-columns: 1fr;
	}

	.pp-install-band {
		margin-top: 3.5rem;
	}
}
</style>
