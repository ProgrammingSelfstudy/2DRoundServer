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
| `panel_type` (required, 1 to 100) | which panel draws the tab, the index in the client's `ActionPanelTypeView` (2 gift list, 3 accumulated charge, 4 accumulated cost, 16 common aim list, ...); the aims below fit the aim-list panels |
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

## Status codes

400 malformed JSON, unknown field, trailing data, bad id · 401 no or bad token, wrong
login · 403 role too low, wrong current password · 404 not found · 409 conflict
(duplicate, self-lockout, server has roles) · 413 body over 1 MB · 422 validation ·
429 login locked.

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
