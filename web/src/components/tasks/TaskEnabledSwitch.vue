<template>
	<SwitchRoot :checked="enabled" :disabled="busy" :aria-label="`${task} enabled`" data-testid="task-enabled"
		class="relative inline-flex h-5 w-9 shrink-0 items-center rounded-full transition-colors focus:outline-none disabled:opacity-60"
		:class="enabled ? 'bg-blue-600' : 'bg-gray-200 dark:bg-gray-700'"
		@click.stop @update:checked="emit('update', $event)">
		<SwitchThumb class="inline-block h-4 w-4 transform rounded-full bg-white shadow transition-transform"
			:class="enabled ? 'translate-x-[110%]' : 'translate-x-[10%]'" />
	</SwitchRoot>
</template>

<script setup lang="ts">
import { SwitchRoot, SwitchThumb } from "radix-vue";

defineProps<{ task: string; enabled: boolean; busy: boolean }>();
const emit = defineEmits<{ update: [enabled: boolean] }>();
</script>
