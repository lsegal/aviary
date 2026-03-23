# Browser and Channel Tools

Browser tools control a Chrome or Chromium instance via the Chrome DevTools Protocol (CDP). Channel tools deliver files and messages to configured messaging platforms.

## Prerequisites

Browser tools require a running Chrome or Chromium binary. Aviary connects via CDP on the port configured in `browser.cdp_port` (default: `9222`). The first browser tool call launches the browser if it is not already running.

Configure the binary path and profile directory in `aviary.yaml`:

```yaml
browser:
  binary: /usr/bin/chromium
  cdp_port: 9222
  profile_directory: ~/.config/aviary/browser
  headless: false
```

Each browser session is identified by a **`tab_id`** returned when a tab is opened. Pass this ID to all subsequent operations on that tab.

---

## browser_open

Open a URL in a new browser tab and return the tab ID.

**Arguments:**

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `url` | string | yes | URL to navigate to |

**Returns:** JSON object with the new tab ID and current URL.

```json
{ "tab_id": "CDC1", "url": "https://example.com" }
```

---

## browser_tabs

List all currently open browser tabs.

**Arguments:** none

**Returns:** JSON array of tab objects, each with `tab_id`, `url`, and `title`.

---

## browser_navigate

Navigate an existing browser tab to a new URL.

**Arguments:**

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `tab_id` | string | yes | Tab identifier |
| `url` | string | yes | URL to navigate to |

**Returns:** JSON `{ tab_id, url }` after the page has loaded.

---

## browser_wait

Wait for a CSS selector to become visible in the specified tab. Times out after `timeout_ms` milliseconds (maximum 60 000 ms).

**Arguments:**

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `tab_id` | string | yes | Tab identifier |
| `selector` | string | yes | CSS selector to wait for |
| `timeout_ms` | int | | Timeout in milliseconds (default: 5000, max: 60000) |

**Returns:** JSON `{ tab_id, selector, timeout_ms, status: "visible" }` when the element appears.

---

## browser_click

Click an element matched by a CSS selector.

**Arguments:**

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `tab_id` | string | yes | Tab identifier |
| `selector` | string | yes | CSS selector of the element to click |

**Returns:** Text confirmation.

---

## browser_keystroke

Send keystrokes to an element. Use this for special keys (e.g. `Enter`, `Tab`) or key combinations.

**Arguments:**

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `tab_id` | string | yes | Tab identifier |
| `selector` | string | yes | CSS selector of the target element |
| `text` | string | yes | Key names or text to send |

**Returns:** Text confirmation.

---

## browser_fill

Fill an input element with text by replacing its current value.

**Arguments:**

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `tab_id` | string | yes | Tab identifier |
| `selector` | string | yes | CSS selector of the input element |
| `text` | string | yes | Text to enter |

**Returns:** Text confirmation.

---

## browser_text

Extract normalized visible text from the page or from elements matching a CSS selector.

**Arguments:**

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `tab_id` | string | yes | Tab identifier |
| `selector` | string | | CSS selector; omit to extract the whole page |
| `max_length` | int | | Maximum characters to return (default: 4000, max: 20000) |

**Returns:** JSON object.

```json
{
  "tab_id": "CDC1",
  "url": "https://example.com",
  "title": "Example Domain",
  "selector": "",
  "match_count": 1,
  "text": "Example Domain\nThis domain is for use in illustrative examples..."
}
```

---

## browser_query

Extract structured data from elements matching a CSS selector.

**Arguments:**

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `tab_id` | string | yes | Tab identifier |
| `selector` | string | yes | CSS selector |
| `count` | int | | Maximum elements to return (default: 20, max: 100) |
| `max_text_length` | int | | Maximum text per element (default: 500, max: 5000) |
| `include_html` | bool | | Include raw `outerHTML` for each element |

**Returns:** JSON object with a `items` array; each item contains `index`, `tag_name`, `text`, and common attributes (`href`, `src`, `value`, `aria_label`, optionally `html`).

---

## browser_screenshot

Capture a screenshot of the specified tab and save it to the browser media directory.

**Arguments:**

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `tab_id` | string | yes | Tab identifier |

**Returns:** Text path to the saved PNG file.

**Side effects:** Writes a PNG file to `~/.config/aviary/browser/media/`.

---

## browser_resize

Resize the browser window containing the specified tab.

**Arguments:**

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `tab_id` | string | yes | Tab identifier |
| `width` | int | yes | Window width in pixels |
| `height` | int | yes | Window height in pixels |

**Returns:** Text confirmation.

---

## browser_eval

Evaluate JavaScript in the specified tab and return the result.

**Arguments:**

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `tab_id` | string | yes | Tab identifier |
| `javascript` | string | yes | JavaScript expression or statements to evaluate |

**Returns:** Text representation of the evaluation result.

---

## browser_close

Close an existing browser tab.

**Arguments:**

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `tab_id` | string | yes | Tab identifier to close |

**Returns:** JSON `{ tab_id, closed: true }`.

---

## channel_send_file

Send a local file to the current conversation channel. Use this to share screenshots or generated files with the user rather than asking them to open a path manually.

**Arguments:**

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `file_path` | string | yes | Absolute path to the local file |
| `caption` | string | | Optional text to accompany the file |

**Returns:** Text confirmation including the file path.

**Side effects:** Delivers the file to the channel connected to the current session. If no channel target is set, the file is persisted as session media instead.
