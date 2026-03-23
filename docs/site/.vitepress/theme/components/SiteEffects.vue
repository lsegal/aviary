<script setup lang="ts">
import { onMounted, onUnmounted } from "vue";

let io: IntersectionObserver | null = null;

function initReveal() {
	const sel =
		".panel-card, .detail-card, .comparison-card, .section-eyebrow, .section-heading, .section-subheading";

	io = new IntersectionObserver(
		(entries) => {
			entries.forEach((e) => {
				if (e.isIntersecting) {
					e.target.classList.add("sr-visible");
					io!.unobserve(e.target);
				}
			});
		},
		{ threshold: 0.05, rootMargin: "0px 0px -24px 0px" },
	);

	document.querySelectorAll(sel).forEach((el, i) => {
		el.classList.add("sr");
		(el as HTMLElement).style.setProperty("--sr-delay", `${(i % 5) * 65}ms`);
		io!.observe(el);
	});
}

onMounted(() => {
	requestAnimationFrame(() => setTimeout(initReveal, 150));
});

onUnmounted(() => {
	io?.disconnect();
});
</script>

<template>
	<div />
</template>
