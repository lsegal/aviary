import { loadSkills, type Skill } from "../.vitepress/skills/source.mts";

declare const data: Skill[];
export { data };

export default {
	watch: [
		"../../../codingskills.go",
		"../../../.agents/skills/*/SKILL.md",
		"../../../.agents/skills/*/landing.yaml",
		"../../../.agents/skills/*/agents/openai.yaml",
	],
	load: loadSkills,
};
