<script setup lang="ts">
import {
	AdjustmentsHorizontalIcon,
	ChartBarSquareIcon,
	ChatBubbleLeftRightIcon,
	ClockIcon,
	CommandLineIcon,
	GlobeAltIcon,
	WrenchScrewdriverIcon,
} from "@heroicons/vue/24/outline";
import { computed } from "vue";

const props = defineProps<{
	name: "chat" | "clock" | "chart" | "tools" | "controls" | "cli" | "browser";
}>();

const icon = computed(() => {
	switch (props.name) {
		case "chat":
			return ChatBubbleLeftRightIcon;
		case "clock":
			return ClockIcon;
		case "chart":
			return ChartBarSquareIcon;
		case "tools":
			return WrenchScrewdriverIcon;
		case "controls":
			return AdjustmentsHorizontalIcon;
		case "cli":
			return CommandLineIcon;
		case "browser":
			return GlobeAltIcon;
	}
});
</script>

<template>
	<div class="panel-icon" aria-hidden="true">
		<component :is="icon" />
	</div>
</template>
