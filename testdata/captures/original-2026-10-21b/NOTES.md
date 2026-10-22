# Original server capture, batch 2, 2026-10-21 (the vivo driven over adb, owner's authorization)

How: same chain as `original-2026-10-21` (PCAPdroid -> `adb reverse tcp:5123` -> receiver outside
the repository -> `cmd/pcapdecode`). This time the clicks were made over adb by Claude on the
owner's account (VIP 0, level 124), with the owner's explicit permission for this session. The
raw pcap, the login frames, the role data and the connection address are not in the repository;
`vip-swap-shop.jsonl` has only the messages of the commands listed below.

## Captured
- **16710 (VIP gift list)**: at login and on opening the page `{list: []}`. After the buy it is
  `{list: [{lev: 0}]}` (levels already bought).
- **16711 buy** `{lev: 0}` -> `{code: 1, msg: ""}`. The VIP0 gift costs 0 gems (list price 238) and
  gave 200000 gems. The 16710 list is re-sent right after the buy.
- **16700**: the recharge tiers (`id`, `name`, `need_rmb` in cents, `add_gold`, `get_gold`,
  `is_first`, `pic`). Nothing on the recharge page was bought.
- **11003** `{partner_id}` for a level-1 hero: the answer is `11002` (one hero, flat fields,
  `lev` 6, `power`, `atk2/hp2/def2` account bonuses) then `11003 {result 1, msg ""}`. Same 5-level
  rule and costs as batch 1 (exp 105 / coin 410 shown before, 432 / 1477 after).
- **13403 `{type: 4}`** is the random shop of the tavern ("市场"): `{code 1, count 0,
  free_count 5, item_list [...]}` with `discount`, `discount_type`, `item_id`, `item_num`,
  `limit_*`, `pay_type`, `price`. It is not the recycle shop or the skill shop.
- **23214** (hero list of the seer palace's convert page, `partner_ids`, `new_partner_bid 0`),
  **23235 / 23234** `{flag, msg}` at login and on opening the seer palace, **23215
  `{action: 1, partner_id: 5}` -> `{code: 0, msg: ""}`**: sent by the convert button (the client
  sends action 1 only from `_onClickChangeBtn`; picking a hero sends nothing). Convert essence
  was 0, so the original refused with code 0 and an empty `msg` (the client's `message("")` shows
  nothing; the "how to get it" popup that appeared is the client's own).

## Not captured (blocked by the client, nothing was sent)
- **23213 crystal summon**: all three materials were 0, the client refuses before sending.
- **Hero convert, success path**: needs 20 convert essence ("转换精华", 0 owned). The refused try
  is the 23215 above, so only the success path (23214 with a new hero, action 2 save) is missing.
  The altar's swap (needs 10 potions, none) was not tried.
- **星象占卜**: not tried (needs materials).
- **Recycle shop (英雄 page) and skill shop 13403**: entrance not found. Visited: 杂货酒馆 (type 4),
  终结技 page. Not visited: 声望商城 / 特权商店 (seer palace, right side), 通灵兽商店.
- **10344 on level up**: the hero level-up above did not send it (it did not appear in the c2s or
  s2c lists); it may belong to the role level-up, not the hero.
- **Gift boxes (10515)**: the bag's 每日签到礼包 (a fixed-reward gift, 2 owned) opens its tooltip,
  but tapping 使用 twice closed the tooltip and sent nothing (no 10515, count stayed 2). Either the
  tap does not land on the button (the phone is laggy: the first tap on an item often shows nothing)
  or this particular gift is only usable from the sign-in page. Not tried: the other "使用" item
  (22 of them, a scroll), random and choose-one gifts.
- **Materials-short errors**: not done.

## Next time
- Try the scroll item (22) and the seer-palace 声望商城 / 特权商店 and 通灵兽商店 for the recycle and
  skill shop.
- Take a screenshot 1 s after every tap: the screen on this phone is slow to react.
- A free path to gather convert essence: the popup lists 先知召唤, 远航商人, 竞技商城.
- The owner said to leave VIP and recharge alone for now (the free VIP0 gift was bought before
  that message arrived).

## Recharge pages (second session the same evening, only looked, nothing bought)
`recharge-pages.jsonl`: replies of 16700, 16705, 16707, 16710, 16712, 21005, 21006 and the
recharge-related fields of 10301 / 10302 (the rest of those two is left out). The account was
VIP 3 with 1500 of 2000 accumulated; the page says "再充值 500 钻石即可成为 VIP4".
- `10301 {vip_lev 3, vip_exp 1500}`, `16712 {charge_sum 1500, list [{id, status}]}` (status 1 = can
  claim, 0 = not reached; ids 1 and 2 claimable at 1500): **VIP experience and the accumulated
  recharge are the same number, counted in diamonds** (the paid amount, not the bonus).
- `10302 charge_bi 1000500`: meaning unknown (not the diamond sum; maybe a cumulative in another
  unit). Not copied.
- `16705 {card1_*, card2_*}`: the two month cards (card1 / card2: acc, days, end_time, is_reward).
- `16707 {status 2}`, `21005 {count 0, gold 0, next_gold 1}`, `21006 {first_gift []}`.
- The page layout (screenshots, not stored): the recharge tab lists diamond tiers with an
  "首次" badge (first purchase of a tier gives the extra `add_gold` again as "再赠"); the VIP tab
  shows each level's privileges (offline hook hours, hook exp/gold +20%, the gift) and a
  "至尊月卡" daily reward; the 每日礼包 tab has 1/6/12 yuan packs limited to 3 a day, "均可获得对应
  VIP积分" (the daily packs add VIP points); the 特权商城 tab has weekly/monthly privilege packs
  (30 / 68 yuan).

## Seer palace crystal summon 23213 (the owner clicked, we captured; `seer-summon.jsonl`)
- `23213 {group_id: 4000}` (the 光暗 card) -> `{group_id 4000, rewards [{27801, 30}, {24, 1}, {14002, 30}, {39, 50}]}`:
  one pool hero fragment (27801 x30, which is in our camp-4 pool at 5.63%), then the fixed
  bonus (asset 24 x1, seer essence 14002 x30, star-card points 39 x50). Same as our reply.
  One crystal (14001) was spent (13 -> 12).
- Pushes in the same instant: `10302` (assets) **first**, then two `10512` item updates (one for
  14002 / 14001, one for the fragment; fields `base_id, quantity, id, pos, storage`, not a full
  `10500` list), then the reply `23213`, then `16606 {bid, can_get_num}` and `16603`.
- **`16603`** is a list "累计伟大召唤 10/20/30/... 次" milestone rewards (`aim`, `aim_str`, `item_list`,
  `status` 0 / 2); the summon count goes up with each draw. We do not have it (16603 is an
  empty reply). Not copied beyond this file.
- Not done in this session: the hero convert (the owner did not try it), 星象占卜, 声望商城 /
  特权商店.

## Hero convert, whole flow (the owner clicked, we captured; `seer-convert.jsonl`)
A 5-star hero (partner_id 15, level 6, water camp) was converted; essence 420 -> 320, so a 5-star
costs 100 (our table). The sequence:
- Open the page: `23234` -> `23235` + `23234`; `23214` -> `{new_partner_bid 0, partner_id 0, partner_ids [{id 1}]}`
  (earlier in the evening the same request answered 5 ids; what `partner_ids` means is still unknown).
- **`23215 {action 1, partner_id 15}`** -> `10512` (the essence item, quantity 320), `11002` (the hero
  with `is_lock [{is_lock 1, lock_type 2}]`, same bid and level), `28701` (daily task 50025 done),
  `23214 {partner_id 15, new_partner_bid 10509, partner_ids [{id 1}]}`, `23215 {code 1, msg ""}`.
- **`23215 {action 2, partner_id 15}`** (save) -> `11001 {partners [the new hero]}` with a **new
  partner_id 522**, bid 10509, same level 6 and star 5, **`11006 {expend2 [{partner_id 15}]}`** (the old
  hero is gone), `23214 {0, 0, [{id 1}]}`, `23215 {code 1, msg ""}`.
Changed on our side from this: the save makes a new hero id (the old one is deleted; worn equipment
and formation places move to the new hero: our choice, the sample hero wore nothing), the start locks
the hero (11002), the pushes come in the original's order and `msg` is empty.

## Seer palace shop 声望商城 = shop type 31 (the owner clicked; `seer-shop.jsonl`)
- Opening it sends `13401 {type 31}` -> `{code 1, is_half 0, item_list [], msg "", type 31}`: the list is
  empty until something is bought (the goods come from the client's table; ours is the same).
- Buying `13402 {eid 31055, num 1}` (27800 = 布鲁诺碎片 x30 for 5 of asset 24) -> pushes `10302`,
  `10510` (item update, not a full 10500), `12761` (a system line in channel 64: "获得布鲁诺碎片x30"),
  `12741 {msg}` (the same text as a toast), then **`13402 {code 1, eid, ext [], is_half 0, msg "", type 31}`**.
  `ext` is empty for a good without any limit; the msg is empty.
- Our table has the same good (eid 31055: item 27800 x30, price 5 `recruithigh_hero`). Changed: the
  answer's `ext` is empty for a good with no limit and the success msg is empty. Not done: the
  "获得..." lines (12761 / 12741; their colour tags depend on the item quality).

## Tavern shop refresh and buy (the owner clicked; `tavern-refresh-buy.jsonl`)
- `13405 {type 4}` -> a new six-good list (`code 1, count 0, free_count 4, num 6`): a refresh costs one of
  the five free refreshes and deals a different shelf (this one had two 40111 equipment pieces at
  different prices, a 37001 x10, a hero item 10401, 26802 x30, 25804 x30).
- `13407 {buy_type 1, num 1, order 2, type 4}` -> pushes `10302`, `10512`, `12761` + `12741`
  ("消耗<assets=3/>300，获得探宝券x10"), then `13407 {code 1, msg "", num 1, order 2, type 4}`.
- The price paid is the `discount` figure (300 for price 500 at "6折") when `discount_type` > 0; pay_type 1 is
  coins and 3 diamonds (asset 3).
- Implemented (not a copy of the original's random pool, which we do not know): a pool of the 12 goods
  seen in the two lists, a per-role shelf with bought flags, five free refreshes per free-draw day
  (05:00 China time, a guess), the first shelf again on a new day. The "消耗…获得…" lines are not sent.
- **特权商店 (the seer palace's second shop) was not captured**: the capture was restarted between the
  声望商城 and the tavern clicks and no shop traffic is in either file. To redo.

## Hero level-up and break (the owner clicked; `hero-level-break.jsonl`)
Hero 13 (5-star, bid 30506): level 16 -> 21 -> 26 -> 30 (the cap with no break), break 1, 30 -> 35 -> 40,
break 2. Each `11003 {partner_id}` raises **up to 5 levels** (the last step to the cap was 4 levels);
costs read from the asset changes of 10302 (hero_exp / coin): 16->21 3109 / 8770, 21->26 6036 / 15809,
26->30 7881 / 19513, 30->35 15055 / 35450, 35->40 22605 / 50718 - **identical to our `data_partner_lev`
sums**. A break (`11004`) costs coin 10000 (first) and 20000 (second) plus 10001 items (100 for the
second): our `brach` table has the same figures. The cap is 30 with no break, 40 after one break.
Push order for both: `10302`, (`10512` when an item was spent), **`11002`** (the hero), (`16401` for the
level achievement), then the answer **`{result 1, msg ""}`**. Ours was: answer "操作成功", `11001`, assets.
Changed to the original's order with `11002` and an empty msg.

## 特权商店 (seer palace, second shop): no traffic
Opened by the owner right after 23234 (11:31:45); the capture shows no shop command at all, only a
`10397 {status 1}` four seconds later (the same status message the client sends when a window opens
or closes). So this shop is client-side only (probably the same paid-pack page as the VIP page's
特权商城 tab, priced in yuan): nothing for the server to answer. Left as it is.

## Elite summon 精英召唤 (the owner used the 精英召唤卷 10408 scroll; `elite-summon.jsonl`)
A new summon family we have not implemented (the client: `elitesummon/`; protocols 23220-23222,
23230-23233, 16690-16694). Seen:
- `23230` (no args) -> `{camp_id 50051, end_time, free_time, item_id 3686, item_num 0, lucky_bid 29426,
  must_count 90, reward_list [{id 4},{id 3},{id 1},{id 2}], start_time, times 232}`: the pick-a-hero page's
  state (a lucky hero `lucky_bid`, a guarantee counter `must_count`, the total draws `times`).
- `23231 {recruit_type 4, times 1}` (one draw with the scroll) -> `10302`, `10512` (the scroll: 10408,
  quantity 7 after the draw), `11001` (the hero drawn: bid 30301, star 3, new partner id), **`23201`**
  `{flag 3, group_id 50051, must_five_num 0, partner_bids [{init_star 3, partner_bid 30301}],
  partner_chips [], rewards [{26200, 1}], times 1}` (the usual card item of the hero: (23+camp)*1000+
  (star-1)*100+(bid%100-1) = 26200), then `23230` again with `must_count 89`, `times 233`.
- Not captured: 23220-23222, 23232 / 23233 (pick the lucky hero), 16690-16694 (the "预知召唤" pine
  panel), ten draws, the guarantee. The item 10408 count in the owner's bag was 8 (the owner said), 7 after.
