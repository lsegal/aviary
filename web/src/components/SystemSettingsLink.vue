<template>
  <router-link to="/system/settings" data-testid="system-settings-link" aria-label="Settings" title="Settings"
    :class="HEADER_ICON_CLS">
    <Cog6ToothIcon aria-hidden="true" class="h-5 w-5" />
  </router-link>
</template>

<script setup lang="ts">
// The gear at the top right of every System page header, linking to the
// global Settings page the sidebar gear menu also opens (#932). Styled like
// the channel view's settings link.
import Cog6ToothIcon from "@heroicons/vue/24/outline/Cog6ToothIcon";
import { HEADER_ICON_CLS } from "../lib/headerActions";
</script>
