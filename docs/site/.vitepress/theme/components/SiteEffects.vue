<script setup lang="ts">
import { onMounted, onUnmounted } from "vue";

let io: IntersectionObserver | null = null;
let mo: MutationObserver | null = null;

function bindReveal(root: ParentNode = document) {
	if (!io) return;
	root.querySelectorAll(".reveal").forEach((el) => {
		const node = el as HTMLElement;
		if (node.dataset.revealBound === "true") return;
		node.dataset.revealBound = "true";
		io?.observe(node);
	});
}

function initReveal() {
	io = new IntersectionObserver(
		(entries) => {
			entries.forEach((e) => {
				if (e.isIntersecting) {
					e.target.classList.add("in");
					io?.unobserve(e.target);
				}
			});
		},
		{ threshold: 0.05, rootMargin: "0px 0px -24px 0px" },
	);

	bindReveal();

	mo = new MutationObserver((records) => {
		for (const record of records) {
			record.addedNodes.forEach((node) => {
				if (!(node instanceof HTMLElement)) return;
				if (node.matches(".reveal")) {
					bindReveal(node.parentElement ?? document);
					return;
				}
				bindReveal(node);
			});
		}
	});

	mo.observe(document.body, {
		childList: true,
		subtree: true,
	});
}

onMounted(() => {
	requestAnimationFrame(() => setTimeout(initReveal, 150));
});

onUnmounted(() => {
	io?.disconnect();
	mo?.disconnect();
});
</script>

<template>
	<div />
</template>
