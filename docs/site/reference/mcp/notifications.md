# Notification Tools

Tools for Aviary's [notifications](/guide/notifications).

---

## notify

Send the user a notification. It reaches the bell, a banner or toast, a system notification, and sound, as their **Settings → Notifications** allow; with notifications off, nothing is sent.

**Arguments:**

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `title` | string | ✓ | A short headline. |
| `body` | string | | Optional detail. |
| `severity` | string | | `info` (default), `success`, `warning` or `error`. |
| `link` | string | | A web UI path the notification opens, such as `/agents/assistant/chat/main`. |

**Returns:** `outcome` (`delivered`, `merged` into a recent identical one, `grouped` with a burst, or `suppressed` by the settings) and the `notification`.

---

## notification_list

List the recent notifications in the bell, newest first, with `unread`, how many are unread.

---

## notification_settings_get

Return the notification `settings`, plus the `eventTypes` and `sounds` available.

```json
{
  "settings": {
    "enabled": true,
    "system": true,
    "inApp": true,
    "server": false,
    "sound": { "enabled": false, "type": "chime", "volume": 60 },
    "quietHours": { "enabled": false, "start": "22:00", "end": "07:00" },
    "events": {
      "codeloop.failed": { "enabled": true, "delivery": "big" }
    }
  }
}
```

`delivery` is `big` (a banner), `small` (a toast) or `system`. An event's `sound` is empty for the default sound, `none`, or a sound name.

---

## notification_settings_set

Replace the notification settings.

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `settings` | object | | The full settings object, as `notification_settings_get` returns it under `settings`. Values Aviary does not offer fall back to their defaults. |
| `reset` | boolean | | Restore the defaults instead. |
