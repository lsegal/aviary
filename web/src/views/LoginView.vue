<template>
  <div class="flex min-h-screen items-center justify-center bg-gray-50 dark:bg-gray-950">
    <div class="w-full max-w-sm rounded-xl border border-gray-200 bg-white p-8 shadow-xl dark:border-gray-800 dark:bg-gray-900">
      <h1 class="mb-2 text-2xl font-bold text-gray-900 dark:text-white">Aviary</h1>
      <p class="mb-4 text-sm text-gray-500 dark:text-gray-400">Enter your authentication token to continue.</p>
      <p class="mb-6 rounded-lg bg-gray-100 px-3 py-2 text-xs text-gray-500 dark:bg-gray-800 dark:text-gray-400">
        Run <code class="font-mono font-semibold text-gray-700 dark:text-gray-200">aviary token</code> in your terminal to see your token.
      </p>
      <form @submit.prevent="submit">
        <label class="mb-1 block text-xs font-medium text-gray-600 dark:text-gray-400" for="token">Token</label>
        <input
          id="token"
          v-model="tokenInput"
          type="password"
          autocomplete="current-password"
          placeholder="aviary_..."
          class="mb-4 w-full rounded-lg border border-gray-300 bg-white px-4 py-2 text-sm text-gray-900 placeholder-gray-400 focus:border-blue-500 focus:outline-none dark:border-gray-700 dark:bg-gray-800 dark:text-white dark:placeholder-gray-500"
        />
        <p v-if="errorMsg" class="mb-3 text-sm text-red-500 dark:text-red-400">{{ errorMsg }}</p>
        <button
          type="submit"
          :disabled="loading"
          class="w-full rounded-lg bg-blue-600 py-2 text-sm font-semibold text-white hover:bg-blue-500 disabled:opacity-50"
        >
          {{ loading ? 'Logging in…' : 'Log in' }}
        </button>
      </form>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref } from "vue";
import { useRouter } from "vue-router";
import { useAgentsStore } from "../stores/agents";
import { useAuthStore } from "../stores/auth";

const auth = useAuthStore();
const agentsStore = useAgentsStore();
const router = useRouter();
const tokenInput = ref("");
const loading = ref(false);
const errorMsg = ref("");

async function submit() {
	loading.value = true;
	errorMsg.value = "";
	const ok = await auth.login(tokenInput.value.trim());
	loading.value = false;
	if (ok) {
		await agentsStore.fetchAgents();
		await router.push(agentsStore.agents.length === 0 ? "/overview" : "/chat");
	} else {
		errorMsg.value = "Invalid token. Please try again.";
	}
}
</script>
