<script setup lang="ts">
import { withBase } from "vitepress";
import type { Skill } from "../../../skills/source.mts";

defineProps<{ skill: Skill }>();
</script>

<template>
	<a class="skill-card" :href="withBase(`/skills/${skill.name}/`)">
		<span class="skill-card-eyebrow">{{ skill.eyebrow }}</span>
		<span class="skill-card-title">{{ skill.displayName }}</span>
		<span class="skill-card-text">{{ skill.shortDescription }}</span>
		<span class="skill-card-foot">
			<span class="skill-tag">{{ skill.name }}</span>
			<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"
				aria-hidden="true">
				<path d="M5 12h14M13 5l7 7-7 7" />
			</svg>
		</span>
	</a>
</template>
