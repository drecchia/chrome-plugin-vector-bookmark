# Privacy Policy — Vector Bookmark

**Last updated:** 2026-05-08

Vector Bookmark is a Chrome extension that builds a personal, **fully local** semantic
index of the pages you read. This policy explains exactly what the extension touches and
what it does not.

## TL;DR

- All your browsing data stays on your machine.
- Vector Bookmark does not have any servers, analytics, or tracking.
- The extension talks only to a local daemon (`vbmd`) running on `127.0.0.1:7532` on
  your own computer.
- If you optionally configure an LLM/embedding provider (e.g. OpenAI-compatible API),
  page text you choose to summarize or tag is sent to that provider — entirely under
  your control.

## What data the extension collects

When you spend at least 10 seconds on a page (configurable), the extension captures:

- The page **URL**, **title**, and **HTML meta tags** (description, keywords, og:*, author).
- The **dwell time** (time the page was visible).
- On manual indexing, the **page body text** extracted with Mozilla Readability.
- Optional **tags** you assign to a page yourself.

The extension never captures:

- Form inputs, password fields, or anything inside `<input type="password">`.
- Pages opened in **Incognito** windows (the manifest declares `incognito: "not_allowed"`).
- Pages whose hostname matches your user-managed blacklist.

## Where data is stored

All captured data is sent over `localhost` HTTP to the `vbmd` daemon, which stores it in a
**local SQLite database** under your operating system's user directory:

- Linux: `~/.local/share/vbm/`
- Windows: `%APPDATA%\vbm\`
- Custom: wherever `VBM_DATA_DIR` is set (e.g. inside a Docker volume).

The database never leaves your machine.

## What data is shared with third parties

By default: **none**.

If — and only if — you configure an external embedding or LLM endpoint by setting
`VBM_EMBED_URL` and `VBM_EMBED_API_KEY` in the daemon environment, the daemon will send
text to that endpoint for the following operations you trigger explicitly:

- Generating vector embeddings for indexed page text.
- Generating LLM summaries (when you choose the `llm_summary` ingest mode in the popup).
- Suggesting tags (when you click the ✨ button next to the tag input).

You choose the provider, you provide the API key, and you control which mode is used per
page. No data is sent to any third party automatically.

## Permissions and why we need them

| Permission | Why |
|---|---|
| `tabs`, `webNavigation` | Detect navigation events to start dwell timing. |
| `storage` | Save your settings (daemon host/port, dwell threshold, blacklist). |
| `scripting` | Inject the content script that extracts page text on demand. |
| `omnibox` | Provide the `@recall` omnibox keyword for semantic search. |
| `idle` | Pause dwell tracking when the system is idle. |
| `host_permissions: <all_urls>` | The extension's purpose is recalling **any** page you read; it must observe all sites you visit (subject to your blacklist). |

## Your rights and controls

- **Pause capture:** stop the daemon (`systemctl --user stop vbmd` on Linux, or close the
  process on Windows).
- **Block sites:** add hostname patterns to the user blacklist in the popup.
- **Forget specific pages:** call `DELETE /forget` on the daemon, or use the UI at
  `http://127.0.0.1:7532/`.
- **Wipe everything:** delete the data directory listed above.
- **Uninstall:** removing the extension and stopping/uninstalling the daemon removes all
  capture and access.

## Children's privacy

The extension is not directed to children under 13 and does not knowingly process their
data.

## Changes to this policy

Material changes to this policy will be reflected in this file with an updated date. The
file is versioned in the public GitHub repository, so the full history is auditable.

## Contact

Questions or concerns: open an issue at
<https://github.com/drecchia/chrome-plugin-vector-bookmark/issues>.
