import DefaultTheme from "vitepress/theme";
import { h } from "vue";
import ConversationStopDemo from "./components/ConversationStopDemo.vue";
import LandingHomePage from "./components/LandingHomePage.vue";
import SiteEffects from "./components/SiteEffects.vue";
import "./custom.css";

export default {
	...DefaultTheme,
	Layout() {
		return h(DefaultTheme.Layout, null, {
			"layout-bottom": () => h(SiteEffects),
		});
	},
	enhanceApp({ app }) {
		DefaultTheme.enhanceApp?.({ app });
		app.component("ConversationStopDemo", ConversationStopDemo);
		app.component("LandingHomePage", LandingHomePage);
	},
};
