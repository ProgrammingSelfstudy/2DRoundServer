# Admin API

JSON over HTTP, served by `cmd/admin` (default `127.0.0.1:8080`). Everything except
`POST /api/login` and `GET /api/healthz` needs `Authorization: Bearer <token>`.
Errors are `{"error": "..."}` with the status in the table below.

Run: `go run ./cmd/admin -config configs/dev.yaml`. `admin.jwt_secret` must be a
private random string of at least 32 characters (the service refuses to start
otherwise). On the first start, when there is no admin yet, a superadmin `admin` is
created and its random password is printed once.

## Roles

| role | may do |
|---|---|
| `viewer` | read everything except admins and the audit log |
| `operator` | viewer + send mail, notices, ban/unban accounts and roles |
| `superadmin` | operator + servers, admin management, audit log |

The role, status and password are read from the database on every request, so
demoting or disabling an admin, or changing their password, takes effect at once
(a password change also invalidates all tokens issued before).

## Endpoints

| method and path | level | notes |
|---|---|---|
| `POST /api/login` `{username,password}` | public | -> `{token, expires_at, admin}`; 5 failures per (name, IP) lock it for 15 min (429) |
| `GET /api/me` | read | |
| `POST /api/me/password` `{old,new}` | read | 8 to 72 characters; ends every session |
| `GET /api/stats`, `GET /api/online`, `GET /api/meta` | read | online asks the gateway (`online: null` + `error` when it is down); meta lists currency names and role names |
| `GET/POST /api/admins`, `PATCH /api/admins/{id}` | manage | PATCH takes `role`, `status`, `password`; you cannot lower yourself and one active superadmin always remains |
| `GET/POST /api/servers`, `PUT/DELETE /api/servers/{srv_id}` | read / manage | `srv_id` like `symlf_1`; delete is refused (409) while the server has roles |
| `GET /api/accounts?q=&limit=&offset=` | read | search by name or id; includes `role_count` |
| `POST /api/accounts/{id}/ban` `{reason}`, `.../unban` | write | ban also revokes game tokens and kicks the account's online roles |
| `GET /api/roles?q=&srv_id=` | read | search by role name, account or id |
| `GET /api/roles/{id}` | read | one role with its owner `account`, `online` (`null` + `online_error` when the gateway is down), `banned`, `assets` (`label`, `name`, `val`) and `items` (`base_id`, `name`, `num`, `listed`: whether the game's backpack shows it; equipment is in the ledger but not listed) |
| `GET /api/modules` | read | all 107 runtime registrations (role/global): key, title, window, scope, has_view, has_rank, editable, resets |
| `GET /api/roles/{id}/charge?limit=&offset=` | read | nine concrete charge-module views, role_charge totals and only this role's orders with real items/total/limit/offset pagination |
| `GET /api/roles/{id}/modules` | read | existing saved module rows only: concrete key, registered flag, stored byte count and registry metadata; unknown legacy keys are marked, not silently dropped |
| `GET /api/roles/{id}/modules/{key}` | read | concrete registered role key, exact raw JSON, exists and registry metadata; an absent saved row returns exists=false/data=null without initialization |
| `POST /api/roles/{id}/modules/{key}/reset` | manage | `{item,reason}`; gateway offline check, explicit whitelist, compare-before write and transactional audit; phase 2 enables only the six charge-module whitelists listed below |
| `POST /api/roles/{id}/level` `{lev, kick?}` | write | sets the level (1 to 200); the client reads it at login, so an online role sees it after reconnecting, and `kick: true` disconnects it for that; `{ok, lev, was}`; 422 for a level out of range; audited as `role.level` with the old and new level |
| `POST /api/roles/{id}/vip` `{vip_lev, kick?}` | write | sets the VIP level (0 to 30; 0 is none); the client reads it at login, so an online role sees it after reconnecting, and `kick: true` disconnects it for that; `{ok, vip_lev, was}`; 422 for a level out of range; audited as `role.vip` with the old and new level. The role detail (`GET /api/roles/{id}`) carries `role.vip_lev` |
| `POST /api/roles/{id}/grant` `{grants: [{kind, asset?, item_id?, num}]}` | write | puts currencies (`kind: asset`, by `asset` name from `/api/meta`) and items (`kind: item`, by `item_id`) straight into the role's ledgers, the same ones a claimed mail fills, without a mail; the same validation as mail attachments (1 to 1,000,000,000 each, known currency or item, no currency as an item), 1 to 20 entries, the same entry twice adds up; an online role is sent its currencies (10302) and backpack (10500) at once through the gateway's `POST /push/refresh?rid=`; `{ok, granted, refreshed}`, `refreshed` is `null` and `push_error` set when the gateway cannot be reached (the grant stands); 422 for a bad entry, 404 for an unknown role; audited as `role.grant` with the entries. Granting experience (`exp`) levels the role up |
| `POST /api/roles/{id}/heroes` `{heroes: [{bid, star, num?}]}` | write | gives the role new heroes straight away, made like summoned ones (level 1, the given star; `num` defaults to 1); `bid` and `star` must exist in the client's hero table (`GET /api/meta` lists `heroes` with `init_star` and `max_star`), 1 to 20 heroes in all; the hero-slot limit is not checked; an online role is sent them (11001) through the gateway's `POST /push/refresh?rid=&heroes=<partner ids>`; `{ok, granted: [{partner_id, bid, star}], refreshed}` (`push_error` when the gateway cannot be reached, the heroes stand); 422 for a bad entry, 404 for an unknown role; audited as `role.heroes` |
| `POST /api/roles/{id}/skins` `{skin_id, days?}` | write | gives the role a hero skin (ids in `GET /api/meta` `skins`: `id`, `bid` the hero it belongs to, `name`, `level`), for good (`days` 0 or left out) or for 1 to 3650 days; granting a skin it owns sets the new end time; an online role is sent its skin list (11020) through the gateway's `POST /push/refresh?rid=&skins=1`, which opens the client's "new skin" window; `{ok, skin_id, end_time, refreshed}` (`push_error` when the gateway cannot be reached, the skin stands); 422 for an unknown skin or bad days, 404 for an unknown role; audited as `role.skin` |
| `POST /api/roles/{id}/looks` `{kind, base_id, days?}` | write | gives the role a cosmetic: `kind` is `face` (a head id), `backdrop` (an item id of the client), `avatar` (avatar frame), `bubble` or `title`; `avatar` and `title` must be in the client's tables (`GET /api/meta` `looks`: `kind`, `id`, `name`), `backdrop` an item the client knows; for good (`days` 0 or left out) or for 1 to 3650 days; granting one it owns sets the new end time; nothing is pushed, the client reads the lists when the panel opens; `{ok, kind, base_id, end_time}`; 422 for a bad kind, unknown id or bad days, 404 for an unknown role; audited as `role.look` |
| `POST /api/roles/{id}/kick` | write | disconnects the role without banning it; `{ok, kicked}` (`kicked` false = it was offline); 502 when the gateway cannot be reached |
| `POST /api/roles/{id}/ban`, `.../unban` | write | ban kicks the role if online |
| `GET /api/redeem-codes`, `POST /api/redeem-codes` `{code, note?, grants: [{kind, asset?, item_id?, num}], max_uses?, days?}`, `DELETE /api/redeem-codes/{code}` | read / write | codes players type under 设置 → 兑换码 (10945). The code is 4 to 32 letters or digits and is compared without regard to case (listed upper-cased); `grants` as in `/api/roles/{id}/grant` (1 to 20 entries, same validation); every role may use a code once, `max_uses` limits all roles together (0 = no limit), `days` how long it works (1 to 3650; 0 or left out = for good). The list shows `used` and `expires_at`. Create replies `{ok, code, expires_at}`, 409 when the code exists, 422 for a bad entry; delete keeps what players got. Audited as `redeem.create` / `redeem.delete`. A role that types ten wrong codes in a day is refused until the next day |
| `GET /api/reports?limit=` | read | what players reported (12770), newest first: `{id, rid, target_rid, target_srv_id, type, msg, created_at}`; the chat history the client attaches is not stored |
| `GET /api/guilds?srv_id=&q=&limit=&offset=` | read | the guilds (all servers, or one) with `gid`, `srv_id`, `name`, `lev`, `members`, `leader`, `leader_rid`, `created_at`, biggest first; `q` matches the name; `{items, total}` |
| `POST /api/guilds/{id}/disband` | write | dissolves a guild (members, applications and log go with it; the leader gets the leader's cool-down). Online members see it when they log in again; 404 for an unknown guild; audited as `guild.disband` |
| `GET/POST /api/activities`, `PUT/DELETE /api/activities/{id}` | read / write | the schedule of the holiday activities (the client's action tabs, see "Activities" below); `GET` takes `srv_id` (that server's rows and the ones for every server), `limit`, `offset` and replies `{items, total}`; `POST`/`PUT` take `{srv_id, camp_id, bid, status, start_at, end_at, spec}` (`srv_id` empty = every server, `end_at` 0 = never ends, `status` 1 on / 0 off) and reply the row (201 / 200), 422 for a bad row or spec, 404 for an unknown id; audited as `activity.create` / `.update` / `.delete`. Nothing is pushed: the gateway notices the change within 15 seconds and tells the online roles to ask for the list again |
| `GET/POST /api/notices`, `PUT/DELETE /api/notices/{id}` | read / write | shown in the game's mailbox ("公告" tab); create and update reply with the notice plus `pushed` (online roles told) or `push_error`; delete replies `{ok, pushed}` |
| `GET /api/backups`, `POST /api/backups` | manage | list the database backups (newest first, with `keep`), or make one now and prune the old ones; `{ok, name, size, pruned}`; the directory is never taken from the request; 409 when backups are not configured; audited as `backup.create` |
| `POST /api/broadcast` `{msg}` | write | scrolls `msg` (1 to 100 characters) across the top of every online player's screen (12761 on the NoticeTop channel; not kept in the chat history); `{ok, pushed}`; 422 for a bad message, 502 when the gateway cannot be reached; audited as `broadcast` |
| `GET /api/mails`, `POST /api/mails` | read / write | see below; the list also has `item_names` (base id -> name) for the items its mails carry |
| `GET /api/items?q=&limit=` | read | search the client's item table by name (contains) or by base id (prefix), by id order, at most `limit` (default 30, max 100); each entry has `base_id`, `name`, `type`, `overlap`, `sellable`, `in_backpack`, `currency`; an empty `q` gives `[]` |
| `GET /api/audit?limit=&offset=` | manage | newest first |

Paging: `limit` defaults to 50 and is capped at 200.

### Activities

A row is one activity open from `start_at` to `end_at` on a server. `camp_id` is the campaign number the client keeps (a
positive number of your choice for activities made here) and `bid` the tab id; two open rows with one `bid` show only the
older. `spec` is an object (unknown keys are refused):

| key | meaning |
|---|---|
| `title` (required, at most 40 characters), `title2`, `ico`, `type_ico`, `top_banner`, `rule_str`, `time_str`, `bottom_alert`, `aim_title`, `reward_title`, `channel_ban` | the texts and pictures the tab shows (client resource names) |
| `panel_type` (required, 1 to 145) | which panel draws the tab, the index in the client's `ActionPanelTypeView` (2 gift list, 3 accumulated charge, 4 accumulated cost, 16 common aim list, ...); the aims below fit the aim-list panels |
| `sort_val` | the order of the tabs |
| `cli_type`, `cli_type_name` | the main-screen icon the tab is grouped under (`MainuiConst.icon` id) and its name; 0 = none |
| `daily` | counts and rewards start again every day |
| `aims` (up to 60) | `{id, str, event, target, rewards}`: `id` positive and unique in the activity; `event` the event label it counts (empty: can be taken at once), `target` the count to reach, `rewards` 1 to 10 `[item_id, amount]` pairs of known items or currencies |

The event labels are the ones the daily quests count (`questEvent`): `evt_cli_loaded` (the daily login), `evt_recruit`
(one hero summon draw), `evt_friend_present` (one friend gift sent) and the fast-combat label of the campaign. A system
that wants to feed more calls `questEvent` with a new label and the schedule can use it at once.

### Notices

A role sees a notice that is shown (`status` 1), has started (`start_at`), has not ended
(`end_at`, 0 = never) and is for every server or the role's own. Saving a notice pushes
it to the online roles that may see it, and tells the others to drop it (so hiding one
takes it off an open list); deleting pushes a drop to everyone online. A notice that
expires while a player is online stays visible until the next login. Read state is kept
per role.

### Sending mail

```json
{ "scope": "direct", "role_ids": [1, 2],
  "subject": "...", "content": "...", "from_name": "GM", "type": 1,
  "expire_days": 7,
  "attachments": [ {"kind": "asset", "asset": "gold", "num": 100},
                   {"kind": "item",  "item_id": 40112, "num": 2} ] }
```

`scope` is `direct` (needs `role_ids`, 1 to 500, all must exist) or `broadcast`
(no `role_ids`; optional `srv_id`, empty = every server). Give `expire_days`
(1 to 365) or `expire_at` (unix, in the future), not both. Assets are named as in
`/api/meta` (`gold` = 3, `coin` = 1, ...); `kind: "item"` takes any positive base
id because no item table has been extracted yet. At most 8 attachments, amounts 1
to 1,000,000,000. The reply is `{mail, pushed, push_error?}`: the mail is stored
first, then the gateway pushes it to online roles; if the gateway is down the mail
is still stored and `push_error` says why.

## Payments (top-up by QR code)

There is no payment provider. The player taps a top-up in the game; the phone's browser opens the payment page of the login
service (`/pay/order/<no>`), which shows the amount, the order number and the QR codes set here. The player pays; the
operator checks the money arrived and confirms the order, which delivers the product (diamonds, VIP experience) and tells the
role if it is online. Everything that moves money needs the `manage` role.

- `GET /api/pay/config` (read) -> `{qr_wechat, qr_alipay, notice}`; the QR codes are data URLs. `PUT /api/pay/config` (manage)
  `{key, value}` with key `qr_wechat`, `qr_alipay` (a png, jpeg or webp data URL of up to 600 KiB) or `notice` (300 characters);
  an empty value removes it.
- `GET /api/pay/products` (read) the products that can be paid for now. `GET /api/pay/orders?status=0|1|2&limit=` (read): 0
  waiting, 1 paid, 2 cancelled; each row has `product` and `role`.
- `POST /api/pay/orders/{no}/confirm` (manage) `{note}` -> `{ok, order, refreshed?, push_error?}`; 409 when the order is not
  waiting any more (a second confirm cannot deliver twice). `POST /api/pay/orders/{no}/cancel` (manage) `{note}`.
- `POST /api/pay/orders` (manage) `{rid, charge_id, confirm?, note?}` makes an order by hand (the player paid outside the
  game); with `confirm` it is delivered at once. 422 for a product nothing delivers yet, 409 when the role already has five
  orders waiting.

## Status codes

400 malformed JSON, unknown field, trailing data, bad id · 401 no or bad token, wrong
login · 403 role too low, wrong current password · 404 not found · 409 conflict
(duplicate, self-lockout, server has roles) · 413 body over 1 MB · 422 validation ·
429 login locked.

### Module and charge API boundaries (phase 2)

All module/charge GETs are read-only. Raw `data` preserves the saved JSON, including
unknown fields and large integer values. `view.state` is a fresh in-memory projection:
first-charge/month-card activation and daily/cycle roll use existing game logic but
never save, initialize rows, grant rewards or trigger new limited-gift offers.
A registered but absent row returns `exists:false`, `data:null`; charge views can
still show computed defaults. Unknown keys/suffixes are 404. Global metadata does
not expose global data: phase 3a now provides gateway-owned server/global reads and registered rankings; frontend remains later work.
All `editable` flags stay false, and no PUT editor exists.

`GET /api/roles/{id}/charge` returns `rid`, read-only `charge` totals, nine concrete
`modules` entries (`key/module/exists/data/view`) and paged `orders`. The order query
filters by RID **before** limit/offset, includes every payment status and reuses the
pay store scanner; it does not filter a global recent-order list in memory. Page
parameters use the existing admin paging rules (default limit 50, maximum 200).

Each charge view has `protocols`, `state` and optional `details`:

| Module | Protocol-compatible fields | Reset item (manage only) |
| --- | --- | --- |
| `firstcharge_first`, `firstcharge_first_new`, `firstcharge_first_lwt`, `firstcharge_first_3` | `protocols` keyed by 21000/21012/21030/21032, with original gift/choice field names; effective Start/Taken/Last/Chosen in state | `all`: delete only this concrete window's saved row |
| `daycharge` | `protocols["21010"]` status/num after in-memory daily roll | `today`: clear today's Num/Taken and align Day |
| `monthcard` | `protocols["16705"]`; details include per-card remaining days and actual paid cents | `0` or `1`: clear only that card's Start/Last/Days, Base=current paid cents |
| `monthfund` | `protocols["24700"]` plus two `protocols["24701"]` messages | `0` or `1`: clear only that fund's Start/Last |
| `limitgift` | `protocols["21210"]` for saved offers; details enumerate configuration, opened/expired/bought/passed and supported state | exact decimal gift ID from the 23-row configuration |
| `cyclegift` | two `protocols["21007"]` messages for weekly/monthly counts, rolled only in memory | exact decimal charge ID from the 12-row cycle configuration |

The month-card/fund reset indices are zero-based; they are not the game's card_type
or fund ID. Limited-gift kind 3 remains unsupported by gameplay: clearing its old
record never offers or sells it. Configured reset IDs are enumerated into the
module's static whitelist; arbitrary IDs, aliases/leading zeroes and `all` on other
modules return 422. Other registered modules get no reset capability.

The month-card Base policy is an explicit admin rule not confirmed from the original
server (recorded in OPEN_QUESTIONS). It prevents historical money from reactivating
a reset card, preserves the sibling card and never changes paid totals/orders/assets.
Reset itself grants nothing. Subsequent payment/qualification/claim follows existing
game rules; an authorized first-charge reset can deliberately reopen that window.

Reset remains superadmin-only, requiring a 1–200-character nonblank reason with no
control characters. The original order is retained: gateway online check first
(online 409, unavailable/missing/null/non-boolean state 502), whitelist, before,
write with audit. A saved row is required (404 when absent). Typed reset rejects
unknown stored fields instead of silently dropping them. State compare-before and
`role.module.reset` audit commit in one transaction; audit failure rolls back data.
Audit includes key/item/reason, before/after hash/size and changed top-level fields.

Payment confirms and resets share a bounded per-role lock on the same Store. Confirm
re-reads order status inside the lock and rejects completed/cancelled orders **before**
writable hooks, while retaining the SQL atomic status check. Both sequential replay
and simultaneous waiting confirms were reproduced as defects on baseline e222311
and covered by regressions. This protects the current owning admin process; it is
not a distributed lock and multi-admin/cross-Store concurrent deployment is not
claimed verified. No reward amounts, limits or eligibility formulas were changed.

## Security notes

- Tokens are HS256 JWTs, 12 h by default (`admin.ttl_hours`); only HS256 is accepted
  and `exp` is required.
- Login failures return the same message for an unknown user, a wrong password and a
  disabled admin, and an unknown user costs the same bcrypt time.
- Request bodies are limited to 1 MB and decoded strictly.
- The API trusts the connection's address for the lock-out, not `X-Forwarded-For`.
  Behind a reverse proxy every client would share the proxy's address: put the lock
  on the proxy too, or extend `clientIP` deliberately.
- There is no TLS and no CORS here. Put it behind a TLS proxy; serve the web UI from
  the same origin.
- The gateway control endpoint (`gateway.admin_addr`) has no authentication and must
  stay on loopback; this API is the only thing that should call it.

### Global reads (phase 3a)

All routes below require `read`. Data is obtained only from the gateway control
endpoint, under each registered live service's lock. GET never invokes a writable
loader or timer: it does not initialize rows, roll seasons, grant rewards or mail.
Unregistered service owners and unreachable/invalid upstream responses produce 502;
there is no direct-database fallback. The gateway only serves its configured server.

| Admin route | Gateway route | Result |
| --- | --- | --- |
| `GET /api/servers/{id}/modules` | `GET /ops/global?srv=` | registered fixed keys (including absent states) and saved, validated concrete guild keys |
| `GET /api/servers/{id}/modules/{key}` | `GET /ops/global?srv=&key=` | key, exists, raw data, view.state and view.season |
| `GET /api/servers/{id}/modules/{key}/rank?limit=` | `GET /ops/rank?srv=&key=&limit=` | registered existing rankings, default 50/max 200; unsupported modules 405 |
| `GET /api/servers/{id}/modules/{key}/season` | `GET /ops/season?srv=&key=` | existing calendars and persisted settlement markers; unsupported season projections have supported=false |
| `GET /api/servers/{id}/overview` | `GET /ops/overview?srv=` | world_lev, effective open_time/open_day and arena season/start/end |

Raw data preserves unknown fields and integer precision. Guild prefixes accept only
canonical positive GIDs; unknown global keys and foreign servers are 404. A list is
a sequence of service snapshots, not a transaction across all services. Rankings
reflect saved standings; no administrative GET advances a game clock/rollover.
Sixteen registrations expose their existing score/tie ordering; the rest have no
Rank callback. No global JSON editor or settlement write is enabled by 3a.

### Due-only settlement (phase 3b)

`POST /api/servers/{id}/seasons/{key}/settle` requires `manage` and a body
`{"reason":"..."}` (1-200 Unicode characters, nonblank, no controls). It forwards
to `POST /ops/season/settle?srv=&key=` with the authenticated actor/IP; end users
cannot supply those internal actor fields. The loopback gateway commits the
`server.season.settle` audit with payout and state in one database transaction.
No duplicate audit is written by the proxy. A failed transaction leaves all three
unchanged; accepted not_due/already requests are audited too.

Only arenateam, elitematch, crossarena, arenamany, daysrank, guildwar, arenachamp and
crosschamp are enabled. Other registered keys return 405. Unknown keys or foreign
servers return 404; gateway failures return 502. Results contain settled, reason
and season: running is `false/not_due`, completed/repeated catch-up is
`false/already`, actual catch-up is `true/settled`. A previous team arena season
may have been caught up while the new season is still running; season.settled
continues to describe the current season.

The native service clock/calendar is authoritative. No future clock, early
championship advancement/cancellation, or new championship/guild-war creation is
allowed. Cross/multi arena payout still waits for native rollover. The owner
mutex serializes admin catch-up with native rollover/ticker. Original rewards,
rank ordering and eligibility remain unchanged. Production-player operations and
cross-process service locking are outside these local tests.
