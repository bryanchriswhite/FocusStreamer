# FocusStreamer Chrome Extension
# FocusStreamer Chrome Extension

This extension provides a popup UI and background updates for FocusStreamer URL-based allowlisting.

## Load in Chrome/Brave
1. Open `chrome://extensions`
2. Enable Developer mode
3. Click **Load unpacked**
4. Select the `FocusStreamer/extension` folder

## What it does
- Sends the active tab URL/title to FocusStreamer for URL allowlisting.
- Provides quick actions to allow page/domain/subdomain rules.
- Toggles allow/block for the detected browser window class.

## Backend requirements
FocusStreamer should be running locally and exposing these endpoints:
- `POST http://127.0.0.1:8080/api/browser/active`
- `POST http://127.0.0.1:8080/api/browser/allowlist`
- `POST http://127.0.0.1:8080/api/config/url-rules`
- `DELETE http://127.0.0.1:8080/api/config/url-rules/{id}`
- `GET http://127.0.0.1:8080/api/window/current`
- `GET http://127.0.0.1:8080/api/browser/status`
- `GET http://127.0.0.1:8080/api/window/allowlist-status`
- `GET http://127.0.0.1:8080/api/config`

## Notes
- The background worker posts updates on tab activation, title/URL changes, and window focus.
- An offscreen document sends a heartbeat every 2 seconds to keep the browser context fresh.
