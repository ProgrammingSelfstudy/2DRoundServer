# Real client against the gateway, 2026-10-19 (device session)

Same rebuilt client as before (`org.local.onepiece.reconstructed`, patched addresses),
Samsung SM-A165F. Login and game traffic went through `adb reverse` (8081, 8800), the
services ran on the LAN configuration (`configs/lan.yaml`). Screens are in `screens/`.
589 requests and 314 replies/pushes in the gateway log: no signature mismatch, no
decode error; 3 commands without a handler (20200, 20250, 20223, all arena, they
only appear from level 30; now in `new-requests.txt` and the empty replies).

Verified on the device (all of this was only unit-tested before):
- new account (auto-registered) -> server list -> 1164 (no roles) -> "+" -> 10191 ->
  10192 -> 10300 -> main city; role name "玩家2".
- mail with currency and items pushed live (10803), "一键领取" (10802) paid the
  currency, the resource bar and the backpack were refreshed (10302, 10500).
- backpack (10500): the items appear in the 道具 tab with the right counts (mail.png).
- selling (10521/10522): the client's own sell dialog computed 5 x 10000 = 50000, the
  same as our value table; after confirming, coins 0 -> 50000, the stack disappeared
  (sell-dialog.png, sold.png). Only items whose tips_btn contains 37 show a sell button.
- daily sign-in (14100/14101): the first cell becomes "已领取", and the daily gift box
  lands in the backpack (checkin.png).
- friends (13300/13311/13320/13303): the friend window opens, the recommendation list
  shows the other role, "加为好友" turns into "等待同意" and the request is stored
  (friend-request.png).
- world chat (12762 -> 12761 + 12762): the message is sent and comes back from the
  server (world-chat.png).

Second part (the same phone, logging in as the other account):
- accepting a friend request (13305 + 13310): role 2's request showed in the applicant
  list (30, "在线"); after "接受" the friend list shows the friend (1/100) and the
  database holds both directions.
- Found: the friend list showed an offline friend as "1小时前" after only minutes. The
  stored logout time was older than the login time: that session had ended when the
  gateway process was killed, so no logout time was written. Fixed (graceful shutdown on
  SIGINT/SIGTERM, and stale sessions get a logout time at start), then checked on the
  device: stopping the gateway with the phone online stored the logout time.

- private chat to an offline friend (12720 -> 12721 echo + 12720 reply): the message is
  stored (chat_offline); when the other account logged in, the gateway pushed 12722
  after the main city was up, the private tab showed the contact and the message, and
  the stored row was gone (private-chat-sent.png, private-chat-offline-delivered.png).
  The client's message list is clipped at the top edge (the bubble is partly hidden):
  a layout matter of the restored client, not of the data.

Not tried on the device:
- invite codes: the entry (系统设置 -> 推荐码 -> welfare tab 8016) exists, but
  holiday_client_data.lua gives that tab `open_lev = {"lev", 999}` and the role level is a
  uint8, so this client build keeps it closed (the tap only shows "条件不满足"). The server
  side is covered by unit tests only.

Online gifts (third part), entry: the reward icon on the campaign (剧情) top bar, which
opens the 礼包预览 window (online-gift.png):
- 10926 progress and the 12 steps with the countdown to the next one work.
- Found: tapping a step sends 10927 with `time` = the step's NUMBER (its index in the
  table, 1..12), not its required seconds. The server had read it as seconds, so the
  first claim was refused ("奖励不存在"). Fixed (the request names the step by number; the
  reply and 10926 keep using the seconds), tests adapted. After the fix: diamonds
  100 -> 5100, the first step shows "已领取" (online-gift-claimed.png).
- Another finding: the phone's USB forwarding (`adb reverse`) was gone after the phone
  slept for a while; the client then loops on downloading url_config until it is set
  again (`adb reverse tcp:8081 tcp:8081`, `tcp:8800 tcp:8800`).

## Findings about the setup
- The first patched client (`local-patched.apk`) talks to loopback only. With the LAN
  address in the server list or in url_config it logged in but never asked for the server
  list, and never connected to the gateway, silently. Cause (read from the native code):
  `SmartSocket::ConnectAsyn` returns -1 for every host not contained in
  `Data::getWhiteList()` (the three host strings of AppDelegate, patched to 127.0.0.1), and
  the native XHR/url_config paths only accept the same hosts. Verified by experiment: a
  SYN to any non-loopback address never left the phone (polled /proc/net/tcp), while the
  127.0.0.1 control showed an ESTABLISHED connection.
- `tools/make_lan_client.py` builds `local-lan.apk` that fixes this (LAN address on the
  white list, Java-side host rewrite, LAN url_config). Checked on the device with no
  `adb reverse` at all: url_config, login, server list, maketoken and the gateway TCP
  connection all came from the phone's Wi-Fi address (192.168.110.227), and the role
  entered the main city.
- Modules unlock by level: the friend, mail and other icons only appear from level 30
  (the test role was set to 30 in the database).
- The Android "built for an older version" dialog shows once at launch.
- Typing into the login fields: tap the field, `adb shell input text`, then Back to
  hide the keyboard before tapping the login button.

## Fourth part: marquee broadcast, connection limits
- Marquee broadcast (12761, channel bit 32 = NoticeTop, no speaker): after
  `POST /push/broadcast?msg=...` on the gateway's control endpoint the text scrolled across
  the top of the main city on the device (broadcast-marquee.png); the endpoint answered
  `{"pushed":1}`. It is not kept in the chat history.
- The broadcast box on the admin Overview page was also clicked in a real browser (on a
  throw-away database with nobody online): the toast said "已发送给 0 个在线角色", the box
  was cleared and the audit log has the `broadcast` entry.
- The new connection limits (20 s without logging in, 50 connections per address) did not
  disturb a normal login from the phone over Wi-Fi.

## Fifth part: reconnect (seen by accident, from the new log files)
The gateway was restarted (twice) while the phone sat in the main city. The client noticed the
lost connection by itself and, without any tap, did: connect -> `1164` (role list) ->
`10193` (relogin, the reconnect command, until now covered by tests only) -> `10300` ->
`10301` + `10304`, all within 0.13 s (data/logs/gateway.log). Afterwards the phone was still
in the main city with its currencies (after-reconnect.png). Findings:
- `10193` is answered like `10192` and works on the real client.
- Restarting the gateway does not log players out of the game screen; they come back on their own.
  Stopping it with SIGTERM also stores the logout times and starts the reconnect cleanly.

## Sixth part: friend gifts (13316 / 13317)
Verified with both accounts on the one phone:
- 玩家2 tapped the gift icon next to the friend (13316 code 0): the icon turned grey
  (`is_present` 1), coin 50000 -> 51000 (sending pays coin), one row in `friend_gifts`
  (friend-gift-sent.png).
- Logged in as 玩家1: the 领取礼物 tab had a red dot "1", the friend with a heart, "礼物数：1"
  (friend-gift-waiting.png). "一键领取" (13317 code 1): the gift list emptied ("暂无好友赠送"),
  the heart counter went 0 -> 10 (friend points), the red dot went away (friend-gift-drawn.png);
  the gateway sent 13317, 10302 and a fresh 13300.
- Day boundaries use China time: at 19:11 on the Mac (Pacific) the game day was already 20261020.
- Only the numbers the client states are the client's (10 sends and 30 draws a day, coin only past
  the draw limit); 10 friend points per gift and 1000 coin are ours.
