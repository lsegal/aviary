<template>
  <a
    :href="releaseUrl"
    class="header-release-badge"
    target="_blank"
    rel="noreferrer"
    :aria-label="`View Aviary ${releaseVersion} release on GitHub`"
  >
    <img :src="badgeSrc" :alt="`Aviary release ${releaseVersion}`" />
  </a>
</template>

<script setup lang="ts">
import pkg from "../../../../../package.json";

const releaseVersion = `v${pkg.version}`;
const releaseUrl = `https://github.com/lsegal/aviary/releases/tag/${releaseVersion}`;
const badgeSrc =
	`https://img.shields.io/static/v1?label=release&message=${encodeURIComponent(releaseVersion)}` +
	"&style=flat&color=white&labelColor=white";
</script>
