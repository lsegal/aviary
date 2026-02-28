<template>
  <div class="flex min-h-screen">
    <!-- Sidebar -->
    <nav class="flex w-52 flex-col border-r border-gray-800 bg-gray-900 p-4">
      <div class="mb-6 flex items-center gap-2">
        <span class="text-lg font-bold text-white">🐦 Aviary</span>
      </div>
      <router-link
        v-for="link in links"
        :key="link.to"
        :to="link.to"
        class="mb-1 rounded-lg px-3 py-2 text-sm font-medium text-gray-400 hover:bg-gray-800 hover:text-white"
        active-class="bg-gray-800 text-white"
      >
        {{ link.label }}
      </router-link>
      <div class="mt-auto">
        <button
          class="w-full rounded-lg px-3 py-2 text-left text-sm font-medium text-gray-500 hover:text-red-400"
          @click="auth.logout(); $router.push('/login')"
        >
          Log out
        </button>
      </div>
    </nav>

    <!-- Main -->
    <main class="flex flex-1 flex-col overflow-hidden">
      <slot />
    </main>
  </div>
</template>

<script setup lang="ts">
import { useAuthStore } from '../stores/auth'

const auth = useAuthStore()
const links = [
  { to: '/chat', label: 'Chat' },
  { to: '/agents', label: 'Agents' },
  { to: '/tasks', label: 'Tasks' },
  { to: '/sessions', label: 'Sessions' },
]
</script>
