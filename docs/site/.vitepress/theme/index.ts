import { h } from "vue";
import DefaultTheme from "vitepress/theme";
import FeatureIcon from "./components/FeatureIcon.vue";
import HeroActions from "./components/HeroActions.vue";
import MessagingLogo from "./components/MessagingLogo.vue";
import SiteEffects from "./components/SiteEffects.vue";
import "./custom.css";

export default {
	...DefaultTheme,
	Layout() {
		return h(DefaultTheme.Layout, null, {
			"home-hero-after": () => h(HeroActions),
			"layout-bottom": () => h(SiteEffects),
		});
	},
	enhanceApp({ app }) {
		DefaultTheme.enhanceApp?.({ app });
		app.component("FeatureIcon", FeatureIcon);
		app.component("MessagingLogo", MessagingLogo);
	},
};
