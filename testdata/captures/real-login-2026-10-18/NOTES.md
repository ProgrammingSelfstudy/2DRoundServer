# Real login flow on device, 2026-10-18

Device: Samsung SM-A165F, Android 16. App: the rebuilt client
(`org.local.onepiece.reconstructed`) built from the restored project, with
every hard-coded server address patched to the local machine
(tools/patch_client_urls.py, tools/patch_native_strings.py). The phone's Wi-Fi
and mobile data were off during the run, so nothing could leave the device;
traffic reached the dev machine only through `adb reverse`.
Client code is recovered from a decompiled APK, not original source.

Flow observed (http.log, capture.jsonl):
1. GET `/` (native XMLHttpRequest reachability probe, was www.baidu.com)
2. GET `/update/config/url_config_demo.lua`
3. POST `/index.php/userlogin/userLogins2?...&username=..&password=..`
4. GET `/api/serverlist` (twice) -> server-select screen showed the local server
5. "Start game": TCP connect, handshake, POST `.../yxmmdasba` (maketoken),
   then frames 1197 and 1164.

## Confirmed
- Handshake `game_client------------` is sent first on the real (async) path,
  with no frame header.
- 1197 sign = md5(idx .. time .. cmd .. "__sszg666__") as hex: matched the
  captured value. code = cmd + (idx+5) % 16. idx starts at 1 per connection,
  time = client clock (server-synced after login).
- 1197 immediately precedes the command it signs (same TCP segment); every signed
  pair in the capture verifies. 10300 is sent unsigned.
- 1164 carries 26 key/value args (account, timestamp, enter_srv_id, platform,
  device_id, device_type, gettui_cid, idfa, token, channel, sign, ip, logsign,
  os_ver, carrier_name, net_type, os, emulator, app_name, package_name,
  package_version, now_ver, srv_lock_ver, accflag, icebird_resp, icebird_body).
  Decoded exactly with 2-byte big-endian prefixes (546 of 546 bytes).
- The `token` arg is the `msg` field of the **userLogins2 reply**, not of the
  maketoken reply (the original maketoken reads a global left by the previous
  call). The server therefore returns the real token in userLogins2.
- Platform login replies: msg "206" unknown account, "207" wrong password,
  "208" contact support, anything else = success; register: "205" ok,
  "200" exists, "203" bad name, "204" bad password.

## Confirmed by accident: receive direction on the real login path
The capture server still answered each 1169 heartbeat with the two canned
frames from the earlier probe (10192 success, and 1164 with one fake role
rid=0x0A0B0C0D srv_id="a_1"). The real client accepted them: after the 1164 it
sent `10192 {rid:0x0A0B0C0D, srv_id:"a_1"}` (the first role), and after the 10192
reply it sent `10300` (no 1197 before it, as gamenet.lua says). So 1164 and
10192 replies built with the confirmed codec drive the real login state machine.
The client heartbeats 1169 about every 10 s, each preceded by a 1197.

## Not yet confirmed
- What the client does after a 1164 reply: this run sent no reply.
- 10191 (create role) and 10193 (relogin) as sent by the real client.
- What the server must send after 10300 for the client to enter the main city.
- Whether the client expects the server to answer the 1197 frame (it is not a
  request; the capture server stayed silent and the client kept the connection).
- Real-game values of `logsign` and the empty `sign`/`ip` args.
