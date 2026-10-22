# 服务端对客户端的覆盖（2026-10-21 快照）

口径：客户端 Lua 里所有 `SendProtocal(编号` 发出的请求协议（1102 个，分布在 97 个模块），逐个看服务端是 **真实处理**、**只有空回复**（`empty_gen.go`，格式对但没内容）还是**什么都没有**。服务端主动推送的协议不在内。“真实处理”只表示有实现，不表示已和原服务器核对（核对情况见 `OPEN_QUESTIONS.md`）。

| 请求协议 | 个数 | 占比 |
|---|---|---|
| 真实处理 | 101 | 9.2% |
| 只有空回复 | 115 | 10.4% |
| 什么都没有 | 886 | 80.4% |

97 个模块里：22 个至少有一个真实处理；5 个全做了（mail、onlinegift、activity、seerpalace、newpartnersummon）；75 个一个都没做，合计 768 个请求（多数是公会、各种竞技场、跨服、家园、限时活动）。

## 有真实处理的模块

| 模块 | 请求数 | 真实 | 空回复 | 没有 |
|---|---|---|---|---|
| hero | 78 | 14 | 8 | 56 |
| friend | 17 | 12 | 0 | 5 |
| battle_drama | 13 | 8 | 1 | 4 |
| backpack | 11 | 7 | 0 | 4 |
| mail | 7 | 7 | 0 | 0 |
| battle | 17 | 6 | 0 | 11 |
| mall | 12 | 5 | 0 | 7 |
| invitecode | 9 | 5 | 1 | 3 |
| login | 6 | 5 | 0 | 1 |
| elitesummon | 12 | 4 | 0 | 8 |
| task | 7 | 4 | 1 | 2 |
| seerpalace | 4 | 4 | 0 | 0 |
| role | 65 | 3 | 10 | 52 |
| chat | 9 | 3 | 0 | 6 |
| rank | 5 | 3 | 0 | 2 |
| newpartnersummon | 3 | 3 | 0 | 0 |
| welfare | 21 | 2 | 6 | 13 |
| vip | 12 | 2 | 6 | 4 |
| partnersummon | 3 | 2 | 0 | 1 |
| onlinegift | 2 | 2 | 0 | 0 |
| elfin | 28 | 1 | 3 | 24 |
| activity | 1 | 1 | 0 | 0 |

## 没有任何处理的最大模块

action(88), adventure(35), arenateam(32), guild(28), arena(25), arenamanypeople(24), elitematch(23), arenapeakchampion(18), planesafk(18), crossarena(17), actionyearmonster(16), homeworld(16), market(16), returnaction(16), crosschampion(16), ladder(15), hallows(15), planes(14), emoguoshi(14), endless_trail(14), heaven(14), monopoly(13), heroexpedit(13), homepet(12), guildsecretarea(11), guildwar(11), actiontermbegins(11), escort(11), guildskill(11), animate_action(9)

## 怎么重新统计

客户端：在 `outputs/readable-source/lua/0zWH…` 下扫 `SendProtocal(\d+`，按第一层目录当模块。服务端：给一个临时的 `internal/game` 测试，用 `newEnv(t)` 的路由器（`e.gw.Router.Has(cmd)`）判断真实处理，`emptyReplies` 判断空回复；统计完删掉这个测试。
