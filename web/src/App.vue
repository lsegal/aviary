<template>
  <router-view />
  <UnsavedChangesDialog />
</template>

<script setup lang="ts">
import UnsavedChangesDialog from "./components/UnsavedChangesDialog.vue";
</script>
