# 11-05 双模拟器互动：好友、赠礼、关注、留言、私聊、拉黑（c42）

两台 moniqi 模拟器（Android 13 x86_64 + arm64 转译）同时在线，各登一个账号，原版客户端同包名，直接连原服 93 服。抓包不经手机：给 moniqi 的网络栈加了 `MONIQI_PCAP=<文件>` 环境变量，把客户机网卡上的每个以太网帧写成经典 pcap（链路类型 1），`pcapdecode` 直接可解，含完整登录握手。A = 新号“纯情的回忆”（rid 21，124 级），B = 老号 93 服角色“王者之单身”（rid 22，148 级）。两个原包（各约 28 万字节，0 解码错误）放仓库外；这里是互动协议的脱敏证据 `c42-social-evidence.jsonl`（每条带 `emu` A/B，登录推送之后）。没有战斗，没有花钻，没有真钱。

## 好友

- 推荐 `13320 ""` → `13320 {recommend_list [{rid 21, name, lev 124, power, srv_id}]}`：**只列在线角色**（印证 c36 的空结果是对方不在线）。
- B 申请：`13303 {rid 21, srv_id}` → `13303 {code 1, msg "请求已发放，请耐心等待"}`；A 实时收到 `16800 {type 2}`（红点）、`13304 {…B 的名片}`、`13311 {friend_req_list […]}`。
- A 同意：`13305 {agreed 1, rid 22, srv_id}` → `13310`（好友名片，intimacy 0、is_draw、is_present…）→ `13305 {code 1, msg ""}`；B 实时收到 `13310`。拒绝分支没测（agreed 0）。
- 赠心：A `13316 {code 0, rid 22, srv_id}`（code 0 = 赠送，1 = 回礼/领取，与 13317 区别见下）→ `12741 “成功赠送王者之单身礼物,获得<assets=1/>10000”`（赠送者得 1 万贝里）→ `13316 {code 1, draw_count 30, is_draw 0, is_present 1, msg "", present_count 9, type 0}`；B 实时收到 `13302`（好友状态 is_draw 1）、`16800 {type 13}`、`12741 “…向你赠送了一份礼物哦”`。每日赠送 10 次上限（present_count 10→9），领取上限 30（draw_count）。
- B 领礼物（一键领取 `13317 {code 1, list [{rid 21, srv_id}]}`）：→ `12741 “成功领取好友礼物,获得<assets=1/>10000，<assets=14/>10”`（贝里 1 万 + 14 号货币 10 个，界面是爱心）→ `13300`（好友列表，draw_count 29）→ `13317 {code 1, list [{is_draw 0, is_present 1, rid, srv_id}], msg "", type 1}` → `13311 {friend_req_list []}`。点“回礼”同样走 `13316 {code 0}`。
- 进窗口 `13300 ""`、`13311 ""`、`13330 ""`（黑名单）。好友 1/100、黑名单 1/50。

## 个人信息卡和空间

- 点好友头像：`10315 {rid, srv_id}` → `10315 {…名片：avatar_bid, elite_lev, fans_num, fans_rank, gid, gname, 阵容…}`（段位、公会、剧情战斗阵容，佩罗娜等级）；按钮：举报、加黑名单、切磋、私聊、个人空间。
- 个人空间：`10813 ""` → `{feedback_logs []}`；关注 `25801 {rid, srv_id}` → `12741 “关注成功”` → `25801 {code 1, flag 1, msg ""}`（粉丝 0→1，按钮变“取消关注”）；被关注方 B 收到 `12741 “…关注了您”`。留言板 `25837 {num 44, rid, srv_id, start 0}` → `{max_num 0, msg "", num 44, result 1, room_bbs_info []}`；留言 `25835 {msg "hello", rid, srv_id}` → `25835 {msg "留言成功", result 1}` → `25841 {max_num, rid, room_bbs_info [{bbs_id 1, bbs_type 1, lev, msg, name, reply_name…}]}`；B 实时收到 `16800 {type 22}`、`25841`（新留言）。荣誉墙 `25806 {rid, srv_id}` → `{honor_badges [], point 0, use_badges []}`；成长之路 `25832 {num 10, rid, srv_id, start 0}` → `{max_num 2, num 10, room_grow_info [{arge, id 101302, order 2, time}, {id 100003, order 1, time}]}`（100003 是创号、101302 是升到 124 级）。家园灰（未开）。

## 私聊 12720/12721

- A `12720 {len 0, msg "hi", to_rid 22, to_srv_id "symlf_93"}`；A 自己收到 `12721 {flag 1, msg "hi", name 对方, rid 22, lev, head_bid, bubble_bid, capacity, len …}`（flag 1 = 我发的），B 收到 `12721 {flag 2, name 发送者, rid 21…}`（flag 2 = 对方发的）加 `16800 {type 8, arg_str [{1 srv_id}, {2 发送者名}], arg_uint32 [{1 rid}, {2 0}]}`（私聊红点）。聊天频道页签：同省、跨服、世界、公会、私聊、系统。

## 拉黑 13332

- 弹窗文字：“被列入黑名单后将无法接收到该玩家发出的消息，是否确认将XX列入黑名单？（若为好友则会把该玩家从好友列表里删除）”。确认 `13332 {rid 22, srv_id}` → `13331 {black_list [{…is_online 1, lev, login_time, name, power…}]}` → `13332 {code 1, msg "添加成功", rid, srv_id}`；被拉黑方 B 收到 `13308 {rid 21, srv_id}`（被删除好友通知）。A 的好友列表 1→0，黑名单 0→1（“移除”按钮可撤销，没点）。

## 统计

| 项目 | 数 |
|---|---|
| 场景 | 7（申请/同意、赠心、领取回礼、关注、留言、私聊、拉黑） |
| 双方新增协议 | 13300~13332 好友族、10315、25801/25806/25832/25835/25837/25841、12720/12721、16800 |
| 战斗 | 0 |
| 没有改本地代码 | |

## 没确认

拒绝申请（13305 agreed 0）、删除好友（13306/13307？）、移除黑名单、被拉黑方再加好友的提示、赠送上限满的回包、领取上限、切磋、举报、取消关注、留言回复/删除、对方不在线时的私聊（离线消息）。
