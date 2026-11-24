<template>
  <div class="flex h-full flex-col bg-white dark:bg-gray-950 lg:flex-row">
    <header class="border-b border-gray-200 bg-gray-50 px-4 py-2 dark:border-gray-800 dark:bg-gray-900 lg:hidden">
      <div class="flex items-center justify-between gap-3">
        <div class="flex items-center gap-2">
          <img :src="logoUrl" alt="Aviary logo" class="h-8 w-8 object-contain" />
          <span class="text-lg font-bold text-gray-900 dark:text-white">Aviary</span>
          <div class="flex items-center gap-1">
            <a
              v-for="link in brandLinks"
              :key="link.label"
              :href="link.href"
              :aria-label="link.label"
              target="_blank"
              rel="noreferrer"
              class="inline-flex h-8 w-8 items-center justify-center rounded-lg text-gray-500 transition hover:bg-gray-200 hover:text-gray-900 dark:text-gray-400 dark:hover:bg-gray-800 dark:hover:text-white">
              <component
                v-if="link.iconComponent"
                :is="link.iconComponent"
                aria-hidden="true"
                class="h-4 w-4" />
              <svg
                v-else
                viewBox="0 0 24 24"
                fill="currentColor"
                aria-hidden="true"
                class="h-4 w-4">
                <path :d="link.iconPath" />
              </svg>
            </a>
          </div>
        </div>
        <div class="flex shrink-0 items-center gap-1" data-testid="mobile-header-status">
          <SidebarGearMenu v-if="!minimal" :restart-disabled="upgrading || restartPending" :restart-pending="restartPending"
            @restart="triggerRestart()" />
          <VersionIndicator v-if="!minimal" :version="version" :latest-version="latestVersion" :status="status"
            :upgrade-available="upgradeAvailable" :disabled="upgradeDisabled" @upgrade="openUpgradeDialog()" />
          <ConnectionIndicator :status="status" @retry="reconnect()" />
        </div>
      </div>
      <div class="relative mt-2">
        <UpdateStatusBanner :restart="restart" class="mb-3" @cancel="cancelRestart()" />
        <div v-if="upgradeAvailable && !minimal"
          class="mb-3 rounded-xl border border-yellow-300 bg-yellow-50 px-3 py-2 text-sm text-yellow-900 dark:border-yellow-700 dark:bg-yellow-950/40 dark:text-yellow-100">
          <div class="flex items-center justify-between gap-3">
            <span>New version available:
              <a v-if="releaseUrl" :href="releaseUrl" target="_blank" rel="noreferrer" class="underline">{{ shortVersion(latestVersion) }}</a>
              <template v-else>{{ shortVersion(latestVersion) }}</template>
            </span>
            <button type="button"
              class="rounded-lg bg-yellow-500 px-3 py-1.5 text-xs font-semibold text-yellow-950 transition hover:bg-yellow-400 disabled:opacity-60"
              :disabled="upgrading || restartPending || !upgradeSupported" @click="openUpgradeDialog()">
              {{ upgrading ? "Upgrading..." : "Upgrade" }}
            </button>
          </div>
          <div v-if="versionMessage" class="mt-1 text-xs opacity-80" data-testid="version-message">{{ versionMessage }}</div>
          <div v-if="autoUpdateWaiting" class="mt-1 text-xs" data-testid="auto-update-status">{{ autoUpdateWaiting }}</div>
          <AutoUpdateError :auto="restart?.autoUpdate" :disabled="upgradeDisabled"
            @retry="triggerUpgrade('now')" @remove-lock="triggerUpgrade('now', { removeStaleLock: true })" />
        </div>
        <div v-else-if="updateNotice" data-testid="update-notice"
          class="mb-3 rounded-xl border border-green-300 bg-green-50 px-3 py-2 text-xs text-green-900 dark:border-green-700 dark:bg-green-950/40 dark:text-green-100">
          {{ updateNotice }}
        </div>
        <div v-else-if="noCheckout" data-testid="update-no-checkout"
          class="mb-3 rounded-xl border border-gray-300 bg-gray-50 px-3 py-2 text-xs text-gray-700 dark:border-gray-700 dark:bg-gray-800/60 dark:text-gray-300">
          <div class="font-medium">Dev build — no checkout found</div>
          <div v-if="versionMessage" class="mt-1 opacity-80">{{ versionMessage }}</div>
        </div>
        <MobileMainNav :items="mobileMainItems" />
      </div>
    </header>

    <div v-if="mobileChildren.length"
      class="border-b border-gray-200 bg-white px-4 py-2 dark:border-gray-800 dark:bg-gray-950 lg:hidden">
      <nav data-testid="mobile-sub-nav" class="group/row relative flex gap-2 overflow-x-auto pb-1" :class="mobileAdd ? 'pr-9' : ''">
        <router-link v-for="child in mobileChildren" :key="child.key" :to="child.to"
          :title="child.paused ? 'Paused' : child.disabled ? 'Disabled' : undefined" :data-paused="child.paused || undefined"
          :data-disabled="child.disabled || undefined"
          :class="[mobileSubnavClass(child), 'inline-flex items-center gap-1.5', child.paused || child.disabled ? 'italic opacity-60' : '']">
          <SystemPageIcon v-if="systemActive" :page-key="child.key" class="h-4 w-4" /><span v-if="child.paused"
            data-testid="mobile-task-paused" class="inline-flex text-amber-600 dark:text-amber-400"><PauseIcon aria-hidden="true" class="h-3.5 w-3.5" /></span><span v-if="child.disabled"
            data-testid="mobile-channel-disabled" class="inline-flex text-gray-400 dark:text-gray-500"><NoSymbolIcon aria-hidden="true" class="h-3.5 w-3.5" /></span>{{ child.label }}<ProjectRunningBadge
            :running="child.running ?? 0" class="ml-1.5" />
        </router-link>
        <SidebarRowAction v-if="mobileAdd" :label="rowAddLabel(mobileAdd.page)" data-testid="mobile-row-action" @click="openAdd(mobileAdd.page, mobileAdd.agent)" />
      </nav>
    </div>

    <!-- While collapsed, resting the pointer on the rail peeks the full
         sidebar as an overlay above the rail and main content. -->
    <nav v-if="!sidebarCollapsed || sidebarPeek" ref="sidebarEl" data-testid="sidebar"
      :data-peek="sidebarPeek || undefined" :style="{ width: `${sidebarWidth}px` }"
      class="hidden shrink-0 flex-col border-r border-gray-200 bg-gray-50 p-4 dark:border-gray-800 dark:bg-gray-900 lg:flex"
      :class="sidebarPeek ? 'fixed inset-y-0 left-0 z-40 shadow-xl' : 'relative'"
      @mouseenter="cancelSidebarPeekClose" @mouseleave="closeSidebarPeekSoon" @click="closeSidebarPeekOnLink"
      @focusout="closeSidebarPeekOnFocusOut">
      <div class="mb-6 flex items-center gap-2">
        <img :src="logoUrl" alt="Aviary logo" class="h-8 w-8 shrink-0 object-contain" />
        <span class="min-w-0 flex-1 truncate text-lg font-bold text-gray-900 dark:text-white" data-testid="sidebar-title">Aviary</span>
        <div class="flex items-center gap-1">
          <a
            v-for="link in brandLinks"
            :key="link.label"
            :href="link.href"
            :aria-label="link.label"
            target="_blank"
            rel="noreferrer"
            class="inline-flex h-8 w-8 items-center justify-center rounded-lg text-gray-500 transition hover:bg-gray-200 hover:text-gray-900 dark:text-gray-400 dark:hover:bg-gray-800 dark:hover:text-white">
            <component
              v-if="link.iconComponent"
              :is="link.iconComponent"
              aria-hidden="true"
              class="h-4 w-4" />
            <svg
              v-else
              viewBox="0 0 24 24"
              fill="currentColor"
              aria-hidden="true"
              class="h-4 w-4">
              <path :d="link.iconPath" />
            </svg>
          </a>
          <button v-if="sidebarPeek" type="button" data-testid="sidebar-pin" aria-label="Expand sidebar"
            :title="`Expand sidebar (${shortcutLabel})`"
            class="inline-flex h-8 w-8 items-center justify-center rounded-lg text-gray-500 transition hover:bg-gray-200 hover:text-gray-900 dark:text-gray-400 dark:hover:bg-gray-800 dark:hover:text-white"
            @click="sidebar.toggle()">
            <ChevronDoubleRightIcon aria-hidden="true" class="h-4 w-4" />
          </button>
          <button v-else type="button" data-testid="sidebar-collapse" aria-label="Collapse sidebar"
            :title="`Collapse sidebar (${shortcutLabel})`"
            class="inline-flex h-8 w-8 items-center justify-center rounded-lg text-gray-500 transition hover:bg-gray-200 hover:text-gray-900 dark:text-gray-400 dark:hover:bg-gray-800 dark:hover:text-white"
            @click="sidebar.toggle()">
            <ChevronDoubleLeftIcon aria-hidden="true" class="h-4 w-4" />
          </button>
        </div>
      </div>
      <div class="min-h-0 flex-1 space-y-1 overflow-y-auto" data-testid="sidebar-nav">
        <div v-if="soleAgent" class="mb-3 space-y-1" data-testid="sidebar-sole-agent" :data-agent="soleAgent.name">
          <template v-for="child in soleAgent.children" :key="child.key">
            <div class="group/row relative" :data-testid="`sidebar-row-${child.key}`">
              <router-link :to="child.to"
                :class="[topLevelLinkClass(isChildActive(child)), 'flex items-center gap-2', hasRowAction(child) ? rowActionPad : '']">
                <span class="min-w-0 flex-1 truncate">{{ child.label }}</span>
                <ProjectRunningBadge :running="child.running ?? 0" />
              </router-link>
              <SidebarRowAction v-if="child.key === 'projects' && !minimal" label="Add project" data-testid="sidebar-add-project"
                :to="addProjectLink(soleAgent.name)" />
              <SidebarRowAction v-else-if="child.key === 'chat'" label="New chat" data-testid="sidebar-new-chat"
                :disabled="creatingChat" @click="newChat(soleAgent.name)" />
              <SidebarRowAction v-else-if="rowAddLabel(child.key)" :label="rowAddLabel(child.key)"
                @click="openAdd(child.key, soleAgent.name)" />
            </div>
            <ChatSessionList v-if="child.key === 'chat'" :agent="soleAgent.name" />
            <AgentChannelList v-if="child.key === 'channels'" :agent="soleAgent.name" />
            <AgentTaskList v-if="child.key === 'tasks'" :agent="soleAgent.name" />
            <AgentFileList v-if="child.key === 'files' && filesOpen" :agent="soleAgent.name" />
            <div v-if="child.key === 'projects'" class="space-y-1 pl-2" data-testid="sidebar-agent-projects">
              <router-link v-for="p in soleAgent.projects" :key="p.key" :to="p.to"
                :class="[childLinkClass(isChildActive(p), false), 'flex items-center gap-2']">
                <span class="min-w-0 flex-1 truncate">{{ p.label }}</span>
                <ProjectRunningBadge :running="p.running ?? 0" />
              </router-link>
            </div>
          </template>
        </div>
        <template v-else>
        <router-link to="/agents" :class="[topLevelLinkClass(false), 'flex items-center']" data-testid="sidebar-agents">
          Agents
        </router-link>
        <div class="mb-3 mt-1 space-y-1 pl-2" data-testid="sidebar-agent-list">
          <template v-for="a in agentNav" :key="a.name">
            <router-link :to="a.to" :data-agent="a.name"
              :class="[childLinkClass(a.active && !a.children.length, false), 'flex items-center gap-2', a.active ? 'font-semibold text-gray-950 dark:text-white' : '']">
              <span class="min-w-0 flex-1 truncate">{{ a.label }}</span>
              <span v-if="a.running > 0" data-testid="agent-running-badge"
                class="shrink-0 rounded-full bg-green-600 px-1.5 text-xs font-semibold text-white">{{ a.running }}</span>
            </router-link>
            <div v-if="a.children.length" class="space-y-1 border-l border-gray-200 pl-2 dark:border-gray-800" :data-testid="`sidebar-agent-${a.name}`">
              <template v-for="child in a.children" :key="child.key">
                <div class="group/row relative" :data-testid="`sidebar-row-${child.key}`">
                  <router-link :to="child.to"
                    :class="[childLinkClass(isChildActive(child), false), 'flex items-center gap-2', hasRowAction(child) ? rowActionPad : '']">
                    <span class="min-w-0 flex-1 truncate">{{ child.label }}</span>
                    <ProjectRunningBadge :running="child.running ?? 0" />
                  </router-link>
                  <SidebarRowAction v-if="child.key === 'projects' && !minimal" label="Add project" data-testid="sidebar-add-project"
                    :to="addProjectLink(a.name)" />
                  <SidebarRowAction v-else-if="child.key === 'chat'" label="New chat" data-testid="sidebar-new-chat"
                    :disabled="creatingChat" @click="newChat(a.name)" />
                  <SidebarRowAction v-else-if="rowAddLabel(child.key)" :label="rowAddLabel(child.key)"
                    @click="openAdd(child.key, a.name)" />
                </div>
                <ChatSessionList v-if="child.key === 'chat'" :agent="a.name" />
                <AgentChannelList v-if="child.key === 'channels'" :agent="a.name" />
                <AgentTaskList v-if="child.key === 'tasks'" :agent="a.name" />
                <AgentFileList v-if="child.key === 'files' && filesOpen" :agent="a.name" />
                <div v-if="child.key === 'projects'" class="space-y-1 pl-2" data-testid="sidebar-agent-projects">
                  <router-link v-for="p in a.projects" :key="p.key" :to="p.to"
                    :class="[childLinkClass(isChildActive(p), false), 'flex items-center gap-2']">
                    <span class="min-w-0 flex-1 truncate">{{ p.label }}</span>
                    <ProjectRunningBadge :running="p.running ?? 0" />
                  </router-link>
                </div>
              </template>
            </div>
          </template>
        </div>
        </template>

        <div v-if="!minimal" class="relative">
          <router-link to="/system/overview" :class="[topLevelLinkClass(systemHeaderActive), 'flex items-center pr-9']" data-testid="sidebar-system">
            System
          </router-link>
          <button type="button" data-testid="sidebar-system-toggle" :aria-expanded="!systemCollapsed"
            :aria-label="systemCollapsed ? 'Expand System' : 'Collapse System'"
            :title="systemCollapsed ? 'Expand System' : 'Collapse System'"
            class="absolute right-2 top-1/2 inline-flex h-5 w-5 -translate-y-1/2 items-center justify-center rounded-md text-gray-500 transition-colors hover:bg-gray-200 hover:text-gray-900 dark:text-gray-400 dark:hover:bg-gray-800 dark:hover:text-white"
            @click="systemCollapsed = !systemCollapsed">
            <span aria-hidden="true" class="inline-block transition-transform" :class="systemCollapsed ? '' : 'rotate-90'">›</span>
          </button>
        </div>
        <div v-if="!systemCollapsed && !minimal" class="mb-3 mt-1 space-y-1 pl-2" data-testid="sidebar-system-list">
          <router-link v-for="child in systemChildren" :key="child.key" :to="child.to"
            :data-testid="`sidebar-system-${child.key}`"
            :class="[childLinkClass(isChildActive(child), false), 'flex items-center gap-2']">
            <span class="min-w-0 flex-1 truncate">{{ child.label }}</span>
            <ProjectRunningBadge :running="child.running ?? 0" />
          </router-link>
        </div>
        <button v-if="hiddenNavCount" type="button" data-testid="sidebar-show-hidden" :aria-pressed="nav.showHidden.value"
          class="block w-full rounded-lg px-3 py-1.5 text-left text-xs text-gray-500 transition-colors hover:bg-gray-200 hover:text-gray-900 dark:text-gray-400 dark:hover:bg-gray-800 dark:hover:text-white"
          @click="nav.showHidden.value = !nav.showHidden.value">
          {{ nav.showHidden.value ? "Hide hidden pages" : `Show ${hiddenNavCount} hidden ${hiddenNavCount === 1 ? "page" : "pages"}` }}
        </button>
      </div>
      <div class="mt-auto flex flex-col gap-2">
        <UpdateStatusBanner :restart="restart" @cancel="cancelRestart()" />
        <div v-if="upgradeAvailable && !minimal"
          class="rounded-xl border border-yellow-300 bg-yellow-50 px-3 py-2 text-sm text-yellow-900 dark:border-yellow-700 dark:bg-yellow-950/40 dark:text-yellow-100">
          <div class="flex items-center justify-between gap-2">
            <span>New version available</span>
            <button type="button"
              class="rounded-lg bg-yellow-500 px-2.5 py-1 text-xs font-semibold text-yellow-950 transition hover:bg-yellow-400 disabled:opacity-60"
              :disabled="upgrading || restartPending || !upgradeSupported" @click="openUpgradeDialog()">
              {{ upgrading ? "Upgrading..." : "Upgrade" }}
            </button>
          </div>
          <div class="mt-1 text-xs">
            <a v-if="releaseUrl" :href="releaseUrl" target="_blank" rel="noreferrer" class="underline">{{ shortVersion(latestVersion) }}</a>
            <template v-else>{{ shortVersion(latestVersion) }}</template>
          </div>
          <div v-if="versionMessage" class="mt-1 text-xs opacity-80" data-testid="version-message">{{ versionMessage }}</div>
          <div v-if="autoUpdateWaiting" class="mt-1 text-xs" data-testid="auto-update-status">{{ autoUpdateWaiting }}</div>
          <AutoUpdateError :auto="restart?.autoUpdate" :disabled="upgradeDisabled"
            @retry="triggerUpgrade('now')" @remove-lock="triggerUpgrade('now', { removeStaleLock: true })" />
        </div>
        <div v-else-if="updateNotice" data-testid="update-notice"
          class="rounded-xl border border-green-300 bg-green-50 px-3 py-2 text-xs text-green-900 dark:border-green-700 dark:bg-green-950/40 dark:text-green-100">
          {{ updateNotice }}
        </div>
        <div v-else-if="noCheckout" data-testid="update-no-checkout"
          class="rounded-xl border border-gray-300 bg-gray-50 px-3 py-2 text-xs text-gray-700 dark:border-gray-700 dark:bg-gray-800/60 dark:text-gray-300">
          <div class="font-medium">Dev build — no checkout found</div>
          <div v-if="versionMessage" class="mt-1 opacity-80">{{ versionMessage }}</div>
        </div>
        <div class="flex items-center gap-2 px-1" data-testid="sidebar-footer">
          <VersionIndicator v-if="!minimal" :version="version" :latest-version="latestVersion" :status="status"
            :upgrade-available="upgradeAvailable" :disabled="upgradeDisabled" @upgrade="openUpgradeDialog()" />
          <div class="ml-auto flex shrink-0 items-center gap-1" data-testid="sidebar-footer-actions">
            <ConnectionIndicator :status="status" @retry="reconnect()" />
            <SidebarGearMenu v-if="!minimal" :restart-disabled="upgrading || restartPending" :restart-pending="restartPending"
              @restart="triggerRestart()" />
          </div>
        </div>
      </div>
      <div v-if="!sidebarPeek" role="separator" tabindex="0" aria-orientation="vertical" aria-label="Resize sidebar"
        data-testid="sidebar-resize" :aria-valuenow="sidebarWidth" :aria-valuemin="SIDEBAR_MIN_WIDTH"
        :aria-valuemax="SIDEBAR_MAX_WIDTH" title="Drag to resize, double-click to reset"
        class="absolute inset-y-0 -right-1 z-20 w-2 cursor-col-resize transition-colors hover:bg-blue-500/30 focus-visible:bg-blue-500/40 focus-visible:outline-none"
        :class="sidebarResizing ? 'bg-blue-500/40' : ''"
        @pointerdown="startSidebarResize" @dblclick="sidebar.resetWidth()" @keydown="resizeSidebarByKey" />
    </nav>

    <nav v-if="sidebarCollapsed" data-testid="sidebar-rail" aria-label="Main"
      class="hidden w-14 shrink-0 flex-col items-center gap-1 border-r border-gray-200 bg-gray-50 py-3 dark:border-gray-800 dark:bg-gray-900 lg:flex"
      @mouseenter="openSidebarPeekSoon" @mousemove="openSidebarPeekSoon" @mouseleave="cancelSidebarPeekOpen">
      <img :src="logoUrl" alt="Aviary logo" class="mb-1 h-8 w-8 object-contain" />
      <SidebarRailItem :label="`Expand sidebar (${shortcutLabel})`" data-testid="sidebar-expand" @click="sidebar.toggle()">
        <ChevronDoubleRightIcon aria-hidden="true" class="h-5 w-5" />
      </SidebarRailItem>
      <div class="my-1 h-px w-8 shrink-0 bg-gray-200 dark:bg-gray-800" />
      <div class="flex min-h-0 w-full flex-1 flex-col items-center gap-1 overflow-y-auto" data-testid="sidebar-rail-nav">
        <template v-if="soleAgent">
          <SidebarRailItem v-for="child in soleAgent.children" :key="child.key" :label="child.label" :to="child.to"
            :active="isChildActive(child)" :badge="child.running" :data-testid="`sidebar-rail-${child.key}`">
            <component :is="pageIcons[child.key]" aria-hidden="true" class="h-5 w-5" />
          </SidebarRailItem>
        </template>
        <template v-else>
          <SidebarRailItem label="Agents" to="/agents" :active="route.path === '/agents'" data-testid="sidebar-rail-agents">
            <UsersIcon aria-hidden="true" class="h-5 w-5" />
          </SidebarRailItem>
          <template v-for="a in agentNav" :key="a.name">
            <SidebarRailItem :label="a.label" :to="a.to" :active="a.active && !a.children.length" :badge="a.running"
              :data-agent="a.name" data-testid="sidebar-rail-agent">
              <span aria-hidden="true" class="text-sm font-semibold uppercase"
                :class="a.active ? 'text-gray-950 dark:text-white' : ''">{{ a.label.slice(0, 1) }}</span>
            </SidebarRailItem>
            <template v-if="a.active">
              <SidebarRailItem v-for="child in a.children" :key="child.key" :label="child.label" :to="child.to"
                :active="isChildActive(child)" :badge="child.running" :data-testid="`sidebar-rail-${child.key}`">
                <component :is="pageIcons[child.key]" aria-hidden="true" class="h-5 w-5" />
              </SidebarRailItem>
            </template>
          </template>
        </template>
        <SidebarRailItem v-if="!minimal" label="System" to="/system/overview" :active="route.path === '/system/overview'"
          data-testid="sidebar-rail-system">
          <ServerStackIcon aria-hidden="true" class="h-5 w-5" />
        </SidebarRailItem>
        <SidebarRailItem v-if="currentSystemChild && !minimal" :label="currentSystemChild.label" :to="currentSystemChild.to"
          active :badge="currentSystemChild.running" :data-testid="`sidebar-rail-system-${currentSystemChild.key}`">
          <SystemPageIcon :page-key="currentSystemChild.key" class="h-5 w-5" />
        </SidebarRailItem>
      </div>
      <div class="mt-auto flex flex-col items-center gap-1 pt-2">
        <SidebarRailItem v-if="upgradeAvailable && !minimal" :label="railUpdateLabel" dot="bg-yellow-400"
          data-testid="sidebar-rail-update" :disabled="upgradeDisabled" @click="openUpgradeDialog()">
          <ArrowUpCircleIcon aria-hidden="true" class="h-5 w-5" />
        </SidebarRailItem>
        <SidebarRailItem v-else-if="!minimal" :label="railVersionLabel" data-testid="sidebar-rail-version">
          <TagIcon aria-hidden="true" class="h-5 w-5" />
        </SidebarRailItem>
        <ConnectionIndicator :status="status" @retry="reconnect()" />
        <SidebarGearMenu v-if="!minimal" rail :restart-disabled="upgrading || restartPending" :restart-pending="restartPending"
          @restart="triggerRestart()" />
      </div>
    </nav>

    <main class="flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden">
      <slot />
    </main>

    <NavEditDialog :groups="navEditGroups" />
    <AgentItemDialogs />
    <ChannelDialogs />
    <ToastStack />

    <UpgradeDialog :open="upgradeDialogOpen" :latest-version="latestVersion" :release-url="releaseUrl"
      :jobs="restart?.jobs ?? []" :busy="upgrading" :git="git" @close="upgradeDialogOpen = false"
      @confirm="triggerUpgrade($event)" />
  </div>
</template>

<script lang="ts">
import { createRouteExpandedGroupState } from "../composables/useRouteExpandedGroup";

// Shared by every page's layout, so the System section's collapse stays as
// toggled. It starts collapsed and opens on reaching a System page.
const systemGroupState = createRouteExpandedGroupState();
</script>

<script setup lang="ts">
import NoSymbolIcon from "@heroicons/vue/20/solid/NoSymbolIcon";
import PauseIcon from "@heroicons/vue/20/solid/PauseIcon";
import ArrowUpCircleIcon from "@heroicons/vue/24/outline/ArrowUpCircleIcon";
import ChatBubbleLeftRightIcon from "@heroicons/vue/24/outline/ChatBubbleLeftRightIcon";
import ChevronDoubleLeftIcon from "@heroicons/vue/24/outline/ChevronDoubleLeftIcon";
import ChevronDoubleRightIcon from "@heroicons/vue/24/outline/ChevronDoubleRightIcon";
import ClockIcon from "@heroicons/vue/24/outline/ClockIcon";
import Cog6ToothIcon from "@heroicons/vue/24/outline/Cog6ToothIcon";
import DocumentTextIcon from "@heroicons/vue/24/outline/DocumentTextIcon";
import GlobeAltIcon from "@heroicons/vue/24/outline/GlobeAltIcon";
import ServerStackIcon from "@heroicons/vue/24/outline/ServerStackIcon";
import HashtagIcon from "@heroicons/vue/24/outline/HashtagIcon";
import TagIcon from "@heroicons/vue/24/outline/TagIcon";
import UsersIcon from "@heroicons/vue/24/outline/UsersIcon";
import {
	type Component,
	computed,
	markRaw,
	onBeforeUnmount,
	onMounted,
	ref,
	watch,
} from "vue";
import { useRoute, useRouter } from "vue-router";
import { useProjectList } from "../composables/useProjects";
import { useAgentItemDialogs } from "../composables/useAgentItemDialogs";
import { useChannelDialogs } from "../composables/useChannelDialogs";
import { channelName, channelPath } from "../lib/channels";
import { useServerStatus } from "../composables/useServerStatus";
import {
	SIDEBAR_KEY_STEP,
	SIDEBAR_MAX_WIDTH,
	SIDEBAR_MIN_WIDTH,
	SIDEBAR_SNAP_WIDTH,
	useSidebarLayout,
} from "../composables/useSidebarLayout";
import { agentFilePath, useAgentFilesStore } from "../stores/agentFiles";
import { projectOwner } from "../lib/projectSettings";
import { useRouteExpandedGroup } from "../composables/useRouteExpandedGroup";
import { useNavPrefs } from "../composables/useNavPrefs";
import { arrangeNav, type NavList } from "../lib/navPrefs";
import { MINIMAL_AGENT_PAGES } from "../lib/minimalMode";
import { isSettingsPath, SYSTEM_PAGES } from "../lib/systemNav";
import { shortVersion } from "../lib/version";
import { type ProjectSummary, projectPath } from "../lib/projects";
import { useAuthStore } from "../stores/auth";
import { useChatSessionsStore } from "../stores/chatSessions";
import {
	activeTaskName,
	isWorkflow,
	taskSessionPath,
	useSettingsStore,
} from "../stores/settings";
import CodeLoopIcon from "./CodeLoopIcon.vue";
import SidebarRowAction from "./SidebarRowAction.vue";
import AutoUpdateError from "./AutoUpdateError.vue";
import UpdateStatusBanner from "./UpdateStatusBanner.vue";
import MobileMainNav, { type MobileNavItem } from "./MobileMainNav.vue";
import ProjectRunningBadge from "./projects/ProjectRunningBadge.vue";
import ChatSessionList from "./chat/ChatSessionList.vue";
import AgentFileList from "./sidebar/AgentFileList.vue";
import AgentItemDialogs from "./sidebar/AgentItemDialogs.vue";
import AgentChannelList from "./sidebar/AgentChannelList.vue";
import ChannelDialogs from "./sidebar/ChannelDialogs.vue";
import ConnectionIndicator from "./ConnectionIndicator.vue";
import AgentTaskList from "./sidebar/AgentTaskList.vue";
import SidebarGearMenu from "./sidebar/SidebarGearMenu.vue";
import SidebarRailItem from "./sidebar/SidebarRailItem.vue";
import SystemPageIcon from "./SystemPageIcon.vue";
import ToastStack from "./ui/ToastStack.vue";
import NavEditDialog, { type NavEditGroup } from "./nav/NavEditDialog.vue";
import UpgradeDialog from "./UpgradeDialog.vue";
import VersionIndicator from "./VersionIndicator.vue";

interface NavChild {
	key: string;
	label: string;
	to: string;
	/** Running jobs (a project's, or all of Code Loop'), shown as a
	 * gray badge when any. */
	running?: number;
	/** Overrides the path match, for items picked by a query. */
	active?: boolean;
	/** A paused scheduled task, shown dimmed with a pause icon. */
	paused?: boolean;
	/** A disabled channel, shown dimmed with a no-entry icon. */
	disabled?: boolean;
}

interface AgentNav {
	/** `agent:<name>`, the agent's place in the top-row order. */
	key: string;
	name: string;
	label: string;
	to: string;
	active: boolean;
	running: number;
	/** The active agent's pages; empty for the others. */
	children: NavChild[];
	projects: NavChild[];
}

interface BrandLink {
	label: string;
	href: string;
	iconComponent?: Component;
	iconPath?: string;
}

const auth = useAuthStore();
// An `aviary watch` session shows only Code Loop and Scheduled Tasks.
const minimal = computed(() => auth.minimal);
const route = useRoute();
const router = useRouter();
const {
	status,
	version,
	latestVersion,
	upgradeAvailable,
	upgradeSupported,
	git,
	noCheckout,
	updateNotice,
	versionMessage,
	upgrading,
	releaseUrl,
	restart,
	upgradeDialogOpen,
	reconnect,
	openUpgradeDialog,
	triggerUpgrade,
	triggerRestart,
	cancelRestart,
} = useServerStatus();
const restartPending = computed(
	() => !!restart.value && restart.value.phase !== "idle",
);
const upgradeDisabled = computed(
	() => upgrading.value || restartPending.value || !upgradeSupported.value,
);
// Why an automatic update has not applied yet, and how its last attempt
// failed, shown at the update notice.
const autoUpdateWaiting = computed(() => {
	const auto = restart.value?.autoUpdate;
	switch (auto?.state) {
		case "waiting-restart":
			return "Auto update is waiting for the restart in progress.";
		case "applying":
			return git.value ? "Auto update is pulling the checkout…" : "Auto update is applying…";
		default:
			return "";
	}
});
const logoUrl = "/logo.png";
const brandLinks: BrandLink[] = [
	{
		label: "Open Aviary website",
		href: "https://aviary.bot",
		iconComponent: markRaw(GlobeAltIcon),
	},
	{
		label: "Open Aviary GitHub repository",
		href: "https://github.com/lsegal/aviary",
		iconPath:
			"M12 .297c-6.63 0-12 5.373-12 12 0 5.303 3.438 9.8 8.205 11.385.6.113.82-.258.82-.577 0-.285-.01-1.04-.015-2.04-3.338.724-4.042-1.61-4.042-1.61C4.422 17.97 3.633 17.7 3.633 17.7c-1.087-.744.084-.729.084-.729 1.205.084 1.84 1.236 1.84 1.236 1.07 1.834 2.807 1.304 3.492.997.108-.776.417-1.305.76-1.605-2.665-.3-5.466-1.332-5.466-5.93 0-1.31.465-2.38 1.235-3.22-.135-.303-.54-1.523.105-3.176 0 0 1.005-.322 3.3 1.23a11.49 11.49 0 0 1 3.003-.404c1.02.005 2.045.138 3.003.404 2.295-1.552 3.3-1.23 3.3-1.23.645 1.653.24 2.873.12 3.176.765.84 1.23 1.91 1.23 3.22 0 4.61-2.805 5.625-5.475 5.92.42.36.81 1.096.81 2.22 0 1.606-.015 2.896-.015 3.286 0 .315.21.69.825.57C20.565 22.092 24 17.592 24 12.297c0-6.627-5.373-12-12-12",
	},
];

// An agent's pages, in sidebar order. Settings holds its General and
// Permissions sections; each channel's settings open from its own entry.
const AGENT_PAGES = [
	{ key: "chat", label: "Chats" },
	{ key: "projects", label: "Code Loop" },
	{ key: "channels", label: "Channels" },
	{ key: "tasks", label: "Scheduled Tasks" },
	{ key: "files", label: "Files" },
	{ key: "settings", label: "Agent Settings" },
] as const;

// An agent page's icon on the collapsed sidebar rail.
const pageIcons: Record<string, Component> = {
	chat: markRaw(ChatBubbleLeftRightIcon),
	projects: markRaw(CodeLoopIcon),
	channels: markRaw(HashtagIcon),
	tasks: markRaw(ClockIcon),
	files: markRaw(DocumentTextIcon),
	settings: markRaw(Cog6ToothIcon),
};

// The desktop sidebar collapses to an icon rail with Cmd/Ctrl+B or its
// header toggle, and its right edge drags to resize it (#724).
const sidebar = useSidebarLayout();
const sidebarWidth = sidebar.width;
const sidebarCollapsed = sidebar.collapsed;
const sidebarResizing = ref(false);
const railVersionLabel = computed(() => {
	if (version.value) return `Version ${shortVersion(version.value)}`;
	return status.value === "disconnected" ? "Version unknown" : "Version …";
});
const railUpdateLabel = computed(() =>
	latestVersion.value
		? `Update available: ${shortVersion(latestVersion.value)}`
		: "Update available",
);
const isMac = /Mac|iPhone|iPad/.test(navigator.platform);
const shortcutLabel = isMac ? "⌘B" : "Ctrl+B";

function startSidebarResize(e: PointerEvent) {
	if (e.button !== 0) return;
	e.preventDefault();
	const startX = e.clientX;
	const startWidth = sidebarWidth.value;
	sidebarResizing.value = true;
	document.body.style.cursor = "col-resize";
	document.body.style.userSelect = "none";
	const onMove = (move: PointerEvent) => {
		const next = startWidth + move.clientX - startX;
		// Dragging well below the minimum snaps to the rail; expanding again
		// restores the width the drag started from.
		if (next < SIDEBAR_SNAP_WIDTH) {
			sidebar.setWidth(startWidth);
			sidebarCollapsed.value = true;
			stop();
			return;
		}
		sidebar.setWidth(next);
	};
	const stop = () => {
		window.removeEventListener("pointermove", onMove);
		window.removeEventListener("pointerup", stop);
		sidebarResizing.value = false;
		document.body.style.cursor = "";
		document.body.style.userSelect = "";
	};
	window.addEventListener("pointermove", onMove);
	window.addEventListener("pointerup", stop);
}

function resizeSidebarByKey(e: KeyboardEvent) {
	const steps: Record<string, number> = {
		ArrowLeft: sidebarWidth.value - SIDEBAR_KEY_STEP,
		ArrowRight: sidebarWidth.value + SIDEBAR_KEY_STEP,
		Home: SIDEBAR_MIN_WIDTH,
		End: SIDEBAR_MAX_WIDTH,
	};
	if (!(e.key in steps)) return;
	e.preventDefault();
	sidebar.setWidth(steps[e.key]);
}

// Hover peek: resting the pointer on the collapsed rail's background opens
// the full sidebar as an overlay without changing the saved collapsed state
// (#737).
const SIDEBAR_PEEK_DELAY = 300;
const sidebarPeek = ref(false);
const sidebarEl = ref<HTMLElement | null>(null);
let sidebarPeekOpenTimer: ReturnType<typeof setTimeout> | undefined;
let sidebarPeekCloseTimer: ReturnType<typeof setTimeout> | undefined;

function cancelSidebarPeekOpen() {
	clearTimeout(sidebarPeekOpenTimer);
}

function cancelSidebarPeekClose() {
	clearTimeout(sidebarPeekCloseTimer);
}

// Restarts on every pointer move, so the peek waits for the pointer to rest.
// Resting on an icon shows its tooltip instead, so the overlay never covers
// the icon about to be clicked.
function openSidebarPeekSoon(e: MouseEvent) {
	if (sidebarPeek.value) return;
	cancelSidebarPeekOpen();
	// mouseenter targets the rail itself, also when a page change remounts it
	// under a pointer resting on one of its icons; ask what is under the
	// pointer instead.
	const under = document.elementFromPoint(e.clientX, e.clientY) ?? e.target;
	if ((under as Element | null)?.closest?.("a, button")) return;
	sidebarPeekOpenTimer = setTimeout(() => {
		sidebarPeek.value = true;
	}, SIDEBAR_PEEK_DELAY);
}

function closeSidebarPeek() {
	cancelSidebarPeekOpen();
	cancelSidebarPeekClose();
	sidebarPeek.value = false;
}

function closeSidebarPeekSoon() {
	if (!sidebarPeek.value) return;
	cancelSidebarPeekClose();
	sidebarPeekCloseTimer = setTimeout(closeSidebarPeek, SIDEBAR_PEEK_DELAY);
}

function closeSidebarPeekOnLink(e: MouseEvent) {
	if (sidebarPeek.value && (e.target as Element).closest("a")) {
		closeSidebarPeek();
	}
}

// Tabbing or clicking out of the overlay closes it, so focus is never held.
function closeSidebarPeekOnFocusOut(e: FocusEvent) {
	if (!sidebarPeek.value) return;
	const next = e.relatedTarget as Node | null;
	if (next && sidebarEl.value?.contains(next)) return;
	closeSidebarPeek();
}

// Expanding for real, or collapsing again by dragging, ends the peek.
watch(sidebarCollapsed, closeSidebarPeek);

function onSidebarShortcut(e: KeyboardEvent) {
	if (e.key === "Escape" && sidebarPeek.value) {
		closeSidebarPeek();
		return;
	}
	if (e.defaultPrevented || e.altKey || e.shiftKey) return;
	if (!(isMac ? e.metaKey : e.ctrlKey) || e.key.toLowerCase() !== "b") return;
	// Below lg the mobile header replaces the sidebar.
	if (!window.matchMedia("(min-width: 1024px)").matches) return;
	e.preventDefault();
	sidebar.toggle();
}

onMounted(() => window.addEventListener("keydown", onSidebarShortcut));
onBeforeUnmount(() => {
	window.removeEventListener("keydown", onSidebarShortcut);
	closeSidebarPeek();
});

// The user's navigation order and visibility (#1003), shared by the
// sidebar, the collapsed rail and the narrow-screen rows.
const nav = useNavPrefs();
// Pages that never show whatever the preferences say: those a minimal
// `aviary watch` session leaves out (#991).
const navForcedHidden = computed<ReadonlySet<string>>(() =>
	minimal.value
		? new Set([
				...AGENT_PAGES.map((p) => p.key).filter(
					(k) => !MINIMAL_AGENT_PAGES.has(k),
				),
				...SYSTEM_PAGES.map((p) => p.key),
			])
		: new Set(),
);
function arranged<T extends { key: string }>(items: T[], list: NavList): T[] {
	return arrangeNav(items, list, nav.prefs.value, {
		forcedHidden: navForcedHidden.value,
		showHidden: nav.showHidden.value,
	});
}

const settingsStore = useSettingsStore();
if (!settingsStore.config && auth.isLoggedIn) void settingsStore.fetchConfig();
const { projects } = useProjectList();

function projectChild(owner: string, p: ProjectSummary): NavChild {
	return {
		key: p.name,
		label: p.name,
		to: projectPath(owner, p.name),
		running: p.running,
	};
}

const routeAgent = computed(() =>
	typeof route.params.agent === "string" ? route.params.agent : "",
);

const agentNames = computed(() =>
	(settingsStore.config?.agents ?? []).map((a) => a.name).filter(Boolean),
);

function ownedProjects(agent: string): ProjectSummary[] {
	return projects.value.filter(
		(p) => projectOwner(settingsStore.config, p.name) === agent,
	);
}

function agentPagePath(agent: string, page: string): string {
	return `/agents/${encodeURIComponent(agent)}/${page}`;
}

// On one agent's page, another agent's link opens the same page for it;
// elsewhere it opens the agent's chat.
const agentPageTail = computed(() => {
	const match = /^\/agents\/[^/]+\/(.+)$/.exec(route.path);
	const tail = match?.[1] ?? "chat";
	if (tail.startsWith("chat")) return "chat";
	if (tail.startsWith("projects")) return "projects";
	if (tail.startsWith("channels")) return "channels";
	return tail;
});

const agentNav = computed<AgentNav[]>(() => {
	const names = [...agentNames.value];
	// An agent added but not saved yet routes as _N until it has a name.
	const pending = routeAgent.value.startsWith("_") ? routeAgent.value : "";
	if (pending) names.push(pending);
	const agents = names.map((name) => {
		const active = routeAgent.value === name;
		const owned = pending === name ? [] : ownedProjects(name);
		const running = owned.reduce((n, p) => n + p.running, 0);
		return {
			name,
			label: pending === name ? "New agent" : name,
			to: agentPagePath(
				name,
				pending === name ? "settings" : agentPageTail.value,
			),
			active,
			running,
			children:
				active || names.length === 1
				? arranged(
						AGENT_PAGES.map((page) => ({
							key: page.key,
							label: page.label,
							to: agentPagePath(name, page.key),
							running: page.key === "projects" ? running : undefined,
						})),
						"agentPages",
					)
				: [],
			projects: owned.map((p) => projectChild(name, p)),
		};
	});
	return agents.map((a) => ({ ...a, key: `agent:${a.name}` }));
});

// With exactly one agent its pages are the top level, with no Agents → name
// level. A second agent, including one being added, brings the full tree back.
const soleAgent = computed<AgentNav | undefined>(() =>
	agentNav.value.length === 1 && !agentNav.value[0].name.startsWith("_")
		? agentNav.value[0]
		: undefined,
);

// The sidebar always lists an agent's projects under Code Loop (#867); the
// narrow-screen second row lists them only on its Code Loop pages and boards.
const projectsOpen = computed(() =>
	/^\/agents\/[^/]+\/projects(\/|$)/.test(route.path),
);

// The sidebar always lists an agent's Scheduled Tasks, like its Channels;
// the narrow-screen second row lists them on the Tasks page and task
// sessions. Files list their items only on their own page.
const tasksOpen = computed(() =>
	/^\/agents\/[^/]+\/tasks(\/|$)/.test(route.path),
);
const filesOpen = computed(() => /^\/agents\/[^/]+\/files\/?$/.test(route.path));
// An agent's Channels pages: its channels' views and settings.
const channelsOpen = computed(() =>
	/^\/agents\/[^/]+\/channels(\/|$)/.test(route.path),
);

const { open: openItemDialog } = useAgentItemDialogs();
const agentFiles = useAgentFilesStore();

/** The "+" of an agent's page row, or "" when the page has none. */
function rowAddLabel(page: string): string {
	if (page === "channels") return "Add channel";
	if (page === "tasks") return "Add task";
	if (page === "files") return "Add file";
	return "";
}

const { open: openChannelDialog } = useChannelDialogs();

function openAdd(page: string, agent: string) {
	if (page === "channels") openChannelDialog({ mode: "create", agent });
	else if (page === "tasks" || page === "files")
		openItemDialog({ kind: page === "tasks" ? "task" : "file", agent, mode: "create" });
}

/** The active agent's channels, tasks or files, for the narrow-screen second row. */
function agentItems(agent: string): NavChild[] {
	if (channelsOpen.value) {
		const channels =
			settingsStore.config?.agents.find((a) => a.name === agent)?.channels ??
			[];
		// Disabled channels follow the enabled ones, as in the sidebar.
		return channels
			.map((c) => ({
				key: `channel:${channelName(c)}`,
				label: channelName(c),
				to: channelPath(agent, channelName(c)),
				disabled: c.enabled === false,
			}))
			.sort((a, b) => Number(a.disabled) - Number(b.disabled));
	}
	if (tasksOpen.value) {
		const tasks =
			settingsStore.config?.agents.find((a) => a.name === agent)?.tasks ?? [];
		const active = activeTaskName(route, agent);
		// Paused tasks follow the active ones, as in the sidebar.
		return tasks
			.filter((t) => t.name && !isWorkflow(t))
			.map((t) => ({
				key: `task:${t.name}`,
				label: t.name,
				to: router.resolve(taskSessionPath(agent, t.name)).fullPath,
				active: active === t.name,
				paused: t.enabled === false,
			}))
			.sort((a, b) => Number(a.paused) - Number(b.paused));
	}
	if (filesOpen.value) {
		const q = route.query.file;
		return agentFiles.files(agent).map((f, i) => ({
			key: `file:${f}`,
			label: f,
			to: router.resolve(agentFilePath(agent, f)).fullPath,
			active: typeof q === "string" && q ? q === f : i === 0,
		}));
	}
	return [];
}

const mobileAdd = computed(() => {
	const agent = agentNav.value.find((a) => a.active);
	if (!agent) return null;
	if (channelsOpen.value) return { page: "channels", agent: agent.name };
	if (tasksOpen.value) return { page: "tasks", agent: agent.name };
	if (filesOpen.value) return { page: "files", agent: agent.name };
	return null;
});

// Add project opens the add dialog on the agent's Code Loop board.
function addProjectLink(agent: string) {
	return { path: agentPagePath(agent, "projects"), query: { add: "1" } };
}

// Rows whose "+" sits over their right edge make room for it only while it
// shows, so a count badge stays at the right edge until then. The "+" shows
// on row hover or focus, and always on touch (see SidebarRowAction).
const rowActionPad =
	"pr-9 [@media(hover:hover)]:pr-3 group-hover/row:pr-9 group-focus-within/row:pr-9";

// Rows whose "+" sits over their right edge keep room for it.
function hasRowAction(child: NavChild): boolean {
	return (
		child.key === "projects" ||
		child.key === "chat" ||
		rowAddLabel(child.key) !== ""
	);
}

const chatSessions = useChatSessionsStore();
const creatingChat = ref(false);
async function newChat(agent: string) {
	creatingChat.value = true;
	try {
		const sess = await chatSessions.create(agent);
		await router.push(`${agentPagePath(agent, "chat")}/${sess.id}`);
	} catch (e) {
		console.error("Failed to create chat", e);
	} finally {
		creatingChat.value = false;
	}
}

// Runtime watches no agent owns sit with System's other pages.
const watchChildren = computed<NavChild[]>(() =>
	projects.value
		.filter((p) => !projectOwner(settingsStore.config, p.name))
		.map((p) => ({
			...projectChild("", p),
			key: `watch:${p.name}`,
			label: `Watch: ${p.name}`,
		})),
);

// The System item is the overview, so its pages list without one. Pages
// the user hid stay reachable from the overview's cards.
const systemChildren = computed<NavChild[]>(() => [
	...arranged(
		SYSTEM_PAGES.map(({ key, label, to }) => ({ key, label, to })),
		"systemPages",
	),
	...watchChildren.value,
]);

// The rail shows only the current System page under System, in its own
// icon, so the rail keeps room to rest the pointer and peek the sidebar.
// A hidden page still shows there while it is open.
const currentSystemChild = computed<NavChild | undefined>(() =>
	[
		...SYSTEM_PAGES.map(({ key, label, to }) => ({ key, label, to })),
		...watchChildren.value,
	].find((child) => isChildActive(child)),
);

// Settings' tabs live under /system but open from the gear menu instead.
const systemActive = computed(
	() => route.path.startsWith("/system/") && !isSettingsPath(route.path),
);

// System opens on each System page it lists, so the active page shows.
// Collapsed, only the System header shows; it then stands in for whichever
// System page is current.
const systemCollapsed = useRouteExpandedGroup(systemGroupState, () =>
	systemActive.value ? currentSystemChild.value?.key : undefined,
);
const systemHeaderActive = computed(
	() =>
		route.path === "/system/overview" ||
		(systemActive.value && systemCollapsed.value),
);

function isChildActive(child: NavChild): boolean {
	// Project names contain an encoded "/", so compare decoded paths.
	const path = decodeURIComponent(child.to);
	const current = decodeURIComponent(route.path);
	return current === path || current.startsWith(`${path}/`);
}

function topLevelLinkClass(active: boolean): string {
	const base = "block rounded-xl px-3 py-2 text-sm font-medium transition-colors";
	return active
		? `${base} bg-white text-gray-950 shadow-sm dark:bg-gray-800 dark:text-white`
		: `${base} text-gray-600 hover:bg-gray-200 hover:text-gray-900 dark:text-gray-400 dark:hover:bg-gray-800 dark:hover:text-white`;
}

function childLinkClass(active: boolean, mobile: boolean): string {
	const base = "mt-1 block rounded-lg px-3 py-2 text-sm transition-colors";
	if (active) {
		return `${base} bg-gray-100 font-semibold text-gray-950 dark:bg-gray-800 dark:text-white`;
	}
	return mobile
		? `${base} text-gray-500 hover:bg-gray-50 hover:text-gray-900 dark:text-gray-400 dark:hover:bg-gray-800 dark:hover:text-white`
		: `${base} text-gray-500 hover:bg-gray-100 hover:text-gray-900 dark:text-gray-400 dark:hover:bg-gray-800 dark:hover:text-white`;
}

// On narrow screens the active agent's pages, or all of System's, form a
// second row under the main one.
const mobileChildren = computed<NavChild[]>(() => {
	const agent = agentNav.value.find((a) => a.active);
	// A sole agent's pages are already the main row.
	const items = agent
		? [...(projectsOpen.value ? agent.projects : []), ...agentItems(agent.name)]
		: [];
	if (agent && soleAgent.value) return items;
	if (agent) return [...agent.children, ...items];
	if (systemActive.value) return systemChildren.value;
	return [];
});

// The narrow-screen main row: a sole agent's pages, in the user's Pages
// order and without the pages they hid (#1015), or one pill per agent,
// plus System. Agents are added from Settings. The row scrolls sideways
// when the items do not fit.
const mobileMainItems = computed<MobileNavItem[]>(() => {
	const system: MobileNavItem = {
		key: "system",
		label: "System",
		to: "/system/overview",
		active: systemActive.value,
	};
	const systemItems = minimal.value ? [] : [system];
	const sole = soleAgent.value;
	if (sole) {
		return [
			...sole.children.map((child) => ({
				key: child.key,
				label: child.label,
				to: child.to,
				active: isChildActive(child),
				running: child.running,
			})),
			...systemItems,
		];
	}
	return [
		...agentNav.value.map((a) => ({
			key: a.key,
			label: a.label,
			to: a.to,
			active: a.active,
		})),
		...systemItems,
	];
});

// Hidden pages the sidebar leaves out, for its Show hidden toggle.
const hiddenNavCount = computed(() => {
	const count = (list: NavList, keys: string[]) =>
		keys.filter((k) => nav.isHidden(list, k) && !navForcedHidden.value.has(k))
			.length;
	return (
		count(
			"agentPages",
			AGENT_PAGES.map((p) => p.key),
		) +
		count(
			"systemPages",
			SYSTEM_PAGES.map((p) => p.key),
		)
	);
});

// The Edit sidebar dialog's lists: the agent pages, whose order the top
// row follows too, and System pages.
const navEditGroups = computed<NavEditGroup[]>(() => {
	const shown = (items: { key: string; label: string }[]) =>
		items
			.filter((i) => !navForcedHidden.value.has(i.key))
			.map(({ key, label }) => ({ key, label }));
	const groups: NavEditGroup[] = [
		{
			list: "agentPages",
			title: soleAgent.value ? "Pages" : "Agent pages",
			items: shown([...AGENT_PAGES]),
		},
		{
			list: "systemPages",
			title: "System pages",
			items: shown(SYSTEM_PAGES),
		},
	];
	return groups.filter((g) => g.items.length);
});

function mobileSubnavClass(child: NavChild): string {
	return (child.active ?? isChildActive(child))
		? "shrink-0 rounded-lg bg-gray-100 px-3 py-2 text-sm font-semibold text-gray-950 dark:bg-gray-800 dark:text-white"
		: "shrink-0 rounded-lg px-3 py-2 text-sm text-gray-500 hover:bg-gray-50 hover:text-gray-900 dark:text-gray-400 dark:hover:bg-gray-800 dark:hover:text-white";
}
</script>
