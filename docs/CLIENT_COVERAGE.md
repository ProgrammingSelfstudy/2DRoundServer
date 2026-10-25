# 服务端对客户端的覆盖（2026-10-25 快照）

口径：客户端 Lua 里所有 `SendProtocal(编号` 发出的请求协议（1102 个，分布在 97 个模块），逐个看服务端是 **真实处理**、**只有空回复**（`empty_gen.go`，格式对但没内容）还是**什么都没有**。服务端主动推送的协议不在内。“真实处理”只表示有实现，不表示已和原服务器核对（核对情况见 `OPEN_QUESTIONS.md`）。

| 请求协议 | 个数 | 占比 | 2026-10-21 |
|---|---|---|---|
| 真实处理 | 259 | 23.5% | 101（9.2%） |
| 只有空回复 | 98 | 8.9% | 115（10.4%） |
| 什么都没有 | 745 | 67.6% | 886（80.4%） |

97 个模块里：33 个至少有一个真实处理（10-21 是 22 个）；10 个全做了（guild、guildskill、mail、forgehouse、seerpalace、newpartnersummon、stone_dungeon、onlinegift、activity、equipmake）；64 个一个都没做，合计 645 个请求（多数是各种竞技场、跨服、家园、限时活动）。

按模块逐行数时有 8 个协议号同时出现在两个模块里，下表各模块的请求数加起来是 1110，总表按协议号去重是 1102。

## 有真实处理的模块

| 模块 | 请求数 | 真实 | 空回复 | 没有 |
|---|---|---|---|---|
| role | 65 | 48 | 4 | 13 |
| hero | 78 | 39 | 4 | 35 |
| guild | 28 | 28 | 0 | 0 |
| friend | 17 | 12 | 0 | 5 |
| arena | 25 | 11 | 1 | 13 |
| guildskill | 11 | 11 | 0 | 0 |
| guildsecretarea | 11 | 10 | 0 | 1 |
| battle_drama | 13 | 8 | 1 | 4 |
| guildboss | 9 | 8 | 0 | 1 |
| backpack | 11 | 7 | 0 | 4 |
| startower | 8 | 7 | 0 | 1 |
| mail | 7 | 7 | 0 | 0 |
| battle | 17 | 6 | 0 | 11 |
| market | 16 | 6 | 0 | 10 |
| mall | 12 | 5 | 0 | 7 |
| invitecode | 9 | 5 | 1 | 3 |
| login | 6 | 5 | 0 | 1 |
| elitesummon | 12 | 4 | 0 | 8 |
| task | 7 | 4 | 1 | 2 |
| forgehouse | 4 | 4 | 0 | 0 |
| seerpalace | 4 | 4 | 0 | 0 |
| chat | 9 | 3 | 0 | 6 |
| rank | 5 | 3 | 0 | 2 |
| newpartnersummon | 3 | 3 | 0 | 0 |
| stone_dungeon | 3 | 3 | 0 | 0 |
| welfare | 21 | 2 | 6 | 13 |
| vip | 12 | 2 | 6 | 4 |
| partnersummon | 3 | 2 | 0 | 1 |
| onlinegift | 2 | 2 | 0 | 0 |
| elfin | 28 | 1 | 3 | 24 |
| redbag | 7 | 1 | 0 | 6 |
| activity | 1 | 1 | 0 | 0 |
| equipmake | 1 | 1 | 0 | 0 |

`empty_gen.go` 里有 32 条协议现在已经有真实处理（真实处理优先，空回复不会生效），下次重新生成时可以去掉。

## 没有任何处理的最大模块

action(88), adventure(35), arenateam(32), arenamanypeople(24), elitematch(23), arenapeakchampion(18), planesafk(18), crossarena(17), actionyearmonster(16), crosschampion(16), homeworld(16), returnaction(16), hallows(15), ladder(15), emoguoshi(14), endless_trail(14), heaven(14), planes(14), heroexpedit(13), monopoly(13), homepet(12), actiontermbegins(11), escort(11), guildwar(11), animate_action(9), newfirstcharge(9), orderaction(8), vedio(8), festivalaction(7), neworderaction(7)

## 怎么重新统计

客户端：在 `2DRound/2DRound-codex-restored-source-2026-10-18/outputs/readable-source/lua/0zWHhlnBORyiROK6lvI6VBn6vAbJRQqS/` 下扫 `SendProtocal\(\s*(\d+)`，按第一层目录当模块，生成“模块<TAB>协议号”列表。服务端：给一个临时的 `internal/game` 测试，用 `newEnv(t, 0)` 的路由器（`e.gw.Router.Has(cmd)`）判断真实处理，`emptyReplies` 判断空回复；`newEnv` 不调 `RegisterEmpty`，所以不会把空回复算成真实处理。统计完删掉这个测试。
