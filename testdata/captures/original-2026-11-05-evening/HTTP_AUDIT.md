# 本批辅助HTTP离线边界核对

存在pay_web请求；游戏帧未见10947订单请求。本批不能据此证明实际外部支付/金额单位/资格公式，不能再概括为整个抓包未见支付请求。

独立重组10个HTTP连接/20方向，共8137字节payload；0缺口/0冲突。Content-Length或chunk边界均完整。DNS仅沿用父任务2包计数，不输出任何名称。

## HTTP userlogin token（difference）

4次POST均HTTP200；单块chunked解码正文65字节，Lua表仅error/msg键，error=0、msg为字符串。local同类token路由输出code=1/msg，控制键error与code存在结构差异；两者都有字符串msg，不能单凭键差证明本地登录失败。

PCAP请求记录：9,3134,3423,10006；响应记录：13,3136,3425,10009。
游戏JSONL源行：2360,2363,4563,4566,8471,8474,13055,13058。
本地源码：internal/login/http.go:47；internal/login/http.go:144；internal/login/http.go:161。

## HTTP update resource（partial）

4次GET均404/146字节HTML正文；本地只有update/config专门处理，其他资源请求落default404，status边界相符。原404未阻止会话继续，不能称AI/客户端卡死或推出更新整体失败。各次10301早于对应404到达，404之后仍有105xx等游戏推送。未核对客户端资源缓存/回退策略。

PCAP请求记录：42,3178,3457,10049；响应记录：53,3183,3465,10058。
游戏JSONL源行：2363,2391,4566,4595,8474,8501,13058,13086。
本地源码：internal/login/http.go:43；internal/login/http.go:60。

## HTTP root request（partial）

1次POST根路径HTTP200，正文为2381字节HTML；本地根路径返回纯文本ok。可确认响应体格式差异，但本批没有页面DOM/回调证据，不能证明该HTML是后续游戏进入条件。

PCAP请求记录：472；响应记录：474。
游戏JSONL源行：无直接对应。
本地源码：internal/login/http.go:57；internal/login/http.go:59。

## HTTP pay_web（difference）

1次POST pay_web，query仅保留money/name/productId/role_id/srv_id键名，请求正文0字节；HTTP200、chunked解码正文27字节，为非结构化UTF8文本，无结构化订单/回执字段。当前本地无对应pay_web路由，支付入口为本地pay/start或10947创建订单再跳转页面、管理员确认交付。因此能确认入口/应答模式不同，不能从请求或HTTP200证明实际外部支付、金额单位、购买执行者、商品映射或签到/月卡资格公式。

PCAP请求记录：4681；响应记录：4684。
游戏JSONL源行：9399,9407,9413,9432,9439。
本地源码：internal/login/http.go:39；internal/login/http.go:60；internal/login/pay.go:175；internal/login/pay.go:235；internal/login/pay.go:239；internal/game/charge_proto.go:122；internal/admin/server.go:175；internal/admin/pay.go:125；internal/game/charge.go:190。

## 404后的会话继续

| 404响应UTC | 之前10301源行 | 之后游戏源行/协议 |
|---|---:|---|
| 2026-11-05T14:25:50.411581Z | 2363 | 2391/10501 |
| 2026-11-05T14:29:36.946452Z | 4566 | 4595/10503 |
| 2026-11-05T14:30:10.598893Z | 8474 | 8501/10501 |
| 2026-11-05T14:40:30.469088Z | 13058 | 13086/10501 |

pay_web响应时刻14:34:55.649929Z，随后游戏10301源9407时刻14:34:55.653383Z，约3.5毫秒后；同批12741购买成功源9399和充值/资格状态变化可作时序关联。单一HTTP请求与随后状态变化不足以证明外部支付已完成、money单位、具体资格公式或14101额外签到前置条件。跨HTTP/游戏通道只比较时间，不把私有JSONL源行当跨方向发生顺序。

本地不是原支付回调兼容实现：本地HTTP及10947入口创建等待支付订单；金额取产品表，HTTP入口302跳转订单页。管理员受权限保护的confirm接口调用ConfirmChargeOrder，先校验waiting状态再交付，之后调用gateway推送充值状态。本次抓包未证明原服等同该本地人工确认流程。

internal/login/pay.go:173；internal/login/pay.go:185；internal/login/pay.go:213；internal/login/pay.go:235；internal/game/charge.go:167；internal/game/charge.go:185；internal/admin/server.go:175；internal/admin/pay.go:126；internal/admin/pay.go:142；internal/game/charge.go:202；internal/game/charge.go:235；internal/game/charge_proto.go:34。

限制：
- 仅离线分析固定PCAP，没有访问录制URL、重放请求、使用凭证或操作设备。
- HTTP200是HTTP响应状态；非结构化正文未建立独立支付平台结算/回调链。
- 仅保留请求字段名、响应控制键/状态、字节数及匿名时序；原始URL、Host、账户、角色、cookie/token及query值均不输出。
- DNS仅纳入父任务计数，不输出名称或地址，不推断归属。
- 4次404之后仍有游戏数据，仅排除把这4个404本身直接称为卡死；不证明整个采集期间没有其他UI/AI停滞。

## DNS record verification

PCAP records467/468 form one query and one response with matching transaction IDs, rcode0 and one answer. Domain names, transaction-ID values and address values are not exported. This verifies only the captured lookup, not all resolver behavior.
