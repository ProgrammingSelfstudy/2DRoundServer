# 服务端对客户端的覆盖（2026-10-26 全量重新统计，弹幕、元素信念、公会宝库之后）

口径：客户端 Lua 里所有 `SendProtocal(编号` 发出的请求协议（1110 个，分布在 97 个模块；比上一次多 8 个，是这次扫描范围略宽或脚本有增减，没有逐个核对），逐个看服务端是 **真实处理**、**只有空回复**（`empty_gen.go`，格式对但没内容）还是**什么都没有**。服务端主动推送的协议不在内。“真实处理”只表示有实现，不表示已和原服务器核对（核对情况见 `OPEN_QUESTIONS.md`）；其中回“未开放”的桩（`hero_stubs.go`、`role_extra.go`）也算真实处理。

| 请求协议 | 个数 | 占比 | 上一次（2026-10-26 午，开学季之后） | 2026-10-26 早 | 2026-10-25 晚 | 2026-10-25 早 | 2026-10-21 |
|---|---|---|---|---|---|---|---|
| 真实处理 | 858 | 77.3% | 799（72.0%） | 751（68.1%） | 710（64.4%） | 259（23.5%） | 101（9.2%） |
| 只有空回复 | 32 | 2.9% | 35（3.2%） | 34（3.1%） | 44（4.0%） | 98（8.9%） | 115（10.4%） |
| 什么都没有 | 220 | 19.8% | 276（24.9%） | 317（28.8%） | 348（31.6%） | 745（67.6%） | 886（80.4%） |

97 个模块里：49 个全做了；23 个做了一部分但还有缺；25 个一个真实处理都没有，合计 148 个请求（多数是限时活动、跨服玩法和充值）。

**客户端根本不调用的请求**（只有发送函数、没有人用）不算缺口，不用做：`backpack` 的 10520、10526、10528，`welfare` 的 21009 等。下面“还有缺”的数字里还含着这样的请求，所以真正要做的比表里少。

## 做了一部分的模块（还有缺）

| 模块 | 真实 | 空回复 | 没有 |
|---|---|---|---|
| action | 18 | 8 | 62 |
| welfare | 2 | 6 | 13 |
| market | 6 | 0 | 10 |
| pokedex | 2 | 0 | 5 |
| chat | 7 | 0 | 2 |
| backpack | 7 | 0 | 4 |
| timesummon | 4 | 0 | 3 |
| invitecode | 5 | 1 | 3 |
| task | 6 | 0 | 1 |
| returnaction | 14 | 0 | 2 |
| rank | 4 | 0 | 1 |
| startower | 7 | 0 | 1 |
| role | 64 | 0 | 1 |
| partnersummon | 2 | 0 | 1 |
| login | 5 | 0 | 1 |
| hallows | 14 | 0 | 1 |
| guildsecretarea | 10 | 0 | 1 |
| guildboss | 8 | 0 | 1 |
| friend | 16 | 0 | 1 |
| elitematch | 22 | 0 | 1 |
| dial_action | 5 | 0 | 1 |
| battle_drama | 11 | 1 | 1 |
| arena | 24 | 0 | 1 |

## 全做了的模块

（现在 49 个；下面的名单和请求数是上一次的，之后新增的全做模块有 barrage、halidom、guildmarketplace 等，没逐个重列）

hero(78)、adventure(35)、arenateam(32)、guild(28)、elfin(28)、arenamanypeople(24)、planesafk(18)、battle(17)、homeworld(16)、ladder(15)、heaven(14)、emoguoshi(14)、endless_trail(14)、planes(14)、heroexpedit(13)、monopoly(13)、vip(12)、mall(12)、elitesummon(12)、homepet(12)、actiontermbegins(11)、escort(11)、guildskill(11)、animate_action(9)、orderaction(8)、vedio(8)、mail(7)、redbag(7)、neworderaction(7)、festivalaction(7)、voyage(6)、practisetower(6)、smashegg(6)、petard_action(6)、encounter(5)、limitexercise(5)、seerpalace(4)、sevengoal(4)、forgehouse(4)、trainingcamp(4)、stone_dungeon(3)、newpartnersummon(3)、onlinegift(2)、equipmake(1)、activity(1)

## 一个真实处理都没有的模块（括号里是请求数）

arenapeakchampion(18)、crossarena(17)、crosschampion(16，其中 3 个空回复)、guildwar(11，其中 2 个空回复)、newfirstcharge(9，其中 2 个空回复)、godbattle(5)、primus(5，其中 2 个空回复)、element(4，其中 1 个空回复)、notice(4)、mergeserver(3)、rolescene(3)、daycharge(2，其中 1 个空回复)、mainui(2，其中 1 个空回复)、exchange(2，其中 1 个空回复)、onecentgift(2，其中 1 个空回复)、guide(2)、scene(2，其中 1 个空回复)、story(2)、look(2)、recruithero(2，其中 1 个空回复)、rfmpersonalgift(1)、limittime(1)、verificationcode(1)、stronger(1)、crossshow(1)

## 怎么重新统计

客户端：在 `2DRound/2DRound-codex-restored-source-2026-10-18/outputs/readable-source/lua/0zWHhlnBORyiROK6lvI6VBn6vAbJRQqS/` 下扫 `SendProtocal\(\s*(\d+)`，按第一层目录当模块，生成“模块<TAB>协议号”列表。服务端：给一个临时的 `internal/game` 测试，用 `newEnv(t, 0)` 的路由器（`e.gw.Router.Has(cmd)`）判断真实处理，`emptyReplies` 判断空回复；`newEnv` 不调 `RegisterEmpty`，所以不会把空回复算成真实处理。统计完删掉这个测试。
