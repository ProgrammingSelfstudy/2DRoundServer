# 服务端对客户端的覆盖（2026-10-25 重新统计，组队竞技场之后；好友黑名单、聊天 12764/12768 后手工改了总数：632 / 50 / 420）

2026-10-25 晚又做了家园 homeworld（26001~26021 全部请求，约 16 个），下面的数字和表没有重算，实际真实处理数比表里多这些、“什么都没有”和“空回复”少这些。

2026-10-25 又做了宠物旅行 homepet（26100~26113 全部 14 个请求），同样没有重算。

2026-10-25 又做了竞技场冠军赛（arena 模块的 20251~20256、20258、20260~20263、20280、20281 共 13 个请求；20250 只推送；20204 没做），同样没有重算。

2026-10-25 又做了试炼之境 limitexercise（25410~25414 全部 5 个请求），同样没有重算。

2026-10-25 又做了多人竞技场 arenamanypeople（29000~29035 共 24 个请求，29020 是推送），同样没有重算。

2026-10-25 又做了预知召唤 presage（elitesummon 模块的 16690~16694 共 5 个请求；23220~23222 还没做），同样没有重算。

口径：客户端 Lua 里所有 `SendProtocal(编号` 发出的请求协议（1102 个，分布在 97 个模块），逐个看服务端是 **真实处理**、**只有空回复**（`empty_gen.go`，格式对但没内容）还是**什么都没有**。服务端主动推送的协议不在内。“真实处理”只表示有实现，不表示已和原服务器核对（核对情况见 `OPEN_QUESTIONS.md`）。

| 请求协议 | 个数 | 占比 | 2026-10-25 早 | 2026-10-21 |
|---|---|---|---|---|
| 真实处理 | 632 | 57.3% | 259（23.5%） | 101（9.2%） |
| 只有空回复 | 50 | 4.5% | 98（8.9%） | 115（10.4%） |
| 什么都没有 | 420 | 38.1% | 745（67.6%） | 886（80.4%） |

97 个模块里：53 个至少有一个真实处理；32 个全做了（hero、adventure、arenateam、elfin、guild、planesafk、battle、ladder、emoguoshi、endless_trail、heaven、planes、heroexpedit、monopoly、mall、vip、escort、guildskill、vedio、mail、practisetower、smashegg、voyage、encounter、forgehouse、seerpalace、trainingcamp、newpartnersummon、stone_dungeon、onlinegift、activity、equipmake）；44 个一个都没做，合计 372 个请求（多数是各种跨服、家园、限时活动）。“全做了”包括回“未开放”的桩，不都是真功能（hero、mall、vip 里有一部分）。

## 有真实处理的模块

按真实处理数从多到少。有协议号同时出现在两个模块里，下表各模块请求数加起来比总数多几个。

| 模块 | 请求数 | 真实 | 空回复 | 没有 |
|---|---|---|---|---|
| hero | 78 | 78 | 0 | 0 |
| role | 65 | 64 | 0 | 1 |
| adventure | 35 | 35 | 0 | 0 |
| arenateam | 32 | 32 | 0 | 0 |
| elfin | 28 | 28 | 0 | 0 |
| guild | 28 | 28 | 0 | 0 |
| planesafk | 18 | 18 | 0 | 0 |
| battle | 17 | 17 | 0 | 0 |
| elitematch | 23 | 15 | 1 | 7 |
| ladder | 15 | 15 | 0 | 0 |
| emoguoshi | 14 | 14 | 0 | 0 |
| endless_trail | 14 | 14 | 0 | 0 |
| hallows | 15 | 14 | 0 | 1 |
| heaven | 14 | 14 | 0 | 0 |
| planes | 14 | 14 | 0 | 0 |
| heroexpedit | 13 | 13 | 0 | 0 |
| monopoly | 13 | 13 | 0 | 0 |
| friend | 17 | 16 | 0 | 1 |
| mall | 12 | 12 | 0 | 0 |
| vip | 12 | 12 | 0 | 0 |
| arena | 25 | 11 | 1 | 13 |
| escort | 11 | 11 | 0 | 0 |
| guildskill | 11 | 11 | 0 | 0 |
| guildsecretarea | 11 | 10 | 0 | 1 |
| battle_drama | 13 | 8 | 1 | 4 |
| guildboss | 9 | 8 | 0 | 1 |
| vedio | 8 | 8 | 0 | 0 |
| backpack | 11 | 7 | 0 | 4 |
| mail | 7 | 7 | 0 | 0 |
| startower | 8 | 7 | 0 | 1 |
| market | 16 | 6 | 0 | 10 |
| practisetower | 6 | 6 | 0 | 0 |
| smashegg | 6 | 6 | 0 | 0 |
| voyage | 6 | 6 | 0 | 0 |
| encounter | 5 | 5 | 0 | 0 |
| invitecode | 9 | 5 | 1 | 3 |
| login | 6 | 5 | 0 | 1 |
| elitesummon | 12 | 4 | 0 | 8 |
| forgehouse | 4 | 4 | 0 | 0 |
| seerpalace | 4 | 4 | 0 | 0 |
| task | 7 | 4 | 1 | 2 |
| trainingcamp | 4 | 4 | 0 | 0 |
| chat | 9 | 5 | 0 | 4 |
| newpartnersummon | 3 | 3 | 0 | 0 |
| rank | 5 | 3 | 0 | 2 |
| stone_dungeon | 3 | 3 | 0 | 0 |
| onlinegift | 2 | 2 | 0 | 0 |
| partnersummon | 3 | 2 | 0 | 1 |
| pokedex | 7 | 2 | 0 | 5 |
| welfare | 21 | 2 | 6 | 13 |
| activity | 1 | 1 | 0 | 0 |
| equipmake | 1 | 1 | 0 | 0 |
| redbag | 7 | 1 | 0 | 6 |

## 没有任何处理的最大模块

action(77), arenamanypeople(24), arenapeakchampion(18), crossarena(17), actionyearmonster(16), returnaction(15), homeworld(15), crosschampion(13), welfare(13), arena(13), actiontermbegins(11), market(10), animate_action(9), guildwar(9), homepet(8), elitesummon(8), festivalaction(7), newfirstcharge(7), elitematch(7), timesummon(7), dial_action(6), redbag(6), petard_action(6), chat(6), orderaction(6), pokedex(5), godbattle(5), friend(5), neworderaction(5), limitexercise(5)

（括号里是“什么都没有”的请求数，arena、market、elitematch、chat 等是已有一部分的模块里剩下的。）

## 怎么重新统计

客户端：在 `2DRound/2DRound-codex-restored-source-2026-10-18/outputs/readable-source/lua/0zWHhlnBORyiROK6lvI6VBn6vAbJRQqS/` 下扫 `SendProtocal\(\s*(\d+)`，按第一层目录当模块，生成“模块<TAB>协议号”列表。服务端：给一个临时的 `internal/game` 测试，用 `newEnv(t, 0)` 的路由器（`e.gw.Router.Has(cmd)`）判断真实处理，`emptyReplies` 判断空回复；`newEnv` 不调 `RegisterEmpty`，所以不会把空回复算成真实处理。统计完删掉这个测试。
