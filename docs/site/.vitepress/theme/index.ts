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

		if (typeof window !== "undefined") {
			function typeInstallCommand(el: Element) {
				if (el.classList.contains('typing-done') || el.classList.contains('typing-in-progress')) return
				let text = el.getAttribute('data-install-text') || el.textContent?.trim() || ''
				// Ensure client shows the correct command for the user's platform (use as fallback only)
				let isWindows = false
				if (typeof navigator !== 'undefined') {
					const uaPlatform = (navigator as any).userAgentData?.platform || navigator.platform || navigator.userAgent || ''
					isWindows = /win/i.test(String(uaPlatform))
				}
				// If component didn't provide client-side text, use a platform-appropriate fallback
				if (!text) {
					text = isWindows ? 'iwr https://aviary.bot/install.ps1 | iex' : 'curl -fsSL https://aviary.bot/install.sh | sh'
				}
				if (!text) return
				el.classList.add('typing-in-progress')
				// reserve space to avoid layout shift: measure text width and set minWidth
				const temp = document.createElement('span')
				temp.style.position = 'absolute'
				temp.style.visibility = 'hidden'
				temp.style.whiteSpace = 'nowrap'
				temp.style.fontFamily = getComputedStyle(el).fontFamily || ''
				temp.style.fontSize = getComputedStyle(el).fontSize || ''
				temp.textContent = text
				document.body.appendChild(temp)
				const measured = temp.getBoundingClientRect().width
				document.body.removeChild(temp)
				el.setAttribute('style', (el.getAttribute('style') || '') + `;min-width:${Math.ceil(measured)}px;display:inline-block;`)
				el.textContent = ''
				const caret = document.createElement('span')
				caret.className = 'install-caret'
				el.appendChild(caret)

				let i = 0
				const speed = 30 // 50% slower than original (20ms -> 30ms)
				function step() {
					if (i < text.length) {
						caret.insertAdjacentText('beforebegin', text[i])
						i++
							setTimeout(step, speed)
					} else {
						el.classList.remove('typing-in-progress')
						el.classList.add('typing-done')
						caret.remove()
					}
				}
					// pre-flash caret for ~0.5s before typing
					caret.classList.add('pre-flash')
					setTimeout(() => {
						caret.classList.remove('pre-flash')
						step()
					}, 500)
			}

			function runTypingOnce() {
				const els = Array.from(document.querySelectorAll('.install-command'))
				const pending = els.filter((el) => !el.classList.contains('typing-done') && !el.classList.contains('typing-in-progress'))
				if (pending.length) {
					pending.forEach(typeInstallCommand)
					return true
				}
				return false
			}

			// Try immediately; if elements aren't rendered yet, retry a few times
			let retries = 0
			const maxRetries = 20
			function waitAndRun() {
				if (runTypingOnce()) return
				if (retries++ < maxRetries) setTimeout(waitAndRun, 150)
			}
			if (document.readyState === 'complete' || document.readyState === 'interactive') {
				waitAndRun()
			} else {
				document.addEventListener('DOMContentLoaded', waitAndRun)
			}

			// Try to hook router navigation to re-run typing after client-side route changes
			setTimeout(() => {
				const router = (app as any)._context && (app as any)._context.router
				if (router && router.afterEach) router.afterEach(() => setTimeout(waitAndRun, 120))
			}, 0)
		}
	},
};
