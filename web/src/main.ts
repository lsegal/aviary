import { createPinia } from "pinia";
import { createApp } from "vue";
import App from "./App.vue";
import { openExternalLinksOutsideApp } from "./lib/externalLinks";
import { registerServiceWorker } from "./lib/serviceWorker";
import router from "./router";
import "./style.css";

const app = createApp(App);
app.use(createPinia());
app.use(router);
app.mount("#app");

openExternalLinksOutsideApp();
registerServiceWorker();
