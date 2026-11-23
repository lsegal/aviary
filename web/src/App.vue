<template>
  <div
    v-if="auth.serverUnavailable"
    role="status"
    data-testid="server-unavailable"
    class="fixed inset-0 z-50 flex items-center justify-center bg-gray-50 dark:bg-gray-950"
  >
    <div class="w-full max-w-sm rounded-xl border border-gray-200 bg-white p-8 text-center shadow-xl dark:border-gray-800 dark:bg-gray-900">
      <h1 class="mb-2 text-2xl font-bold text-gray-900 dark:text-white">Aviary</h1>
      <p class="text-sm text-gray-500 dark:text-gray-400">Server unavailable, reconnecting…</p>
    </div>
  </div>
  <router-view />
  <UnsavedChangesDialog />
</template>

<script setup lang="ts">
import UnsavedChangesDialog from "./components/UnsavedChangesDialog.vue";
import { useAuthStore } from "./stores/auth";

const auth = useAuthStore();
</script>
