---
layout: home

hero:
  name: Aviary
  text: The AI Agent Nest
  tagline: Give your AI a home. Connect it to Slack, Signal, or Discord, chat with it, put it to work on a schedule, and manage everything from a simple web dashboard or the terminal.
  image:
    src: /logo.png
    alt: Aviary logo

features:
  - title: Connected Assistants
    details: Hook your AI into Slack, Signal, Discord, and more so it shows up where your team already talks — no separate app required.
  - title: Run Tasks Without Burning Tokens
    details: Repeat the same task every hour? Aviary can compile it into a lightweight script that runs directly — no AI call, no API cost, every time.
  - title: One Small Binary
    details: Written in Go. As little as 6 MB memory at runtime. No Node, no Python, no Docker. Just start it and... Go.
---

Aviary is a full AI assistant platform. It goes beyond a chat window — it connects your AI models to the apps you already use, keeps conversations going over time, runs tasks on a schedule, and gives you a web dashboard plus a CLI to keep it all under control.

<section class="content-section content-section-stats">
<p class="section-eyebrow">Built To Run Lean</p>

<h2 class="section-heading">Low Overhead By Design</h2>

<p class="section-subheading">Aviary is a single tiny binary. It shares one browser session across all your agents so nothing piles up in the background. Repeat tasks that don't need AI reasoning get compiled into fast scripts that run directly — no API call, no cost, no slowdown no matter how often they fire.</p>

<div class="comparison-grid">
  <div class="comparison-card">
    <h4 class="comparison-title">Memory Footprint</h4>
    <div class="comparison-rows">
      <div class="comparison-row">
        <span class="comparison-label">Aviary *</span>
        <div class="comparison-bar-wrap">
          <div class="comparison-bar comparison-bar-aviary" style="width: 12.5%"></div>
        </div>
        <span class="comparison-bar-value">128 MB</span>
      </div>
      <div class="comparison-row">
        <span class="comparison-label">OpenClaw</span>
        <div class="comparison-bar-wrap">
          <div class="comparison-bar comparison-bar-other" style="width: 100%"></div>
        </div>
        <span class="comparison-bar-value">1 GB</span>
      </div>
    </div>
    <p class="comparison-footnote">* Recommended footprint including Slack, Signal, and Discord channel daemons. Lower when no channels are configured.</p>
  </div>
  <div class="comparison-card">
    <h4 class="comparison-title">Token Cost Per Scheduled Run</h4>
    <div class="comparison-rows">
      <div class="comparison-row">
        <span class="comparison-label">Prompt task</span>
        <div class="comparison-bar-wrap">
          <div class="comparison-bar comparison-bar-other" style="width: 100%"></div>
        </div>
        <span class="comparison-bar-value">~5,000 tokens</span>
      </div>
      <div class="comparison-row">
        <span class="comparison-label">Compiled script</span>
        <div class="comparison-bar-wrap">
          <div class="comparison-bar comparison-bar-aviary" style="width: 2%"></div>
        </div>
        <span class="comparison-zero-label">~100 tokens</span>
      </div>
    </div>
    <p class="comparison-footnote">Measured from real Aviary usage data. Simple tasks (URL checks, API polls) average ~5,000 tokens/run. Research tasks run higher. Average of 100 is based on minimal overhead from non-deterministic operations in compiled scripts.</p>
  </div>
</div>
</section>

<section class="content-section content-section-showcase">
<p class="section-eyebrow">Control Panel</p>

<h2 class="section-heading">See Aviary In Action</h2>

<p class="section-subheading">The same control panel gives you live status, long-running conversations, and token-level usage data without leaving the browser.</p>

<ScreenshotGallery
  :items="[
    {
      src: '/screenshots/chat-workspace.png',
      alt: 'Aviary chat workspace focused on web search, calendar checks, and email triage',
      title: 'Plan The Morning',
      description: 'Work through search results, today’s calendar, and unread email in a single agent thread.',
      featured: true,
    },
    {
      src: '/screenshots/configure-everything.png',
      alt: 'Aviary agent channel configuration screen showing Slack channel settings, allowed senders, and per-channel model overrides',
      title: 'Configure Everything',
      description: 'Set up channels, sender rules, model overrides, and day-to-day routing from one agent config screen.',
    },
    {
      src: '/screenshots/usage-analytics.png',
      alt: 'Aviary usage page with token charts, provider breakdowns, and session analytics',
      title: 'Usage Analytics',
      description: 'Track token spend, provider mix, and session activity from one live reporting view.',
    },
    {
      src: '/screenshots/security-minded.png',
      alt: 'Aviary agent permissions page showing a low-privilege preset, tool restrictions, filesystem rules, and exec command allowlists',
      title: 'Security Minded',
      description: 'Keep agents on a low-privilege setup with tight tool access, path rules, and explicit command allowlists.',
    },
  ]"
/>
</section>

<section class="content-section content-section-alt">
<p class="section-eyebrow">System Views</p>

<h2 class="section-heading">Inspect The Whole Runtime</h2>

<p class="section-subheading">The system surfaces give you a live catalog of what Aviary can do, what is installed, and what your scheduled automations are doing right now.</p>

<ScreenshotGallery
  compact
  :items="[
    {
      src: '/screenshots/system-jobs.png',
      alt: 'Aviary jobs page showing queue health, scheduled tasks, compile attempts, and a detail panel',
      title: 'Jobs',
      description: 'Review scheduled runs, compile attempts, and live job state with detail panes for follow-up.',
    },
    {
      src: '/screenshots/system-skills.png',
      alt: 'Aviary skill marketplace showing installed and enabled skills',
      title: 'Skills',
      description: 'Browse installed skills, filter by source, and toggle what is available to your agents.',
    },
    {
      src: '/screenshots/system-tools.png',
      alt: 'Aviary system tools page with the run tool modal open and a completed tool output',
      title: 'Tools',
      description: 'Inspect the MCP tool catalog and run a real tool call from the browser when you want to test quickly.',
    },
    {
      src: '/screenshots/system-models.png',
      alt: 'Aviary models page showing searchable provider and model cards',
      title: 'Models',
      description: 'Search the built-in model catalog by provider and capability before assigning defaults or fallbacks.',
    },
  ]"
/>
</section>

<section class="content-section">
<p class="section-eyebrow">Core Workflows</p>

<h1 class="section-heading">What You Use Aviary For</h1>

<div class="panel-grid">
  <div class="panel-card accent-chat">
    <FeatureIcon name="chat"></FeatureIcon>
    <h3>Live Conversations</h3>
    <p>Chat with your agents in real time, see what tools they're calling, attach files, and pick up where you left off without losing context.</p>
  </div>
  <div class="panel-card accent-message messaging-card">
    <div class="messaging-logos" aria-label="Supported messaging channels">
      <MessagingLogo name="signal"></MessagingLogo>
      <MessagingLogo name="slack"></MessagingLogo>
      <MessagingLogo name="discord"></MessagingLogo>
    </div>
    <h3>Chat Where Your Team Already Is</h3>
    <p>Drop your agent into Signal, Slack, or Discord and it shows up like any other team member — no new app, no new login, no context switching.</p>
  </div>
  <div class="panel-card accent-flow">
    <FeatureIcon name="clock"></FeatureIcon>
    <h3>Scheduled Tasks And Automation</h3>
    <p>Set a task to run on a schedule or whenever a file changes. Aviary automatically converts routine tasks into fast, free scripts so they never burn tokens just for running on repeat.</p>
  </div>
  <div class="panel-card accent-catalog">
    <FeatureIcon name="browser"></FeatureIcon>
    <h3>Browser Automation</h3>
    <p>Let your agents browse the web, fill out forms, and scrape pages — all through a shared browser session that gets cleaned up automatically when each task finishes.</p>
  </div>
  <div class="panel-card accent-ops">
    <FeatureIcon name="chart"></FeatureIcon>
    <h3>Usage And Analytics</h3>
    <p>See exactly where your tokens are going, check on running jobs, and dig into failures — all while everything is live.</p>
  </div>
  <div class="panel-card accent-catalog">
    <FeatureIcon name="tools"></FeatureIcon>
    <h3>Skills, Tools, And Models</h3>
    <p>Browse available AI models, see what tools your agents can use, and manage the skills installed on your system — all in one place.</p>
  </div>
  <div class="panel-card accent-flow">
    <FeatureIcon name="controls"></FeatureIcon>
    <h3>Web Dashboard</h3>
    <p>Configure agents, set permissions, connect channels, and tweak settings from a clean web UI instead of hand-editing config files.</p>
  </div>
  <div class="panel-card accent-ops">
    <FeatureIcon name="cli"></FeatureIcon>
    <h3>Terminal Control</h3>
    <p>Run commands, trigger tasks, tail logs, and manage agents directly from the terminal whenever you prefer the keyboard over a browser.</p>
  </div>
</div>
</section>

<section class="content-section content-section-alt content-section-last">
<p class="section-eyebrow">Guide Coverage</p>

<h2 class="section-heading">Read This First</h2>

<div class="detail-grid">
  <a class="detail-card detail-link" href="./guide/getting-started">
    <h3>Getting Started</h3>
    <p>Install Aviary, start the server, log into the dashboard, and get your first agent running in a few minutes.</p>
  </a>
  <a class="detail-card detail-link" href="./guide/configuration">
    <h3>Configuration</h3>
    <p>Learn how to change how Aviary behaves — models, permissions, agents, and everything else in <code>aviary.yaml</code>.</p>
  </a>
  <a class="detail-card detail-link" href="./guide/channels">
    <h3>Channels</h3>
    <p>Connect Aviary to Slack, Discord, or Signal and set up sender rules, mentions, and delivery targets.</p>
  </a>
  <a class="detail-card detail-link" href="./guide/security-permissions">
    <h3>Security &amp; Permissions</h3>
    <p>Lock down agent access, harden the server, and choose sensible host-level isolation for safer deployments.</p>
  </a>
  <a class="detail-card detail-link" href="./guide/scheduled-tasks">
    <h3>Scheduled Tasks</h3>
    <p>Set up tasks that run on a timer or when files change, and learn how Aviary can run them for free by compiling them into scripts.</p>
  </a>
  <a class="detail-card detail-link" href="./guide/operations">
    <h3>Day-to-Day Operations</h3>
    <p>Manage chats, check on jobs, read logs, and handle the everyday stuff while Aviary is running.</p>
  </a>
  <a class="detail-card detail-link" href="./reference/mcp/">
    <h3>MCP Tool Reference</h3>
    <p>Look up exact tool names, inputs, and behavior when you're building automations or writing agent scripts.</p>
  </a>
</div>

</section>
