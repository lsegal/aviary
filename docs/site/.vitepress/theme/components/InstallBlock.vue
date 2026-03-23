<template>
  <div class="install-block">
    <span class="install-prompt" aria-hidden="true">$</span>
    <span class="install-command">{{ command }}</span>
    <button class="install-copy" :class="{ copied }" @click="copy" :title="copied ? 'Copied!' : 'Copy'">
      <svg v-if="!copied" xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="9" y="9" width="13" height="13" rx="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg>
      <svg v-else xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="20 6 9 17 4 12"/></svg>
    </button>
  </div>
</template>

<script setup lang="ts">
import { ref, computed } from "vue";

const isWindows = computed(() => {
  if (typeof navigator === "undefined") return false;
  return /windows/i.test(navigator.platform || navigator.userAgent);
});

const command = computed(() =>
  isWindows.value
    ? "iwr https://aviary.bot/install.ps1 | iex"
    : "curl -fsSL https://aviary.bot/install.sh | sh"
);

const copied = ref(false);

function copy() {
  navigator.clipboard?.writeText(command.value).then(() => {
    copied.value = true;
    setTimeout(() => (copied.value = false), 2000);
  });
}
</script>
