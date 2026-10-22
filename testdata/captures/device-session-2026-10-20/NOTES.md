# Device session 2026-10-20 (Samsung SM-A165F, driven over adb)

Client: `org.local.onepiece.reconstructed` (LAN build), server `configs/lan.yaml` via `make dev`
style devall. Account `test01` (auto-registered), role 玩家9 (rid 9). Screenshots in this folder.

## Handling notes (not server bugs)
- Login screen: tap the account field, `adb shell input text`, Back to hide the keyboard, then the
  LEFT 登陆 button (the middle one sends nothing; the 3 `POST /` lines in loginhttp.log are the
  client's own telemetry).
- A brand-new account stays on the start page after "开始游戏": with no role the client does
  nothing. Open 选择服务器 -> pick 1服 -> the "+" row appears -> tap it (10191).
- The mail icon only shows from level 30, so the test resources were written into
  `role_assets` / `role_items` with SQL instead of a mail (dev database only).
- Restarting devall sends the phone back to the main city on its own (10193 relogin), as before.

## Main line (DEVICE_CHECKLIST "先做这条主线")
| step | result |
|------|--------|
| 1 new role, open 英雄 | ok: one 4-star 修罗, power 194, attributes and 3 skills shown (hero-detail.png) |
| 2 campaign 挑战 | ok: the formation panel opens first, the starter hero is on the field (formation-panel.png) |
| 3 开战 | ok: a real fight plays (round 2/15, skip and X1 buttons, fight-round2.png), no loading hang |
| 4 after the fight | ok: stage 伟大航路1 -> 伟大航路2, the income panel (10/25/200/10 per minute) pops up, 远航情报 0 -> 100 (after-fight-route2.png) |
| 5, 6 | not done yet (more stages, level-up window, chest after a few minutes) |

## Found on the device and fixed
- **Hero level-up (11003)**: below level 60 the client's button reads "升5级" and sends one
  11003 with only the partner id; the client's own count (`HeroMainTabTrainPanel`) assumes up to
  5 levels per request. We raised one level. Now one 11003 raises up to 5 levels (1 from level 60 on)
  as far as the role can pay and the cap allows. Verified: 2 -> 7, power 209 -> 295, costs matched the
  panel. The original server's rule is inferred from the button, not captured
  (see `docs/OPEN_QUESTIONS.md`).
- Hero list, hero detail, resource bar, backpack refresh after the change: all matched.

## Summon (checklist item 五)
The 召唤 building is off-screen to the left of the main city (swipe right); the visible
"伟大召唤" bubble is the seer palace and opens from level 20 ("%d级开启伟大召唤处").
The earlier "click 1 召唤, nothing happens" is gone: 23200 and 23201 both arrive and the window reacts.
| action | result |
|--------|--------|
| open the window | ok: 23200 answered, points 0/1000, ticket count, 免费召唤 / 10-ticket price shown (summon-basic.png, summon-advanced.png) |
| 高级召唤 x1 (220 diamonds) | ok: diamonds 2000 -> 1780, hero 20302 (3-star) added, points +20 |
| 高级召唤 x10 (2000 diamonds) | ok: reveal animation for the 5-star 索隆(新世界) with "超稀有 / new!" (summon-ten-5star.png), then the result page with EXP 200 (summon-ten-result.png), "再抽十次" button; hero list 12 after 11 draws |
| 基础召唤 free x1 | ok: free draw used (`summon_free` row for group 100), hero list 13, then "再抽一次" shows the ticket count 20/1 |
Ledger check: diamonds 21780 - 2000 = 19780, points 220, heroes = 1 starter + 11 + 1.

Finding (not fixed, no capture): the client plays the reveal and the hero card only for heroes
with `show_effect = 1` (148 of 174); the 26 with 0 (all 1-3 star heroes, e.g. 20302, and the starters) show
only the `rewards` items (the points) in the result page, even though the hero is added to the list.
What the original server puts into `rewards` for those heroes is unknown (they are not items in the client's item table).
Not tried: group 300 / friend summon, the decompose check boxes, 招募 with tickets, star-up (needs a
camp of materials).

## Hook income, role level-up, quick battle (checklist items 一 and 二)
| action | result |
|--------|--------|
| chest, first tap | **bug found**: the prompt read "当前已有 100/0 远航情报，领取后超出上限部分将损失" |
| after the fix | ok: no prompt; the claim window shows 挂机收益 00:26:59 and 船长经验 260 / 英雄经验 650 / 贝里 5200 / 远航情报 260 = 26 whole minutes at 10 / 25 / 200 / 10 per minute (hook-claim.png); the chest timer resets to 00:00 |
| role level-up window | ok: 船长等级 1 -> 2, 升级奖励 10 diamonds, 功能预告 "日常任务 6级开启" (role-levelup-window.png) |
| 快速战斗 window | ok: "快速作战可获得120分钟挂机收益", 本关怪物掉落 list, "本次免费" (quick-battle-window.png) |
| quick battle #1 (free) | ok: 02:00:00 = 经验 1200 / 英雄经验 3000 / 贝里 24000 / 远航情报 1200, role 2 -> 4 |
| quick battle #2 | ok: the prompt says "本次快速作战花费 钻石 x100", diamonds -100, result as above (quick-battle-result.png); the button now reads "200 快速战斗 今日剩余:1次" |

Fixed: the client takes the cap from `energy_max` of 10302 (role_vo.energy_max, default 0). We never sent it, so the
cap was 0 and every claim asked. The client's own table (`dungeon_const.energy_max`, 情报值最大上限) says 10000;
10302 now carries it (`internal/game/assets.go`). Open: the original server's value is not captured.

Seen but not touched: the window says "特权未激活 / 前往激活" for the quick-battle privilege (3 free a day, 11 more
purchases): a recharge item we do not have, so the link goes nowhere; we give 1 free + paid ones as before.

## Sweep and milestone rewards (checklist item 一)
Campaign pushed to 伟大航路6 (stage 10060) with plain 挑战 clicks; the account is level 5.
| action | result |
|--------|--------|
| 通关奖励 (the gift icon), claim "通关5关" | ok: the row disappears and the next tiers move up (milestones-after-claim.png). Ledger: asset 24 +3000 (声望), item 10090 +1750, item 39198 x2, item 10001 +2,000,000; `drama_milestones` has id 2. The numbers are the ones the client shows on the row |
| claim "通关1关" (id 1, 4,000,000,000 diamonds) | refused as expected: diamonds stay 19850, the row stays, no ledger change |
| 通关10关 / 15关 / 20关 | shown with a 前往 button (6/10 ...), not claimable yet: ok |
| sweep (13005) | **not reachable on the device**: the stage info window (tap the big stage dot on the map, stage-info-window.png) has no sweep button in this client build. In `battle_drama_boos_info_window.lua` the `ack_button` is only fetched, no touch listener is bound, and `checkSwapAlert` / `openDramSwapView` have no caller, so 13005 can never be sent. The server side stays covered by tests only |

## Equipment (checklist item 四, second half)
Hero 索隆(新世界) (5-star, power 383, attack 150, hp 729). Equipment bag before: 4 swords (40101) + 2 coats (40201) (mail + the 5-stage milestone box).
| action | result |
|--------|--------|
| 一键穿戴 | ok: weapon and coat equipped, power 383 -> 459, attack 150 -> 193, hp 729 -> 1051; `hero_equips` rows (slot 1 = 40101, slot 3 = 40201) (equip-one-key-wear.png) |
| 一键卸下 | ok: both slots empty, power 383, attack 150, hp 729, `hero_equips` empty, the items are back in the bag |
| weapon slot -> 装备更换 list | ok: the four swords listed with 等级 1 / 攻击 43 / 评分 0 (equip-swap-list.png) |
| 穿戴 one sword | ok: attack 193 (+43), power 426, the detail card shows 粗制铁剑 评分 86 with 卸下 / 更换 buttons (equip-detail.png) |
| 更换 to another sword | ok: still one sword worn, the other three in the bag (ledger 40101 x3 + 1 worn); all four swords are the same base id, so this cannot show a visible difference |
Not covered: swapping two *different* pieces of one slot (only one kind per slot was available), boots and hat slots (no such equipment given).

## Formation (checklist item 四, first half)
`挑战BOSS` opens the 英雄出战 panel (HeroController:openFormGoFightPanel) every time except while a fight is running ("当前正在战斗中").
My earlier "it went straight into a fight" was my own double tap: the 开战 button sits under the second tap.
| action | result |
|--------|--------|
| open the panel | ok: 3 heroes on the field (power 891, 索隆 still wearing its gear), all 13 heroes listed below with green ticks on the three used (formation-panel-3-heroes.png) |
| tick a 4th hero | nothing happens: this level only has 3 formation slots (3 "锁" markers in the fight view); the client refuses, no request is sent |
| untick one, tick another | ok: the list moves the tick; the panel only sends 11212 on 开战 |
| 开战 | ok: 11212 saves; `formation_slots` slot 3 changed from partner 27 to 26, the fight plays and wins |
| reopen the panel | ok: the same heroes are ticked as saved (formation-reopened.png) |
Not covered: changing the formation type (阵型 button) and moving the position of a hero on the field.

## Backpack, shop, daily quests, achievements, ranking (checklist items 七 to 九)
| action | result |
|--------|--------|
| 道具 tab | ok: the stacks match the ledger (10090 x1750, the milestone box x2, the 200万 tickets ...) |
| open 每日签到礼包 (10515) | ok: the tip lists all 8 entries (代金券 25W, 突破卡 10000, 贝里 5000W, 英雄经验 5000W, 高级寻宝卷 70, 觉醒纹石 10, 强化训练 50000, 竞技挑战卷 10); 使用 pays every entry, the stack disappears and the new stacks (探宝 70, 强化训练 50000, 竞技挑战卷 10) appear, currencies 407万 -> 5407万 (gift-box-opened.png) |
| 商城 (13401 / 13402) | ok: 道具 page lists the goods with 每日限购 0/9999; buying 强化训练 (20000 代金券): 代金券 45万 -> 43万, +50000 强化训练, counter 1/9999, `shop_buys` row (shop-after-buy.png). The 英雄 tab shows the same list as 道具 (not investigated); 积分 / 征战 shows the weekly limits 0/1, 0/5 |
| 每日任务 (10400, 10406) | ok: login 1/1, summon 3/3 (my 3 draws), quick battle 2/5 (my 2 battles) are right; 提交 pays 20 diamonds each, activity points 5 -> 15, the quest leaves the list and the next one appears (daily-quests-claimed.png) |
| 成就 (16400) | **bug found**: the level achievements stayed at 1/10 .. 1/30 although the captain was level 6: the client reads the list once at login and only repaints from 16401 pushes, and nothing pushed one when the level rose. Fixed (`pushLevelFeats` in `sendAssets`, a 16401 right after the 10344); after the fix and a reconnect the bars read 6/10, 6/15 ... (feats-after-fix.png). Claiming an achievement was not tried |
| 排行榜 (12900) | ok after setting the role to level 30 (the client opens it at 30 and says "30级开启" before that): 剧情进度 sorted by stage, 玩家1 (stage 11), 玩家9 (me, stage 10), 玩家4 (stage 4), the podium and "我的排名" agree (ranking-drama.png) |
Also seen: kicking the role over the gateway control endpoint shows "你断线了，原因是：账号已被封禁或被管理员下线" (the text of 10310) and sends the client back to the login page; the 邮箱 shows "暂时没有邮件" when empty.
Not covered: 合成 (compose, needs the 10428/10429 materials), 战力 ranking, buying past a daily limit, the shop's 技能 tab.

## Star-up (checklist item 六) and the shop 英雄 tab
Materials for 修罗 4 -> 5 star (2 more 修罗, 5 water 4-star, 4 water 3-star) were written with `cmd/granthero`
(straight into the database, so the phone had to log in again to see them: the client reads its hero list at login only).
| action | result |
|--------|--------|
| 升星 page | ok: 4 -> 5 stars, 等级上限 80 -> 100, 速度提升10; the three material slots read 修罗 2/2 (filled by itself), 4星英雄 0/5, 3星英雄 0/4; the red dot on 升星 shows because the materials exist (star-up-panel.png) |
| 一键选取 | ok: 5/5 four-star water heroes, 4/4 three-star ones are picked in the window |
| 升星 (11005) | ok: "升星成功", power 295 -> 365, hp 657 -> 857, attack 78 -> 118, speed 94 -> 104, "无技能提升" (star-up-success.png); `partners` went from 24 to 13 rows (11 consumed), hero 16 is now 5-star |

Shop 英雄 tab: it is the recovery shop (MallType 2, currency = hero soul "heart"), not a goods list. Opening it sends 13403 (the reset count), which
had no handler, so the tab kept showing the 道具 list. Fixed: 13403 answers an empty list with `refresh_time` an hour ahead (so the client
does not ask again at once) and 13405 refuses with "该商店暂未开放"; `TestRandomShopsAreEmpty`. The skill shop (type 9) uses the same commands.
Not done on the device: this fix itself (checked by test only), 合成, claiming an achievement.

## Claiming an achievement and composing (the last open items of the checklist)
| action | result |
|--------|--------|
| 成就 at level 30 | ok: 船长等级 10 / 15 / 20 / 25 / 30 all 完成 with 提交, a red dot on the tab (feats-claimable.png) |
| 提交 the 10-level one (16402) | ok: diamonds 21910 -> 22410 (+500), the row leaves, the 35-level one (30/35) moves in with 前往 (feats-after-claim.png) |
| 碎片 tab with 10428 x50 and 10429 x30 | ok: both stacks show a full bar (50/50, 30/30) with a red dot, 获取途径 and 合成 in the tip (compose-tip.png) |
| 合成 (10523) | ok: 10428 x50 gone, 10421 x1 in the ledger, the other stack stays 30/30 (compose-done.png) |
With these, every row of `docs/DEVICE_CHECKLIST.md` has been looked at on the device except: the formation type and field positions, boots and hat, swapping two different equipment pieces, the shop 技能 page, buying past a daily limit, and sweep (13005), which this client build cannot send.
