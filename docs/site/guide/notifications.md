# Notifications

Aviary tells you when something happens while you're looking elsewhere: an agent replies in another chat, a Code Loop job fails or waits on you, or a scheduled task or workflow finishes or fails. Notifications are controlled from **Settings → Notifications**, and the settings are saved on the server, so they follow you to every browser and device.

## Where notifications appear

Every notification goes to the **bell** next to the connection icon (in the sidebar footer, the collapsed rail, or the phone header). It shows the unread count; click it for the recent notifications, newest first, with **Mark all read** and **Clear**. Clicking a notification anywhere opens its page: the chat, the Code Loop job card, the task run, or the task.

Each event type also has a delivery style:

| Style | What it does |
| --- | --- |
| **Banner** | A banner across the top of the page, for important events. |
| **Toast** | A small notice in the bottom corner. |
| **System** | A notification from the browser or the installed app, also while Aviary is in the background. Without permission it falls back to a toast. |

A notification for the page you are viewing is never shown; it is marked read instead. While the tab is hidden, notifications that can reach you through the system do.

### System notifications

Turn on **System notifications** and click **Allow notifications…** to grant the browser permission. Browsers only offer this on a secure (HTTPS) connection, which Aviary serves by default. The settings page says when permission is denied, when the browser can't show notifications, or when the page isn't secure; in-app banners and toasts still work in every case. The installed app shows them as well, and clicking one opens its page.

### macOS without a browser

On a Mac, **macOS Notification Center when no browser is open** has the server show notifications itself while no Aviary page is connected. It uses [terminal-notifier](https://github.com/julienXX/terminal-notifier) when it is installed, so clicking a notification opens its page, and `osascript` otherwise. Whether they appear as banners or alerts is set in macOS **System Settings → Notifications**, under terminal-notifier or Script Editor.

## Events

| Section | Event | On by default | Style |
| --- | --- | --- | --- |
| Chats | An agent replied in one of your chats | Yes | Toast |
| Code Loop | A job failed | Yes | Banner |
| Code Loop | A job is blocked or waiting on you | Yes | Banner |
| Code Loop | A job is done or its pull request merged | No | Toast |
| Scheduled tasks & workflows | A run with **Send via: Notify** finished | Yes | Toast |
| Scheduled tasks & workflows | A workflow failed | Yes | Toast |
| Agents & API | Sent by an agent's `notify` tool or the API | Yes | Toast |

Each can be turned off, given another style, or given its own sound. The **Notifications** switch at the top turns everything off at once.

A burst of events, such as many jobs failing together, doesn't arrive one by one: the same event repeated within a minute counts up on one notification, and after three notifications of one type within 30 seconds the rest arrive together as a single notification that lists them.

## Sound

Sound is **off** by default. Turn it on to pick a sound (chime, bell, ping, pop, marimba or alert, synthesized in the browser, so nothing is downloaded) and a volume; **Play** previews it. Each event type can use the default sound, its own, or none.

## Quiet hours

With **Quiet hours** on, notifications between the two times (in the server's time zone) go to the bell only, with no banner, toast, system notification or sound.

**Test notification** sends a sample through every enabled method.

## Notify from tasks and workflows

Set a scheduled task's or workflow's **Send via** to **Notify** to get its result as a notification, with the run's summary, linking to the run. **Notify on failure only** skips successful runs. In configuration, the targets are `notify` and `notify?failures`:

```yaml
tasks:
  - name: nightly-backup-check
    schedule: "0 6 * * *"
    prompt: "Check last night's backups and report any problems."
    target: notify?failures
```

## Notify from agents and the API

Agents send notifications with the [`notify` tool](/reference/mcp/notifications#notify). Anything else can `POST /api/notifications` with the bearer token and a JSON body with `title` (required), `body`, `severity` (`info`, `success`, `warning` or `error`) and `link` (a path in the web UI):

```sh
curl -k -X POST https://localhost:16677/api/notifications \
  -H "Authorization: Bearer $(cat ~/.config/aviary/token)" \
  -d '{"title":"Deploy finished","body":"v2.3 is live","severity":"success"}'
```

`GET /api/notifications` lists the history, `POST /api/notifications/read` marks notifications read (`{"ids": [...]}`, or all with no IDs), and `DELETE /api/notifications` clears it. The settings are read and written at `/api/preferences/notifications` (`GET`, `PUT`, and `DELETE` to reset), or with the `notification_settings_get` and `notification_settings_set` tools.
