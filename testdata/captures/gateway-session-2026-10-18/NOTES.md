# Real client against the real gateway, 2026-10-18

Same setup as real-login-2026-10-18 (rebuilt client, all addresses patched to
the dev machine, Wi-Fi and mobile data off). Logs here come from the Go gateway
and login service; client addresses are replaced by CLIENT, the password is
redacted. Per-frame 1197/1169 lines are removed for readability.

Observed on device:
1. Login panel -> `userLogins2` -> server list from the servers table -> start game.
2. TCP: handshake, `1164` with the real token -> reply with no roles.
3. The client showed the server panel with an empty slot ("+"), i.e. it parsed
   our 1164 reply and drew the role list from it.
4. Tapping "+" sent `10191` immediately with an EMPTY name (12 bytes: sex,
   name "", career, playform "symlf"). The role is therefore created first and
   named later (10343 {name,sex} -> {code,msg,name,sex,is_first_rename}).
   The gateway creates "玩家<rid>" as a placeholder (our design choice).
5. The client then sent `10192` for the new role by itself, and after our reply
   `10300`. Nothing more was sent: the client waits for the role data that the
   original server pushed after 10300 (10301 and others).

Not done: the data push after 10300, the naming step (10343), the main city.
Behaviour of reconnect (10193) is covered by tests only, not seen on device.

## Update: 10300 answered with 10301 + 10302 (second run)
The client waits for 10301 (role base data): role_controller.lua rgp_hander10301
fires ROLE_CREATE_SUCCESS, which closes the login view and starts the main
scene. With the role already created, "start game" now went
1164 (1 role) -> 10192 -> 10300 -> 10301 + 10302 and the login panel closed.
Right after, the client sent 103 distinct commands (109 requests) within about
a second (post-login-requests.txt): the module initialisation requests.
None are answered yet, and the main scene stays black. This is the end of the
login phase; the main city needs game data, not login logic.

10301 content: ids/name/lev/sex/career/face_id/reg_time from the database,
exp_max and exp_total_nextlev from the client's own level table (the client's
exp_total column has two wrong rows, lev 24 and 124; we use the running sum),
is_first_rename=1 (the client's own default), everything else empty/0.

Side findings
- Android's Java stack does not resolve "127.1": the patched reachability probe
  string should be a dotted-quad. It did not block login in this run.
- The probe patch changed the first XHR behaviour: with the original string
  (www.baidu.com) and no network, no request was sent at all.

## Update: main city reached (third run)
Two things were needed beyond login:
1. Answer the ~103 module-initialisation requests (internal/game/empty_gen.go).
   They are answered with all-empty messages of the same id; that was enough
   for the client to carry on, and it asked 8 more in a second wave (111 total).
2. Push `10304 {play_video}` after 10301/10302. It is a pure server push; the
   client's handle10304 creates the CenterCityScene. play_video=0.
Result (main-city.png): the main city scene, top bar with level 1 and zeroed
resources, bottom navigation. 245 requests, 126 replies, 0 unhandled commands,
0 disconnects over 40 s.

What is visibly missing: the city buildings (the client creates them from the
campaign/drama progress it has not received) and a sharp background (the map
stayed blurry; cause not investigated, probably client-side asset loading).
Empty replies are the "no data" state, not game content: every module is empty.

## Update: main-city navigation pass (nav-screens/)
All entries were opened with empty data; screenshots in nav-screens/.
- Hero: list ("一个英雄都没有哦") 0/0, codex (client config, no server data),
  formation page. New commands: 20060 (which fight am I in; reply 0) and 26555
  (sprite teams; reply echoes `type`, empty list).
- Bag: five tabs, empty grid; only 20060.
- Training (历练), Guild (公会), Skill (神技): the client blocks them locally by
  role LEVEL (tab labels "15级解锁" / "18级解锁"), so at level 1 they send nothing.
  The test role was set to level 30 directly in the scratch SQLite database to
  see them. At level 30 the main city also shows many more widgets, and modules
  ask for data at login (23800, 23805, 25100, 28603, 28616). Guild opens with an
  empty list after 13501 is answered (reply echoes page/flag/num/name).
  "神技" opens a row of function icons in the main city, no command.
- Final pass at level 30: 265 requests, 138 replies, 0 unhandled, 0 disconnects.

Findings about the client
- Unlock levels are enforced by the client from its own config; the server only
  has to report the level correctly in 10301 / 10302.
- Replies identify what they answer by repeating the request key (id, bid, type,
  page, ...), so the empty reply echoes shared scalar fields (internal/game/empty.go).

## Update: main-city buildings (main-city-buildings-*.png)
The city had no buildings, and a blurry background, because the client has no
sender for 13000 (campaign state): the server must PUSH it, like 10304.
MainSceneController.createBuildVo returns early while the model has no drama
data; the blurry image was a placeholder shown until then.

Fix: after 10301/10302 the gateway pushes `13000` with the role's campaign
state, then 10304. A new role starts at the first main-line dungeon (10010,
"伟大航路1", chapter 1, mode 1), with max_dun_id = 10010 and status = 1.
- max_dun_id is a key into the client's own dungeon table, so it is certain.
- status = 1 is read from battle_drama_model.lua ("not passed"); the client
  showed no misbehaviour, but the meaning is not confirmed.
- mode_list is sent empty; the campaign map pages will need it.
- Progress is stored per role (migration 002_role_drama.sql); a role without a
  row is at the start. The chain (640 dungeons, 16 chapters) comes from the
  client's dungeon_data.lua (tools/extract_drama_dungeons.py).

Result: buildings appear and unlock by role level and campaign progress, all in
the client's own config. New player (level 1): 恶魔果实 grey; level 30: coloured;
the 神技 tab reads "3关解锁" (unlocks at dungeon 3). The test role's level was set
in the scratch database to compare. Tapping "伟大召唤" opens the summon page
(nav-screens/summon.png) and asks 23234; answered empty for now.
Final check: 242 requests at level 1, no unhandled command, no disconnect.

## Update: every main-city entry opened (nav-screens/city-*.png, downscaled)
Entries tried (16): 跨服战场, 果实共鸣, 竞技场, 锻造屋, 恶魔果实, 海底监狱, 福利, 充值,
海楼石商城, 录像, 好友, 邮件, 奖杯, 任务卷轴, 乔巴, 更多. To unlock them the test
role was set in the scratch database to level 100 with campaign progress at the
last dungeon (160400): buildings are locked by level / dungeon in the client's
config, and the gateway reports whatever is stored. Level 100 also makes modules
ask for data at login (17 more commands).
Final pass: 369 requests, 199 replies, 0 unhandled, 0 disconnects.

Bug found and fixed: the 20060 reply used to say combat_type 0 always. The
client sends 20060 with the fight type of the window it wants to open (arena,
prison, ...) and only opens it when the reply repeats that type with type = 0
(openRelevanceWindowAtOnce). With a fixed 0 those windows never opened. Now the
reply echoes combat_type (internal/game/battle.go).

State of the pages with empty data
- open and empty: friends (0/100), mail ("暂时没有邮件"), trophy ranking, recordings,
  arena (tabs, no opponents), prison, tutorial (乔巴) and daily quests
- 充值 and 海楼石商城 open with an empty content area: their data commands
  (16700/16705/21005 and the shop's) are answered empty
- "更多" only folds the icon row in the bottom right of the city

## Update: mail, end to end on a device (mail-screens/)
Sent with `mailtool` while the player was online, one direct mail with attachments
(100 gold = the diamond icon, 5000 coin) and one broadcast. What happened:
1. Live push: the gateway sent 10803 for both; the mail icon in the city got a red
   badge "2" without any request from the client (city-red-badge.png).
2. Opening the mailbox lists both as 未读 (list.png). Opening one: the client sends
   10806 (detail) and then 10805 (mark read); the detail window shows content,
   expiry ("有效期：6天") and the two rewards (detail.png).
3. 领取 -> 10801; the server answers it and re-sends 10302, and the resource bar
   changed from 0 to the claimed amounts. The window shows 删除 instead of 领取
   and the reward icons are gone (claimed.png). The asset ledger holds 3:100 and 1:5000.
4. 删除已读 -> 10804 with only the read-and-claimed mail; the unread broadcast stayed.
5. After reading and deleting the second mail and logging in again the mailbox was
   empty (empty-after-relogin.png): the broadcast did not come back (soft delete).

Not covered on a device: 一键领取 (10802) and the notice tab (10950/10951/10952,
still answered empty); both are covered by integration tests.

## Update: admin API against a device (admin-screens/)
Three real processes (login service, gateway, admin API) and the phone. With curl:
login as the bootstrap superadmin, change the password (the old token stopped
working at once), then `POST /api/mails` to the online role: the response said
`pushed: 1` and the mail icon in the city got its red badge (mail-pushed-from-api.png).
`POST /api/roles/1/ban`: online went 1 -> 0, the gateway sent 10310 and closed the
connection, and the phone showed "你断线了，原因是：账号已被封禁或被管理员下线"
(banned-kicked.png). Every step is in the audit log, with no password in it.

## Update: notices in the game (notice-screens/)
The mailbox's notice tab is now served from the notices the admin stores.
Observed on a device with the real stack:
1. The tab lists the visible notices (list.png): title, a one-line summary (the first
   line of the content) and a red count of unread ones. 10950 is requested at login.
2. With the mailbox open, a notice created through the admin API appeared at once
   (the gateway pushed 10950 with the entry) and the count went up.
3. Tapping a notice sends 10952; on code 1 the client opens the detail with the full
   content (detail.png).
4. Found on the device: the client does NOT mark a notice read by itself; the entry
   stays un-greyed and the count unchanged until the server sends it again with
   flag 1. The gateway therefore pushes the entry (10950, flag 1) right after the
   10952 reply, and the list then greys it and the count drops (read-greyed.png).
5. Deleting the notice in the admin pushed 10951 and it vanished from the open list
   (after-delete.png).
Not covered live: a notice that expires while a player is online stays on screen until
the next login (the client has no timer for it).
