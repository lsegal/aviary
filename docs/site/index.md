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
    <div class="panel-icon icon-spark"></div>
    <h3>Conversations That Stay Live</h3>
    <p>Run real agent sessions, inspect tool calls, attach files, branch conversations, and manage active work without dropping into raw logs first.</p>
  </div>
  <div class="panel-card accent-message messaging-card">
    <div class="messaging-logos" aria-label="Supported messaging channels">
      <span class="messaging-logo signal" aria-label="Signal" title="Signal">
        <svg viewBox="0 0 24 24" aria-hidden="true">
          <path d="M12 1.75A10.25 10.25 0 0 0 3.38 17.57L2.14 22l4.57-1.18A10.25 10.25 0 1 0 12 1.75Zm0 1.8a8.45 8.45 0 1 1-4.15 15.8l-.32-.18-2.63.68.71-2.54-.2-.32A8.45 8.45 0 0 1 12 3.55Zm0 2.18c.65 0 1.17.52 1.17 1.17s-.52 1.17-1.17 1.17-1.17-.52-1.17-1.17S11.35 5.73 12 5.73Zm3.63 1.13c.62 0 1.12.5 1.12 1.12s-.5 1.12-1.12 1.12-1.12-.5-1.12-1.12.5-1.12 1.12-1.12Zm-7.26 0c.62 0 1.12.5 1.12 1.12S8.99 9.1 8.37 9.1s-1.12-.5-1.12-1.12.5-1.12 1.12-1.12Zm8.77 2.46c.58 0 1.04.47 1.04 1.04s-.46 1.04-1.04 1.04-1.04-.47-1.04-1.04.47-1.04 1.04-1.04Zm-10.28 0c.58 0 1.04.47 1.04 1.04s-.47 1.04-1.04 1.04-1.04-.47-1.04-1.04.47-1.04 1.04-1.04Zm10.79 2.43c.49 0 .89.4.89.89s-.4.89-.89.89-.89-.4-.89-.89.4-.89.89-.89Zm-11.3 0c.49 0 .89.4.89.89s-.4.89-.89.89-.89-.4-.89-.89.4-.89.89-.89Zm10.73 2.3c.43 0 .78.35.78.78s-.35.78-.78.78-.78-.35-.78-.78.35-.78.78-.78Zm-10.16 0c.43 0 .78.35.78.78s-.35.78-.78.78-.78-.35-.78-.78.35-.78.78-.78Z" />
        </svg>
      </span>
      <span class="messaging-logo slack" aria-label="Slack" title="Slack">
        <svg viewBox="0 0 24 24" aria-hidden="true">
          <path d="M9.74 2a2.26 2.26 0 1 0 0 4.52h2.26V4.26A2.26 2.26 0 0 0 9.74 2Zm0 6.08H4.26a2.26 2.26 0 1 0 0 4.52h5.48a2.26 2.26 0 0 0 0-4.52Zm12-3.82A2.26 2.26 0 0 0 19.48 2a2.26 2.26 0 0 0-2.26 2.26v5.48a2.26 2.26 0 1 0 4.52 0V4.26ZM14.26 9.74A2.26 2.26 0 0 0 12 12a2.26 2.26 0 0 0 2.26 2.26h5.48a2.26 2.26 0 1 0 0-4.52h-5.48Zm3.22 4.52H12a2.26 2.26 0 1 0 0 4.52h5.48a2.26 2.26 0 1 0 0-4.52Zm-5.48 0A2.26 2.26 0 0 0 9.74 16.52V22a2.26 2.26 0 1 0 4.52 0v-5.48A2.26 2.26 0 0 0 12 14.26ZM6.52 14.26A2.26 2.26 0 1 0 4.26 12 2.26 2.26 0 0 0 6.52 14.26Zm7.74-7.74A2.26 2.26 0 0 0 16.52 4.26 2.26 2.26 0 0 0 14.26 2a2.26 2.26 0 0 0 0 4.52Z" />
        </svg>
      </span>
      <span class="messaging-logo discord" aria-label="Discord" title="Discord">
        <svg viewBox="0 0 24 24" aria-hidden="true">
          <path d="M20.32 4.37A16.7 16.7 0 0 0 16.2 3.1l-.2.4c1.6.42 2.35 1.02 2.35 1.02a13.3 13.3 0 0 0-4.11-.63c-1.39 0-2.77.21-4.09.63 0 0 .77-.65 2.57-1.07l-.14-.35a16.5 16.5 0 0 0-4.12 1.27C5.86 8.1 5.16 11.73 5.16 11.73a16.8 16.8 0 0 0 5.06 2.58l.68-1.08c-.94-.34-1.63-.76-2.17-1.14.46.33 1.16.71 2.14 1.03 1.38.45 2.86.45 4.24 0 .98-.32 1.68-.7 2.14-1.03-.54.38-1.23.8-2.17 1.14l.68 1.08a16.8 16.8 0 0 0 5.08-2.58s-.7-3.63-2.52-7.36ZM9.88 12.1c-.83 0-1.5-.76-1.5-1.68s.67-1.68 1.5-1.68c.84 0 1.51.76 1.5 1.68 0 .92-.67 1.68-1.5 1.68Zm4.24 0c-.83 0-1.5-.76-1.5-1.68s.67-1.68 1.5-1.68c.84 0 1.51.76 1.5 1.68 0 .92-.66 1.68-1.5 1.68Z" />
        </svg>
      </span>
    </div>
    <h3>Messaging Where People Already Work</h3>
    <p>Deploy agents into Signal, Slack, and Discord so Aviary shows up as a real operator in the rooms your team already uses, instead of forcing everyone into a separate app first.</p>
  </div>
  <div class="panel-card accent-flow">
    <div class="panel-icon icon-node"></div>
    <h3>Scheduled Tasks And Automation</h3>
    <p>Turn prompts and scripts into scheduled or file-triggered jobs, then follow compile state, retries, outputs, and delivery targets in one workflow.</p>
  </div>
  <div class="panel-card accent-ops">
    <div class="panel-icon icon-grid"></div>
    <h3>Usage And Analytics</h3>
    <p>Break down consumption, inspect session activity, see job status, and debug failures, throttles, and orchestrator behavior while work is live.</p>
  </div>
  <div class="panel-card accent-catalog">
    <div class="panel-icon icon-diamond"></div>
    <h3>Skills, Tools, And Models</h3>
    <p>Browse the catalog of available models, inspect installed skills, review the MCP tool surface, and understand what capabilities are actually wired into the system.</p>
  </div>
  <div class="panel-card accent-flow">
    <div class="panel-icon icon-loop"></div>
    <h3>Control Panel For Real Operations</h3>
    <p>Manage agents, providers, permissions, channels, browser settings, and runtime configuration from the web UI instead of stitching together local notes and config edits.</p>
  </div>
  <div class="panel-card accent-ops">
    <div class="panel-icon icon-pulse"></div>
    <h3>CLI When You Want Direct Control</h3>
    <p>Use the terminal for day-to-day commands and direct execution, with the control panel acting as the visual layer for sessions, jobs, health, and inspection.</p>
  </div>
</div>
</section>

<section class="content-section content-section-alt">
<p class="section-eyebrow">Guide Coverage</p>

<h2 class="section-heading">Read This First</h2>

<div class="detail-grid">
  <div class="detail-card">
    <h3>Getting Started</h3>
    <p>Install Aviary, start the server, authenticate the control panel, and get to the first successful agent run without guesswork.</p>
  </div>
  <div class="detail-card">
    <h3>Configuration</h3>
    <p>Learn the runtime model when you need to change how Aviary behaves, not just use what is already running.</p>
  </div>
  <div class="detail-card">
    <h3>Operating Aviary</h3>
    <p>Work through chats, sessions, jobs, logs, daemon operations, validation, and the control panel paths you use when things are live.</p>
  </div>
  <div class="detail-card">
    <h3>MCP Reference</h3>
    <p>Use the MCP reference when you need schemas, examples, and exact tool behavior instead of broad product copy.</p>
  </div>
</div>

<ul class="quick-list">
  <li><a href="/guide/getting-started">Start here</a> to get the server running and open the control panel with valid auth.</li>
  <li><a href="/guide/control-panel">Tour the control panel</a> to understand how the current UI is organized in practice.</li>
  <li><a href="/guide/configuration">Read configuration</a> when you need to change runtime behavior instead of just inspecting it.</li>
  <li><a href="/reference/ui/control-panel">Use the UI reference</a> for route-level detail on what the browser surface exposes.</li>
  <li><a href="/reference/mcp/">Use the MCP reference</a> when you need the callable interface, grouped by capability.</li>
</ul>
</section>
