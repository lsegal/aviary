import DefaultTheme from "vitepress/theme";
import FeatureIcon from "./components/FeatureIcon.vue";
import MessagingLogo from "./components/MessagingLogo.vue";
import "./custom.css";

export default {
	...DefaultTheme,
	enhanceApp({ app }) {
		DefaultTheme.enhanceApp?.({ app });
		app.component("FeatureIcon", FeatureIcon);
		app.component("MessagingLogo", MessagingLogo);
	},
};
