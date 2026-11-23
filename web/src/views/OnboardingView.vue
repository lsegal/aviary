<template>
  <div class="h-full overflow-y-auto bg-white dark:bg-gray-950" data-testid="onboarding">
    <SetupWizard @skip="skip" />
  </div>
</template>

<script setup lang="ts">
import { useRouter } from "vue-router";
import SetupWizard from "../components/SetupWizard.vue";
import { skipOnboarding } from "../lib/onboarding";

const router = useRouter();

function skip() {
	skipOnboarding();
	void router.replace("/system/overview");
}
</script>
