# SPEC: Chrome Web Store Publication

**Date:** 2026-05-08
**Status:** implemented

## Goal

Prepare the Vector Bookmark extension for submission to the Chrome Web Store. The extension
is functionally complete. This spec covers the compliance artifacts (privacy policy), manifest
hardening, version promotion, and store listing copy — everything needed before hitting
"Submit for review" on the developer dashboard.

## Behavior

### What changes in this spec

- When the store reviewer loads the extension, they see version 1.0.0 with a homepage URL
  and minimum Chrome version declared.
- When Google's dashboard asks for a Privacy Policy URL, we point to the raw GitHub file
  `https://raw.githubusercontent.com/drecchia/chrome-plugin-vector-bookmark/main/PRIVACY.md`.
- When a user reads the store listing, they get a clear, accurate description of what the
  extension does and what permissions it needs.

### Edge cases / constraints

- Privacy policy must be web-accessible before submission. GitHub raw URL works immediately
  without any extra hosting setup.
- `<all_urls>` host permission will trigger "single purpose" review. The store listing
  description must justify it explicitly (needed to track dwell time on any page).
- The extension binary (dist zip) must be built from `extension/` with `npm run build` and
  zipped — no `node_modules/` or source maps in the package.

## Implementation Outline

- `extension/manifest.json` — bump version to `1.0.0`, add `homepage_url`, add
  `minimum_chrome_version`, add `content_security_policy`
- `extension/package.json` — bump version to `1.0.0`
- `PRIVACY.md` (repo root) — new file: privacy policy for a fully local extension
- `docs/STORE_LISTING.md` — new file: store title, short desc, long desc, category,
  screenshots mapping, and submission checklist

## API / Interface

No new runtime interfaces. The manifest changes are metadata-only.

**Manifest additions:**
```json
"version": "1.0.0",
"minimum_chrome_version": "116",
"homepage_url": "https://github.com/drecchia/chrome-plugin-vector-bookmark",
"content_security_policy": {
  "extension_pages": "script-src 'self'; object-src 'self'"
}
```

## Acceptance Criteria

1. [x] `extension/manifest.json` version is `1.0.0` and `minimum_chrome_version` is set
2. [x] `extension/manifest.json` has `homepage_url` pointing to the GitHub repo
3. [x] `extension/manifest.json` has a `content_security_policy` for extension pages
4. [x] `extension/package.json` version matches `1.0.0`
5. [x] `PRIVACY.md` exists at repo root, covers: data collected, storage location, third-party
       sharing (none), user rights, contact
6. [x] `docs/STORE_LISTING.md` exists with title, short desc (≤132 chars), long desc, category,
       screenshots list, and a submission checklist
7. [x] `npm run typecheck` in `extension/` succeeds (build verified at TS level)
8. [x] No secrets, `.env` files, or `node_modules/` in tracked sources (`.gitignore` covers them)
9. [x] Promo tiles 440×280 and 1400×560 generated in `store/` (was Out of Scope, completed)
10. [x] 5 screenshots resized to 1280×800 in `store/screenshots/`

## Out of Scope

- Uploading to the Chrome Web Store developer dashboard — manual step (one-time, $5 fee)
- Pushing `PRIVACY.md` to GitHub `main` so the raw URL is reachable by reviewers — user
  decides when to commit/push
- Changing the extension's functionality, permissions, or UI
