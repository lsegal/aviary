<script setup lang="ts">
import { withBase } from "vitepress";
import { computed, ref } from "vue";
import { data as skills } from "../../../../skills/skills.data.mts";
import SkillCard from "./SkillCard.vue";
import SkillCommand from "./SkillCommand.vue";
import "./skills.css";

const props = defineProps<{ name: string }>();

const skill = computed(() => {
	const found = skills.find((item) => item.name === props.name);
	if (!found) throw new Error(`unknown skill ${props.name}`);
	return found;
});
const headline = computed(() => skill.value.headlineParts);
const otherSkills = computed(() =>
	skills.filter((item) => item.name !== props.name),
);

// Standalone installs for the CLIs skills.sh supports. Dirs mirror
// internal/codingskills SkillsDir.
const agents = [
	{
		key: "claude-code",
		label: "Claude Code",
		dir: "~/.claude/skills/",
		invoke: (s: string) => s,
	},
	{
		key: "codex",
		label: "Codex",
		dir: "~/.agents/skills/",
		invoke: (s: string) => s.replace(/^\//, "$"),
	},
	{
		key: "gemini-cli",
		label: "Gemini CLI",
		dir: "~/.gemini/skills/",
		invoke: (s: string) => s,
	},
	{
		key: "opencode",
		label: "OpenCode",
		dir: "~/.config/opencode/skills/",
		invoke: (s: string) => s,
	},
] as const;

const activeAgent = ref<(typeof agents)[number]["key"]>("claude-code");
const agent = computed(
	() => agents.find((item) => item.key === activeAgent.value) ?? agents[0],
);
const installCommand = computed(
	() =>
		`npx skills add lsegal/aviary@${skill.value.name} --global --agent ${agent.value.key} -y`,
);
const ghCommand = computed(
	() =>
		`gh skill install lsegal/aviary ${skill.value.name} --allow-hidden-dirs --agent ${agent.value.key} --scope user`,
);
</script>

<template>
	<div class="skill-page">
		<section class="skill-hero">
			<div class="skill-wrap skill-hero-inner">
				<div class="skill-hero-copy">
					<a class="skill-crumb" :href="withBase('/skills/')">
						<svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"
							aria-hidden="true">
							<path d="M19 12H5M11 19l-7-7 7-7" />
						</svg>
						All skills
					</a>
					<div class="skill-eyebrow">{{ skill.eyebrow }}</div>
					<h1 class="skill-title">
						{{ headline.before }}<em v-if="headline.accent">{{ headline.accent }}</em>{{ headline.after }}
					</h1>
					<p class="skill-lede">{{ skill.tagline }}</p>
					<div class="skill-hero-row">
						<span class="skill-tag skill-tag-lg">{{ skill.name }}</span>
						<span class="skill-anywhere">Works with or without Aviary</span>
					</div>
					<div class="skill-hero-cta">
						<a class="skill-btn skill-btn-primary" href="#install">Install</a>
						<a class="skill-btn skill-btn-ghost" :href="skill.skillUrl" target="_blank" rel="noreferrer">
							Read the skill
						</a>
					</div>
				</div>
				<div class="skill-terminal" aria-label="Example invocation">
					<div class="skill-terminal-bar">
						<span class="tl"><span></span><span></span><span></span></span>
						<span>{{ skill.displayName }}</span>
					</div>
					<div class="skill-terminal-body">
						<div class="line"><span class="prompt">&gt;</span> {{ skill.usage }}</div>
						<div v-for="(step, index) in skill.steps" :key="step.title" class="line muted">
							<span class="ok">✓</span> {{ String(index + 1).padStart(2, "0") }} {{ step.title }}
						</div>
					</div>
				</div>
			</div>
		</section>

		<section class="skill-band">
			<div class="skill-wrap">
				<div class="skill-section-eyebrow">How it works</div>
				<h2 class="skill-section-title">Step by <em>step.</em></h2>
				<ol class="skill-flow">
					<li v-for="(step, index) in skill.steps" :key="step.title">
						<span class="num">{{ String(index + 1).padStart(2, "0") }}</span>
						<span class="tit">{{ step.title }}</span>
						<span class="sub">{{ step.text }}</span>
					</li>
				</ol>
			</div>
		</section>

		<section id="install" class="skill-band">
			<div class="skill-wrap">
				<div class="skill-section-eyebrow">Install</div>
				<h2 class="skill-section-title">Use it <em>anywhere.</em></h2>
				<p class="skill-section-sub">
					<code>{{ skill.name }}</code> is an ordinary
					<a href="https://skills.sh/" target="_blank" rel="noreferrer">skills.sh</a>
					skill. Install it into your CLI agent and run it yourself, no Aviary
					needed, or let Aviary run it for you.
				</p>
				<div class="skill-install-grid">
					<div class="skill-panel">
						<div class="skill-panel-kind">a · Standalone, without Aviary</div>
						<h3>Install it in your CLI</h3>
						<div class="skill-tabs" role="tablist">
							<button v-for="item in agents" :key="item.key" type="button" role="tab"
								:aria-selected="activeAgent === item.key" :class="{ active: activeAgent === item.key }"
								@click="activeAgent = item.key">
								{{ item.label }}
							</button>
						</div>
						<SkillCommand :command="installCommand" />
						<p class="skill-panel-note">Or with the GitHub CLI:</p>
						<SkillCommand :command="ghCommand" />
						<p class="skill-panel-note">
							The skill lands in <code>{{ agent.dir }}{{ skill.name }}/</code>.
							Then run it in {{ agent.label }}:
						</p>
						<SkillCommand :command="agent.invoke(skill.usage)" prompt=">" />
					</div>
					<div class="skill-panel">
						<div class="skill-panel-kind">b · With Aviary</div>
						<h3>Let Aviary run it</h3>
						<p>{{ skill.aviary }}</p>
						<p class="skill-panel-note">
							Aviary installs and updates the skill in every CLI it finds on
							first launch. To install it by hand:
						</p>
						<SkillCommand command="aviary install-skills" />
						<div class="skill-links">
							<a :href="withBase('/guide/projects')">Projects guide →</a>
							<a :href="withBase('/guide/scheduled-tasks')">Scheduled tasks →</a>
							<a :href="withBase('/guide/skills/')">Keeping skills current →</a>
						</div>
					</div>
				</div>
			</div>
		</section>

		<section class="skill-band">
			<div class="skill-wrap skill-details-grid">
				<div>
					<div class="skill-section-eyebrow">Requirements</div>
					<ul class="skill-list">
						<li v-for="item in skill.requirements" :key="item">{{ item }}</li>
					</ul>
				</div>
				<div>
					<div class="skill-section-eyebrow">Options</div>
					<dl class="skill-options">
						<template v-for="option in skill.options" :key="option.name">
							<dt><code>{{ option.name }}</code></dt>
							<dd>{{ option.text }}</dd>
						</template>
					</dl>
				</div>
				<div>
					<div class="skill-section-eyebrow">Source</div>
					<ul class="skill-list skill-source">
						<li><a :href="skill.skillUrl" target="_blank" rel="noreferrer">SKILL.md on GitHub</a></li>
						<li><a :href="skill.sourceUrl" target="_blank" rel="noreferrer">Skill folder</a></li>
						<li><a :href="withBase(skill.docsPath)">Full reference in the docs</a></li>
					</ul>
				</div>
			</div>
		</section>

		<section class="skill-band skill-band-last">
			<div class="skill-wrap">
				<div class="skill-section-eyebrow">More skills</div>
				<div class="skill-card-grid">
					<SkillCard v-for="item in otherSkills" :key="item.name" :skill="item" />
				</div>
			</div>
		</section>
	</div>
</template>
