/**
 * Opens every link to another origin in a new browsing context. In the
 * installed app that is the system browser, so leaving Aviary never replaces
 * the standalone window, which has no address bar or back button to return.
 */
export function openExternalLinksOutsideApp(): void {
	document.addEventListener("click", (event) => {
		if (event.defaultPrevented || event.button !== 0) return;
		const target = event.target as Element | null;
		const link = target?.closest?.("a[href]") as HTMLAnchorElement | null;
		if (!link || link.target || link.hasAttribute("download")) return;
		const url = new URL(link.href, location.href);
		if (url.origin === location.origin || !/^https?:$/.test(url.protocol)) {
			return;
		}
		event.preventDefault();
		window.open(url.href, "_blank", "noopener,noreferrer");
	});
}
