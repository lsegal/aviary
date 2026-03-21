---
layout: home

hero:
  name: Aviary
  text: The AI Agent Nest
  tagline: Aviary is a full AI assistant platform. Connect your AI models to Slack, Signal, Discord, etc., have conversations, set up scheduled tasks, and let your agents work for you. All managed from a CLI or a web-based control panel.
  image:
    src: /logo.png
    alt: Aviary logo
  actions:
    - theme: brand
      text: Read the guide
      link: /guide/
    - theme: alt
      text: Browse the reference
      link: /reference/

features:
  - title: Connected Assistants
    details: Hook models into Slack, Signal, Discord, and other channels so your agents can meet users where work is already happening.
  - title: Conversations And Tasks
    details: Run real-time chats, keep persistent sessions, and schedule recurring or file-triggered work from the same platform.
  - title: CLI And Control Panel
    details: Operate Aviary from the terminal when you want direct control, or use the web UI when you need visibility across the whole system.
---

Aviary is a full AI assistant platform, not just a chat box or a model picker. It is built to connect models to real channels, let agents carry ongoing conversations, schedule work in the background, and give you both a CLI and a web control panel for managing the system.

<section class="content-section">
<p class="section-eyebrow">Core Workflows</p>

<h1 class="section-heading">What You Use Aviary For</h1>

<div class="panel-grid">
  <div class="panel-card accent-chat">
    <FeatureIcon name="chat" />
    <h3>Conversations That Stay Live</h3>
    <p>Run real agent sessions, inspect tool calls, attach files, branch conversations, and manage active work without dropping into raw logs first.</p>
  </div>
  <div class="panel-card accent-message messaging-card">
    <div class="messaging-logos" aria-label="Supported messaging channels">
      <MessagingLogo name="signal" />
      <MessagingLogo name="slack" />
      <MessagingLogo name="discord" />
    </div>
    <h3>Messaging Where People Already Work</h3>
    <p>Deploy agents into Signal, Slack, and Discord so Aviary shows up as a real operator in the rooms your team already uses, instead of forcing everyone into a separate app first.</p>
  </div>
  <div class="panel-card accent-flow">
    <FeatureIcon name="clock" />
    <h3>Scheduled Tasks And Automation</h3>
    <p>Turn prompts and scripts into scheduled or file-triggered jobs, then follow compile state, retries, outputs, and delivery targets in one workflow.</p>
  </div>
  <div class="panel-card accent-ops">
    <FeatureIcon name="chart" />
    <h3>Usage And Analytics</h3>
    <p>Break down consumption, inspect session activity, see job status, and debug failures, throttles, and orchestrator behavior while work is live.</p>
  </div>
  <div class="panel-card accent-catalog">
    <FeatureIcon name="tools" />
    <h3>Skills, Tools, And Models</h3>
    <p>Browse the catalog of available models, inspect installed skills, review the MCP tool surface, and understand what capabilities are actually wired into the system.</p>
  </div>
  <div class="panel-card accent-flow">
    <FeatureIcon name="controls" />
    <h3>Control Panel For Real Operations</h3>
    <p>Manage agents, providers, permissions, channels, browser settings, and runtime configuration from the web UI instead of stitching together local notes and config edits.</p>
  </div>
  <div class="panel-card accent-ops">
    <FeatureIcon name="cli" />
    <h3>CLI When You Want Direct Control</h3>
    <p>Use the terminal for day-to-day commands and direct execution, with the control panel acting as the visual layer for sessions, jobs, health, and inspection.</p>
  </div>
</div>
</section>

<section class="content-section content-section-alt">
<p class="section-eyebrow">Guide Coverage</p>

<h2 class="section-heading">Read This First</h2>

<div class="detail-grid">
  <a class="detail-card detail-link" href="./guide/getting-started">
    <h3>Getting Started</h3>
    <p>Install Aviary, start the server, authenticate the control panel, and get to the first successful agent run without guesswork.</p>
  </a>
  <a class="detail-card detail-link" href="./guide/configuration">
    <h3>Configuration</h3>
    <p>Learn the runtime model when you need to change how Aviary behaves, not just use what is already running.</p>
  </a>
  <a class="detail-card detail-link" href="./guide/operations">
    <h3>Operating Aviary</h3>
    <p>Work through chats, sessions, jobs, logs, daemon operations, validation, and the control panel paths you use when things are live.</p>
  </a>
  <a class="detail-card detail-link" href="./reference/mcp/">
    <h3>MCP Reference</h3>
    <p>Use the MCP reference when you need schemas, examples, and exact tool behavior instead of broad product copy.</p>
  </a>
</div>

</section>
