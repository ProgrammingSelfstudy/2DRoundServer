# Original server capture, 2026-10-21 (passive, one session, an existing level-124 account)

How: PCAPdroid (TCP exporter -> `adb reverse tcp:5123` -> a local receiver that writes the
stream to a file outside the repository), filtered to the original game package only, decoded
with `cmd/pcapdecode`. Played by the owner on their own account; nothing was sent by us.
`levelup-and-summon.jsonl` keeps only the decoded messages of the commands below, in the
window of the level-ups and the summons plus the login-time 23200. The raw capture stays
outside the repository; the login frames, character name, role id, server id and account are
not in the file (checked).

## What the capture settled
- **11003 raises 5 levels at once** below level 60: hero 13 went 1 -> 6 -> 11 -> 16 with three
  requests of `{partner_id}`. The cost of each request is exactly the sum of our
  `data_partner_lev` rows (levels 6-10: hero exp 432, coin 1477; 11-15: 1335 and 4115).
- **10302 carries `energy_max` 10000** (what we send since the "100/0" fix).
- **23201 `rewards` has one entry per draw**, in step with `partner_bids`: the card item of
  the hero drawn, `(23+camp)*1000 + (star-1)*100 + (bid%100 - 1)` (12 of 12 draws; 25202 =
  "海贼12号" = hero 20303). The summon points are not listed: they arrive in 10302
  (`acc_recruit_hero` +20 per draw in the advanced group). This is why low-star heroes
  showed nothing on our result page.
- **Free single draws exist in groups 100 (basic) and 300 (advanced)**, not in 200: the
  login-time 23200 has `kv key 4 = 1` on the times-1 entry of those two. Each group's
  `draw_list` lists times 10 first, then 1 (group 400: only 1). After a free draw the
  server sends `23202 {group_id, free_times:0, free_cd_end}`, and `free_cd_end` is
  **05:00 China time of the next day** (1792616400 at 13:49 on 2026-10-21).
- Two ten-draws (groups 100 and 200, recruit_type 4) got **no 23201 at all**: a refused
  summon is not answered, like ours.

## Seen, not copied
- The level-up answer is `10302`, **`11002`** (one hero, flat fields; ours is `11001 {partners}`),
  `11003 {result 1, msg ""}`, `10306 {max_power, power}`, `23402 {code 2}` and the
  achievement pushes `16401`, `24121`, `25811` for the power achievements.
- Hero fields `atk2 / hp2 / def2 / speed2` are large in the original (account-wide
  bonuses?), ours are 0. A summon is `11001` x draws, `10302`, `23201`, `23204` (the group
  state with kv_list) or `23202` (after a free draw), then achievement / quest pushes.
- Draw prices: group 300 x10 with `recruit_type 3` paid in gems.
