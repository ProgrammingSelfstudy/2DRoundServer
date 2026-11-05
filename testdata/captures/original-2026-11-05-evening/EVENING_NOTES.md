# 11月5日晚间手工抓包：全协议离线核对

本批为用户手工操作后暂停的原版客户端抓包。分析过程只读取固定快照和本地源码；没有继续操作游戏、补发协议、重启抓包或修改玩法。核对范围是本批实际出现的全部219个协议号，不是未出现的所有游戏功能。

Source revision: `ddc71aa`; capture size: **1,775,235 bytes**; game frames: **13,300** (c2s6667/s2c6633); game connections: **4**; observed game commands: **219**; decode errors: **0**.

Observed game frame time: **22:25:50 - 22:41:07 Asia/Shanghai**. Capture connection began22:25:37; the earlier zero-byte connection is excluded.

除219个游戏协议号外，本批另有10个HTTP连接（20个payload方向，8137字节）及2个DNS包；四次更新GET均404，四次userlogin为200，另有根路径POST和pay_web各一次200。它们独立列在HTTP_AUDIT.md；域名、端点、请求参数值、认证及Cookie不导出，也没有访问录制URL。

已对每个已出现协议号核对解码、帧数、代表源行、本地处理/发送位置及可观察业务。按协议号分类，同一业务问题可能涉及多个协议；partial允许存在已记录的基线字段差异，但没有足够事务验证全部规则；aligned仅指本批已观察分支，不代表整套玩法无缺口。

| status | command IDs |
|---|---:|
| aligned | 12 |
| difference | 81 |
| unsupported | 13 |
| partial | 113 |

[All219 command rows](PROTOCOL_COVERAGE.md) / [machine-readable coverage](protocol-coverage.json) / [field presence and types](protocol-field-inventory.json).

## Confirmed observations

- **传输和结构**: 10475条完整PCAP记录、4条均见SYN及游戏握手的连接；双向无缺口、冲突重传、裁切或帧尾残留。独立重组的每协议/方向计数与13300帧完整解码一致，解码错误0。
- **预签名**: 3301个1197都与紧随的请求摘要和混淆code一致，65个免签请求符合本地免签组。签名及认证值均不导出。
- **升级/突破成本**: 502次升级的资产实扣全部匹配本地表；步长为11次+5、1次+4、490次+1，最高550级。6次突破的贝里及10001卡50/100/200/500/1000/2000也全部匹配。
- **召唤费用与卡映射**: 175次成功产生1732名英雄，卡到英雄的映射全部符合本地公式。167次钻石十连每次实扣2000，合计334000；不把夹杂的分解回款当免费，也不据此推断抽取概率。

## Differences and branch boundaries

| area | finding | detail |
|---|---|---|
| HTTP登录/支付入口 | 四次登录token响应采用Lua文本error=0/msg，本地返回code=1/msg；根路径POST响应内容也不同，原pay_web入口与本地/pay/start等路径不一致。已核对捕获响应结构，但不据200认定外部付款或资格公式。 | [HTTP_AUDIT.md](HTTP_AUDIT.md) |
| 英雄升星与献祭 | 11005共32次：29成功、3缺料失败；其中19次成功携item_expend魂石，最高25星。3次失败发生在补卡前；用户补突破卡后，22:40:43最后一次11005返回result1，最终25星成功。当前本地starPlan仍拒绝魂石。132次献祭成功消耗1346名1级英雄（1108名3星、238名4星），星级基础返还分别为魂/卡/经验10/10/500及25/25/1000；本地refund漏掉这些基础奖励。 | [HERO_REVIEW.md](HERO_REVIEW.md)；11005/11075/11076 |
| 召唤成功与拒绝链 | 194次23201请求=175成功（173十连+2单抽）+19次明确拒绝（16背包满、2券不足、1章节未开）。拒绝经12741/12745告知，不能把没有同号响应直接当丢包。成功与本地的英雄逐条推送、资产、确认、23204顺序不同。 | [REWARDS_REVIEW.md](REWARDS_REVIEW.md)；23201/23204 |
| 礼包、实例与奖励展示 | 9次10515成功涉及背包实例定位和自选args，本地按基础号查表且忽略args。12742不能统一假设id0或非空：有真实装备实例id，也有空奖励列表。活动四盒子的外层展示与已入包的四件神装不能重复计成八件物品。 | [REWARDS_REVIEW.md](REWARDS_REVIEW.md)；10515/12742 |
| 额外签到 | 本批新增day1/status1→day1/status2且day不增加的14101成功，新增99955礼包×1；本地当日直接拒绝，无法覆盖这条路径。它发生在购买相关状态及两卡单领之后，但具体资格阈值、支付方式、日/月重置仍未证明。 | [REWARDS_REVIEW.md](REWARDS_REVIEW.md)；14100/14101 |
| 月卡与附加领取 | 16706两卡各领取200钻、120钻后，16708仍独立发40钻+37001×1，并更新16707状态。本地把16708当两卡一键领取，会漏掉该独立路径。包内有购买/charge_sum状态，不能据此认定真实外部支付途径或金额单位。 | [FEATURES_REVIEW.md](FEATURES_REVIEW.md)；16705..16708 |
| 日常与成就 | 原服用10409推进日常，本地helper仍发10403；10400的accept_time/progress.id及已领progress列表不同。成就列表按39个当前阶段给出，本地直接列199项；领取后推进下一档、空成功文字和奖励顺序仍有差异。 | [REWARDS_REVIEW.md](REWARDS_REVIEW.md)；10400/10409/16400..16402 |
| 活动领奖 | 16603的finish701不是布尔；升星活动993014领取后状态1→0，不能统一写成2。召唤豪礼的实际入包、12742、红点、确认、详情刷新顺序与本地不同。 | [FEATURES_REVIEW.md](FEATURES_REVIEW.md)；16603/16604/16606 |
| 战斗与主线 | 竞技场20210 result2是战败，但随后空奖20006 result1是结束通知；该事务没有20001/20019/20062仍自动结算，与本地推进等待不同。主线13016的等级/经验字段、资产/背包和13001阶段/冷却顺序也不同。不能仅凭协议记录断言本地或原版UI必然卡死。 | [FEATURES_REVIEW.md](FEATURES_REVIEW.md)及覆盖表；200xx/20203/20210/13001/13016 |
| 装备、神装及属性刷新 | 普通装备/战技通知顺序、扩容文字、神装的11007批量刷新存在差异；本批神装路径没有10306，不能把之前一条固定推送策略推广到所有穿戴。容量300→360的额外变化伴随权益相关状态，原因不能当作初始容量规则。 | [HERO_REVIEW.md](HERO_REVIEW.md)；11009/11010/11037/11093 |
| 天界与精灵 | 本批已见天界group2解锁、抽取及score10287，免费状态/实例奖励不同于本地固定关闭/零分。精灵单抽为26551→26553→26550→26520等更新，免费分支和通知链仍有差异。 | [FEATURES_REVIEW.md](FEATURES_REVIEW.md)；25217/25232/265xx |
| 初始化与其他基线 | 登录前主动1169、阵型保存文字、魂器缺料文字，以及23805/24124/25814/26100/27215/28200/29100等固定字段有差异。13601完整活动进度、27700/03/14/30活跃巅峰状态等，本地仍有关闭占位或缺发送；其余状态不能只按不同角色/配置数值判错。 | [PROTOCOL_COVERAGE.md](PROTOCOL_COVERAGE.md)；全部219项逐项列出 |

Detailed domain reports: [heroes](HERO_REVIEW.md), [rewards](REWARDS_REVIEW.md), [features](FEATURES_REVIEW.md), [HTTP/DNS](HTTP_AUDIT.md).

## Repair priorities for a later task

- **先补会漏发或拒绝合法操作的路径**: 献祭基础奖励、礼包实例/自选参数、魂石升星、额外签到、月卡独立附加领取。
- **再统一事务通知及条件**: 召唤拒绝提示、日常10409/成就阶段推进、活动状态/红点、背包增量和战斗结束含义。
- **最后补状态模型和未实现玩法**: 天界第二组/评分、成长活动和巅峰赛；不能只把捕获数值硬编码进响应。

## Limits

- 本批只有协议记录，没有本次截图和逐步UI动作日志；按钮名称、操作者意图及每个提示的界面表现只能保持未确认。
- 本批没有跨日/05:00连续边界，重置周期和额外签到资格公式仍未知；当前代码未据此修改。
- 本批未出现10927在线领取、20301活跃宝箱、13030/13031/13032材料副本或13403/13404市场刷新事务，不能把已有旧包或计划算作本批补齐。
- 本批虽有一个HTTP支付入口请求及购买相关游戏状态，仍不能由单批数据确定真实外部支付完成、金额单位或额外签到资格。抽取概率/保底及未捕获失败分支亦不能据此确定。
- 内部完整的PCAP/TCP流证明已保存数据可连续解码，不证明采集范围之外的网络完全无损。
- 本次未修复、部署或做本地服务端真机验收；当前结论针对源码ddc71aa及本批已观察路径。
- REA MCP仍缺席，保留已有config_drift诊断，没有改配置/安装引擎；本报告是普通本地离线工具和源码核对结果，不是REA Evidence。
- 原包、完整JSONL、原始接收日志和身份/广播原文留在仓库外私有目录。公开证据仅保留字段投影与代表性事务，列表抽样长度有单独标注。

## Evidence and validation

[evening-evidence.jsonl](evening-evidence.jsonl) contains 654 representative, projected rows. Source lines are1-based in the complete private session-final.jsonl. Same-direction order is retained; cross-direction source-line order is not chronology. Lists may be sampled: original lengths and sample limits are explicit.

[evening-manifest.json](evening-manifest.json) records hashes, independent PCAP/TCP framing checks, review coverage and stop status. Raw PCAP, complete decode, receive log and complete review details stay outside the repository. No authentication/signature/identity values or raw broadcast text are exported.
