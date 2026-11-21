<template>
  <span
    class="typing-indicator inline-flex items-center gap-1 rounded-xl bg-gray-100 px-4 py-3 text-gray-500 dark:bg-gray-800 dark:text-gray-400"
    role="status" aria-label="Assistant is working">
    <span v-for="i in 3" :key="i" class="typing-dot h-2 w-2 rounded-full bg-current" :style="{ '--i': i - 1 }" />
  </span>
</template>

<style scoped>
.typing-dot {
  opacity: 0.35;
  animation: typing-dot-wave 1.2s ease-in-out infinite;
  animation-delay: calc(var(--i) * 0.16s);
}

@keyframes typing-dot-wave {
  0%,
  60%,
  100% {
    opacity: 0.35;
    transform: translateY(0) scale(0.85);
  }
  30% {
    opacity: 1;
    transform: translateY(-2px) scale(1);
  }
}

@media (prefers-reduced-motion: reduce) {
  .typing-dot {
    animation: none;
    opacity: 0.6;
    transform: none;
  }
}
</style>
