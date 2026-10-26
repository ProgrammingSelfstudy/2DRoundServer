# 服务端对客户端的覆盖（2026-10-26 全量重新统计，战令、节日精英召唤、推图录像、公会红包之后；之后做的转盘 5 个、烟花 6 个请求没有重算进下表）

口径：客户端 Lua 里所有 `SendProtocal(编号` 发出的请求协议（1102 个，分布在 97 个模块），逐个看服务端是 **真实处理**、**只有空回复**（`empty_gen.go`，格式对但没内容）还是**什么都没有**。服务端主动推送的协议不在内。“真实处理”只表示有实现，不表示已和原服务器核对（核对情况见 `OPEN_QUESTIONS.md`）；其中回“未开放”的桩（`hero_stubs.go`、`role_extra.go`）也算真实处理。

| 请求协议 | 个数 | 占比 | 上一次（2026-10-25 晚，多人竞技场、预知召唤之后） | 2026-10-25 早 | 2026-10-21 |
|---|---|---|---|---|---|
| 真实处理 | 751 | 68.1% | 710（64.4%） | 259（23.5%） | 101（9.2%） |
| 只有空回复 | 34 | 3.1% | 44（4.0%） | 98（8.9%） | 115（10.4%） |
| 什么都没有 | 317 | 28.8% | 348（31.6%） | 745（67.6%） | 886（80.4%） |

97 个模块里：41 个全做了；20 个做了一部分但还有缺；36 个一个真实处理都没有，合计 208 个请求（多数是限时活动、跨服玩法和充值）。

**客户端根本不调用的请求**（只有发送函数、没有人用）不算缺口，不用做：`backpack` 的 10520、10526、10528，`welfare` 的 21009 等。下面“还有缺”的数字里还含着这样的请求，所以真正要做的比表里少。

## 做了一部分的模块（还有缺）

| 模块 | 真实 | 空回复 | 没有 |
|---|---|---|---|
| role | 64 | 0 | 1 |
| arena | 24 | 0 | 1 |
| elitematch | 22 | 0 | 1 |
| friend | 16 | 0 | 1 |
| hallows | 14 | 0 | 1 |
| battle_drama | 11 | 1 | 1 |
| guildsecretarea | 10 | 0 | 1 |
| guildboss | 8 | 0 | 1 |
| backpack | 7 | 0 | 4 |
| startower | 7 | 0 | 1 |
| action | 6 | 8 | 74 |
| market | 6 | 0 | 10 |
| chat | 5 | 0 | 4 |
| invitecode | 5 | 1 | 3 |
| login | 5 | 0 | 1 |
| task | 4 | 1 | 2 |
| rank | 3 | 0 | 2 |
| partnersummon | 2 | 0 | 1 |
| pokedex | 2 | 0 | 5 |
| welfare | 2 | 6 | 13 |

## 全做了的模块

hero(78)、adventure(35)、arenateam(32)、elfin(28)、guild(28)、arenamanypeople(24)、planesafk(18)、battle(17)、homeworld(16)、ladder(15)、emoguoshi(14)、endless_trail(14)、heaven(14)、planes(14)、heroexpedit(13)、monopoly(13)、elitesummon(12)、homepet(12)、mall(12)、vip(12)、escort(11)、guildskill(11)、orderaction(8)、vedio(8)、mail(7)、neworderaction(7)、redbag(7)、practisetower(6)、smashegg(6)、voyage(6)、encounter(5)、limitexercise(5)、forgehouse(4)、seerpalace(4)、sevengoal(4)、trainingcamp(4)、newpartnersummon(3)、stone_dungeon(3)、onlinegift(2)、activity(1)、equipmake(1)

## 一个真实处理都没有的模块（括号里是请求数）

arenapeakchampion(18)、crossarena(17)、actionyearmonster(16)、crosschampion(16，其中 3 个空回复)、returnaction(16，其中 1 个空回复)、actiontermbegins(11)、guildwar(11，其中 2 个空回复)、animate_action(9)、newfirstcharge(9，其中 2 个空回复)、festivalaction(7)、timesummon(7)、godbattle(5)、primus(5，其中 2 个空回复)、barrage(4)、element(4，其中 1 个空回复)、guildmarketplace(4)、halidom(4，其中 1 个空回复)、notice(4)、mergeserver(3)、rolescene(3)、daycharge(2，其中 1 个空回复)、exchange(2，其中 1 个空回复)、guide(2)、look(2)、mainui(2，其中 1 个空回复)、onecentgift(2，其中 1 个空回复)、recruithero(2，其中 1 个空回复)、scene(2，其中 1 个空回复)、story(2)、crossshow(1)、limittime(1)、rfmpersonalgift(1)、stronger(1)、verificationcode(1)

## 怎么重新统计

客户端：在 `2DRound/2DRound-codex-restored-source-2026-10-18/outputs/readable-source/lua/0zWHhlnBORyiROK6lvI6VBn6vAbJRQqS/` 下扫 `SendProtocal\(\s*(\d+)`，按第一层目录当模块，生成“模块<TAB>协议号”列表。服务端：给一个临时的 `internal/game` 测试，用 `newEnv(t, 0)` 的路由器（`e.gw.Router.Has(cmd)`）判断真实处理，`emptyReplies` 判断空回复；`newEnv` 不调 `RegisterEmpty`，所以不会把空回复算成真实处理。统计完删掉这个测试。
