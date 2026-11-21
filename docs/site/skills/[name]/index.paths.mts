import { loadSkills } from "../../.vitepress/skills/source.mts";

export default {
	watch: [
		"../../../../codingskills.go",
		"../../../../.agents/skills/*/landing.yaml",
	],
	paths() {
		return loadSkills().map((skill) => ({
			params: { name: skill.name },
		}));
	},
};
