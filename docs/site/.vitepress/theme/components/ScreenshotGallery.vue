<script setup lang="ts">
import { useData } from "vitepress";
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";

interface ScreenshotItem {
	src?: string;
	lightSrc?: string;
	darkSrc?: string;
	alt: string;
	title: string;
	description: string;
	featured?: boolean;
}

const props = withDefaults(
	defineProps<{
		items: ScreenshotItem[];
		compact?: boolean;
	}>(),
	{
		compact: false,
	},
);

const activeIndex = ref<number | null>(null);
const { isDark } = useData();

const activeItem = computed(() => {
	if (activeIndex.value === null) return null;
	const item = props.items[activeIndex.value] ?? null;
	return item ? withResolvedSrc(item) : null;
});

function resolveSrc(item: ScreenshotItem) {
	if (isDark.value) {
		return item.darkSrc ?? item.lightSrc ?? item.src ?? "";
	}
	return item.lightSrc ?? item.darkSrc ?? item.src ?? "";
}

function withResolvedSrc(item: ScreenshotItem) {
	return {
		...item,
		resolvedSrc: resolveSrc(item),
	};
}

function open(index: number) {
	activeIndex.value = index;
}

function close() {
	activeIndex.value = null;
}

function onKeydown(event: KeyboardEvent) {
	if (event.key === "Escape") {
		close();
	}
}

watch(activeIndex, (value) => {
	if (typeof document === "undefined") return;
	document.body.style.overflow = value === null ? "" : "hidden";
});

onMounted(() => {
	window.addEventListener("keydown", onKeydown);
});

onBeforeUnmount(() => {
	window.removeEventListener("keydown", onKeydown);
	if (typeof document !== "undefined") {
		document.body.style.overflow = "";
	}
});
</script>

<template>
	<div class="docs-shot-grid" :class="{ 'docs-shot-grid-compact': compact }">
		<button
			v-for="(item, index) in items"
			:key="item.lightSrc ?? item.darkSrc ?? item.src ?? `${item.title}-${index}`"
			type="button"
			class="docs-shot-card"
			:class="{ 'docs-shot-featured': item.featured }"
			@click="open(index)"
		>
			<div class="docs-shot-frame">
				<img :src="resolveSrc(item)" :alt="item.alt" loading="lazy" />
			</div>
			<div class="docs-shot-copy">
				<h3>{{ item.title }}</h3>
				<p>{{ item.description }}</p>
				<span class="docs-shot-zoom">Click to expand</span>
			</div>
		</button>
	</div>

	<Teleport to="body">
		<div
			v-if="activeItem"
			class="docs-shot-lightbox"
			role="dialog"
			aria-modal="true"
			:aria-label="activeItem.title"
			@click="close"
		>
			<button
				type="button"
				class="docs-shot-close"
				aria-label="Close screenshot"
				@click.stop="close"
			>
				Close
			</button>
			<figure class="docs-shot-modal" @click.stop>
				<img :src="activeItem.resolvedSrc" :alt="activeItem.alt" />
				<figcaption>
					<h3>{{ activeItem.title }}</h3>
					<p>{{ activeItem.description }}</p>
				</figcaption>
			</figure>
		</div>
	</Teleport>
</template>

<style scoped>
.docs-shot-grid {
	display: grid;
	grid-template-columns: repeat(2, minmax(0, 1fr));
	gap: 1.5rem;
	margin-top: 2.35rem;
}

.docs-shot-grid-compact {
	grid-template-columns: repeat(2, minmax(0, 1fr));
}

.docs-shot-card {
	display: flex;
	flex-direction: column;
	gap: 0;
	padding: 0;
	border: 1px solid rgba(99, 62, 46, 0.16);
	border-radius: 24px;
	background:
		linear-gradient(180deg, var(--aviary-card-gradient-top), var(--aviary-card-gradient-bottom)),
		var(--aviary-card-base);
	box-shadow:
		0 16px 32px -14px rgba(93, 53, 31, 0.32),
		inset 0 1px 0 rgba(255, 255, 255, 0.45);
	text-align: left;
	cursor: zoom-in;
	overflow: hidden;
	transition:
		transform 220ms cubic-bezier(0.16, 1, 0.3, 1),
		box-shadow 220ms cubic-bezier(0.16, 1, 0.3, 1),
		border-color 220ms ease;
}

.docs-shot-card:hover {
	transform: translateY(-4px);
	border-color: rgba(184, 58, 29, 0.24);
	box-shadow:
		0 0 0 1px rgba(184, 58, 29, 0.16),
		0 20px 34px -14px rgba(93, 53, 31, 0.34),
		inset 0 1px 0 rgba(255, 255, 255, 0.55);
}

.dark .docs-shot-card {
	border-color: rgba(255, 223, 196, 0.12);
	background:
		linear-gradient(180deg, var(--aviary-card-dark-gradient-top), var(--aviary-card-dark-gradient-bottom)),
		var(--aviary-card-dark-base);
	box-shadow:
		0 18px 34px -14px rgba(0, 0, 0, 0.5),
		inset 0 1px 0 rgba(255, 244, 232, 0.06);
}

.dark .docs-shot-card:hover {
	border-color: rgba(225, 113, 68, 0.28);
	box-shadow:
		0 0 0 1px rgba(225, 113, 68, 0.2),
		0 22px 36px -14px rgba(0, 0, 0, 0.58),
		inset 0 1px 0 rgba(255, 244, 232, 0.08);
}

.docs-shot-featured {
	grid-column: span 1;
}

.docs-shot-frame {
	padding: 1rem;
	padding-bottom: 0;
}

.docs-shot-frame img {
	display: block;
	width: 100%;
	height: auto;
	aspect-ratio: 16 / 10;
	object-fit: cover;
	border-radius: 16px;
	border: 1px solid rgba(99, 62, 46, 0.1);
	background: rgba(255, 255, 255, 0.94);
}

.dark .docs-shot-frame img {
	border-color: rgba(255, 223, 196, 0.08);
	background: rgba(24, 17, 15, 0.96);
}

.docs-shot-copy {
	padding: 1rem 1.1rem 1.15rem;
}

.docs-shot-copy h3 {
	margin: 0 0 0.4rem;
	font-size: 1rem;
}

.docs-shot-copy p {
	margin: 0;
	color: var(--vp-c-text-2);
}

.docs-shot-zoom {
	display: inline-block;
	margin-top: 0.85rem;
	font-size: 0.78rem;
	font-weight: 700;
	letter-spacing: 0.08em;
	text-transform: uppercase;
	color: var(--vp-c-brand-1);
}

.docs-shot-lightbox {
	position: fixed;
	inset: 0;
	z-index: 100;
	display: flex;
	align-items: center;
	justify-content: center;
	padding: 2rem;
	background: rgba(17, 12, 10, 0.82);
	backdrop-filter: blur(10px);
}

.docs-shot-close {
	position: absolute;
	top: 1.25rem;
	right: 1.25rem;
	border: 1px solid rgba(255, 255, 255, 0.14);
	border-radius: 999px;
	padding: 0.55rem 0.9rem;
	background: rgba(20, 14, 12, 0.72);
	color: #fff7ef;
	font: inherit;
	cursor: pointer;
}

.docs-shot-modal {
	margin: 0;
	width: min(1200px, 100%);
	max-height: 100%;
	overflow: auto;
	border-radius: 28px;
	border: 1px solid rgba(255, 223, 196, 0.16);
	background: rgba(21, 15, 13, 0.96);
	box-shadow: 0 26px 64px -20px rgba(0, 0, 0, 0.55);
}

.docs-shot-modal img {
	display: block;
	width: 100%;
	height: auto;
}

.docs-shot-modal figcaption {
	padding: 1rem 1.15rem 1.2rem;
	color: #f6e6d8;
}

.docs-shot-modal h3 {
	margin: 0 0 0.35rem;
	color: inherit;
}

.docs-shot-modal p {
	margin: 0;
	color: rgba(246, 230, 216, 0.78);
}

@media (max-width: 960px) {
	.docs-shot-grid,
	.docs-shot-grid-compact {
		grid-template-columns: 1fr;
	}

	.docs-shot-featured {
		grid-column: span 1;
	}

	.docs-shot-lightbox {
		padding: 1rem;
	}
}
</style>
