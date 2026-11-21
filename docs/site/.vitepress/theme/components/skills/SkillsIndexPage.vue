<script setup lang="ts">
import { withBase } from "vitepress";
import { data as skills } from "../../../../skills/skills.data.mts";
import SkillCard from "./SkillCard.vue";
import SkillCommand from "./SkillCommand.vue";
import "./skills.css";
</script>

<template>
	<div class="skill-page">
		<section class="skill-hero">
			<div class="skill-wrap">
				<div class="skill-eyebrow">Aviary skills</div>
				<h1 class="skill-title">Skills that ship <em>work.</em></h1>
				<p class="skill-lede">
					GitHub skills for Claude Code, Codex, Gemini CLI and other CLI agents.
					Install them and use them on their own, with no Aviary at all, or let
					Aviary run them for you.
				</p>
				<div class="skill-index-install">
					<SkillCommand command="npx skills add lsegal/aviary@gh-fix --global -y" />
					<p class="skill-panel-note">Swap <code>gh-fix</code> for any skill below.</p>
				</div>
			</div>
		</section>
		<section class="skill-band skill-band-last">
			<div class="skill-wrap">
				<div class="skill-card-grid">
					<SkillCard v-for="item in skills" :key="item.name" :skill="item" />
				</div>
				<p class="skill-section-sub skill-index-foot">
					Using Aviary? It installs these skills for every CLI it finds and runs
					them through <a :href="withBase('/guide/projects')">Code Loop</a>. See
					<a :href="withBase('/guide/skills/')">Skills for providers</a> for the details.
				</p>
			</div>
		</section>
	</div>
</template>
