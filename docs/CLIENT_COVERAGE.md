# 服务端对客户端的覆盖（2026-10-25 全量重新统计，多人竞技场、预知召唤之后）

口径：客户端 Lua 里所有 `SendProtocal(编号` 发出的请求协议（1102 个，分布在 97 个模块），逐个看服务端是 **真实处理**、**只有空回复**（`empty_gen.go`，格式对但没内容）还是**什么都没有**。服务端主动推送的协议不在内。“真实处理”只表示有实现，不表示已和原服务器核对（核对情况见 `OPEN_QUESTIONS.md`）。

| 请求协议 | 个数 | 占比 | 上一次（组队竞技场后） | 2026-10-25 早 | 2026-10-21 |
|---|---|---|---|---|---|
| 真实处理 | 710 | 64.4% | 632 | 259（23.5%） | 101（9.2%） |
| 只有空回复 | 44 | 4.0% | 50 | 98（8.9%） | 115（10.4%） |
| 什么都没有 | 348 | 31.6% | 420 | 745（67.6%） | 886（80.4%） |

97 个模块里：36 个全做了（含回“未开放”的桩，不都是真功能）；21 个做了一部分但还有缺；40 个一个都没做，合计 315 个请求（多数是限时活动、跨服玩法和充值）。

## 做了一部分的模块（还有缺）

| 模块 | 真实 | 空回复 | 没有 |
|---|---|---|---|
| role | 64 | 0 | 1 |
| arena | 24 | 0 | 1 |
| elitematch | 18 | 1 | 4 |
| friend | 16 | 0 | 1 |
| hallows | 14 | 0 | 1 |
| guildsecretarea | 10 | 0 | 1 |
| elitesummon | 9 | 0 | 3 |
| battle_drama | 8 | 1 | 4 |
| guildboss | 8 | 0 | 1 |
| backpack | 7 | 0 | 4 |
| startower | 7 | 0 | 1 |
| market | 6 | 0 | 10 |
| chat | 5 | 0 | 4 |
| invitecode | 5 | 1 | 3 |
| login | 5 | 0 | 1 |
| task | 4 | 1 | 2 |
| rank | 3 | 0 | 2 |
| welfare | 2 | 6 | 13 |
| pokedex | 2 | 0 | 5 |
| partnersummon | 2 | 0 | 1 |
| redbag | 1 | 0 | 6 |

## 全做了的模块

hero(78)、adventure(35)、arenateam(32)、elfin(28)、guild(28)、arenamanypeople(24)、planesafk(18)、battle(17)、homeworld(16)、ladder(15)、emoguoshi(14)、heaven(14)、endless_trail(14)、planes(14)、heroexpedit(13)、monopoly(13)、homepet(12)、vip(12)、mall(12)、guildskill(11)、escort(11)、vedio(8)、mail(7)、voyage(6)、practisetower(6)、smashegg(6)、limitexercise(5)、encounter(5)、forgehouse(4)、seerpalace(4)、trainingcamp(4)、stone_dungeon(3)、newpartnersummon(3)、onlinegift(2)、activity(1)、equipmake(1)

## 一个都没做的模块（括号里是请求数）

action(88)、arenapeakchampion(18)、crossarena(17)、actionyearmonster(16)、returnaction(16)、crosschampion(16)、actiontermbegins(11)、guildwar(11)、animate_action(9)、newfirstcharge(9)、orderaction(8)、timesummon(7)、festivalaction(7)、neworderaction(7)、dial_action(6)、petard_action(6)、godbattle(5)、primus(5)、guildmarketplace(4)、notice(4)、barrage(4)、halidom(4)、element(4)、sevengoal(4)、rolescene(3)、mergeserver(3)、look(2)、story(2)、guide(2)、onecentgift(2)、mainui(2)、exchange(2)、scene(2)、daycharge(2)、recruithero(2)、rfmpersonalgift(1)、verificationcode(1)、crossshow(1)、stronger(1)、limittime(1)

## 怎么重新统计

客户端：在 `2DRound/2DRound-codex-restored-source-2026-10-18/outputs/readable-source/lua/0zWHhlnBORyiROK6lvI6VBn6vAbJRQqS/` 下扫 `SendProtocal\(\s*(\d+)`，按第一层目录当模块，生成“模块<TAB>协议号”列表。服务端：给一个临时的 `internal/game` 测试，用 `newEnv(t, 0)` 的路由器（`e.gw.Router.Has(cmd)`）判断真实处理，`emptyReplies` 判断空回复；`newEnv` 不调 `RegisterEmpty`，所以不会把空回复算成真实处理。统计完删掉这个测试。
