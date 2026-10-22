# Original server capture, 2026-10-20 (passive, one session)

How: PCAPdroid (TCP exporter -> adb reverse -> a local receiver), filtered to the
original game package only, TLS decryption off, decoded with `cmd/pcapdecode`.
The game stream was 10.215.173.1:39880 -> 211.154.26.201:9090. Played by the
owner on their own account; nothing was sent by us.

`battle-flow.jsonl` holds only the campaign-battle commands (13000, 13001, 13003,
13006, 13008, 13011, 13016, 13017, 11099, 11211-11213, 20000-20005, 20019, 20027,
20060, 20061, 20063). The login frames (1164 etc.), HTTP and the raw
payload bytes are left out on purpose, and the character name, role id and server id
that appear inside 20027 / 13016 are replaced ("玩家", 1, "symlf_1"). Only decoded
messages are kept; the raw capture stays outside the repository.

## Real order of one campaign fight (times relative to 13003)
c2s 13003 {is_auto:0}
s2c 13011, 13006, 13003 {code:1}, 20063 {type_list:[]}, 20000 {combat_map:20001, combat_type:3}, 20027 (978 B, fight start)
c2s 20001 (ready) -> s2c 20001 {code:1}, 20002 (round start)
c2s 20019 -> s2c 20004 (843 B, skill plays) -> c2s 20005 -> s2c 20002, c2s 20019, s2c 20004
c2s 20005 (animation finished) -> s2c 10302, 10512 x2, 13001 x2, 10528, 23604, 13016 (683 B), 13008, 20063
then c2s 20060 {combat_type:3} -> s2c 20060 {combat_type:3, type:2} + 20061 (374 B)
There is no separate 20006: the end of the fight is the 13016 / 13001 / 10302 group.

## What differs from our server (found by comparing)
- 13000 at login: status 2, cool_time 0, auto_num_max 10, and a full `mode_list`
  (chapter_list -> dun_list with status 3 for cleared and 2 for the current one).
  We send status 2 now (fixed 2026-10-19) but an empty mode_list and auto_num_max 0.
- 20060 {combat_type:3}: the real server answers `type: 2` and pushes 20061 right
  after (the "normal / hook battle" data: our heroes, one wave of monsters). We answer
  `type: 0` and never send 20061, so the client never builds the hook battle scene,
  and the "挑战BOSS" button stays hidden (btn_layout_status).
- At login the real client sends 20060 twice (combat_type 0 and 3) and gets 20061
  (206 B) once, after the second.
