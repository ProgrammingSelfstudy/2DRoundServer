# Admin web UI

A single-page UI for the admin API, served by `cmd/admin` at `/` (default
`http://127.0.0.1:8080/`). It is plain ES modules in `internal/admin/web/`, embedded
with `go:embed`: no framework, no build step, nothing from outside the server.

Log in with an admin account (the first start prints the bootstrap password once, see
`docs/ADMIN_API.md`).

## Pages

| page | who | what |
|---|---|---|
| 概览 | everyone; operators may broadcast | counters and the number of roles online (asks the gateway); a box that scrolls a message across the top of every online player's screen |
| 区服 | read; manage may edit | list, create, edit, maintenance toggle, delete |
| 账号 | read; operator may ban | search, ban with a reason, unban |
| 角色 | read; operator may ban and kick | search by name / account / id, filter by server, "详情" (online state, currencies, item ledger with names; operators can set the level and VIP, kick, and grant currencies and items straight into the role from it), ban, unban, "发邮件" shortcut |
| 邮件 | read; operator may send | compose (direct or broadcast, currency and item attachments, expiry) and the list of sent mail with recipients and claims; items are picked by name or id from the client's item table (type in the box, choose from the list), and the sent list shows their names |
| 公告 | read; operator may edit | create, edit, delete; online players see the change at once and the toast says how many were told |
| 审计日志 | superadmin | who did what, when, from where |
| 数据库备份 | manage | the backups on disk (name, size, time) and a button that makes one now (the old ones beyond `admin.backup_keep` are removed); restoring is a manual step, described on the page |
| 管理员 | superadmin | create, change role, enable or disable, reset password |
| 我的账号 | everyone | change your own password |

The navigation hides what a role may not use, and the server refuses it anyway.
The forms check their input before sending (the same limits as the API) and show the
server's message when it still refuses.

## How it is kept safe

- **No markup from data.** Role names, mail text and notices come from players or
  operators. The UI builds the page with `textContent` only (`web/dom.js`), never
  `innerHTML`, so `<img onerror=...>` or `<script>` in a name is shown as text.
- **Strict Content-Security-Policy**: `default-src 'none'; script-src 'self'; style-src
  'self'; connect-src 'self'; frame-ancestors 'none'` and no inline script, inline style
  or eval anywhere in the UI. The server also sends `X-Frame-Options: DENY`,
  `Referrer-Policy: no-referrer`, `nosniff` and `no-store` on API replies.
- The token lives in `sessionStorage` (gone when the tab closes), is sent only as a
  bearer header, and is dropped on the first 401, which sends you to the login page.
- Deleting, banning and putting a server into maintenance ask for confirmation in the
  page (Escape or a click outside counts as "no").
- The static handler serves only the embedded files; a directory, an unknown path or
  anything under `/api/` is a 404.

## Tests

- `go test ./internal/admin` covers serving: headers, ETag revalidation, files-only,
  and that the API is unaffected.
- `node --test internal/admin/webtest/lib.test.mjs` covers the pure logic in `web/lib.js`
  (permission levels, time conversion including impossible dates, ID parsing, the
  mail / server / notice request builders and their limits, paging, error texts).
- The pages themselves were checked in a real browser: login (wrong and right), every
  page, creating and deleting a server with validation, composing and sending mail,
  banning a role with the confirm dialog, the read-only role's reduced menu, the
  expired-session flow, a narrow window, and a role name and mail subject containing
  HTML and script.
  Against the real stack with a phone: mail sent from the page arrived live on the
  device, and banning a role from the page disconnected it.

## Known limits

- The server's error messages are English; the UI translates the fixed ones
  (`web/lib.js`, `zhError`) and shows the rest as the server wrote them.
- There is no TLS, no CORS and no logout-everywhere button; put the service behind a TLS
  proxy on the same origin. Changing a password ends every session of that admin.
- Item attachments take a raw item id because no item table has been extracted.
- Broadcast mail recipients fill in as players list their mail, so "收件" on a broadcast
  counts the roles reached so far.
