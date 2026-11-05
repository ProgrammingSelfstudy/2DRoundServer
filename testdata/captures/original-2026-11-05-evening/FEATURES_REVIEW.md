# 本批活动/竞技场/战斗/天界/精灵协议核对

归属命令58种；分类：{"partial": 28, "difference": 26, "unsupported": 3, "aligned": 1}。只有状态/登录基线的行标为partial；其中确定的字段差异仍保留在finding及JSON单列。

## 关键事务

### 召唤豪礼991037六档

finish701；50/100/200/300/400/500档成功，原服资产/增量与12742先于红点、确认、详情。最后一档有4件实际神装通知及4个原盒子展示通知，不能把二者算两份独立奖励。

activity.go当前全包刷新、无12742、finish布尔、通用已领status2不能覆盖这两类活动。

请求源行：5109,5111,5113,5115,5117,5119；响应源行：8850,8851,8852,8853,8854,8855,8856,8857,8858,8859,8860,8861,8862,8863,8864,8865,8866,8867,8868,8869,8870,8871,8872,8873,8874,8875,8876,8877,8878,8879,8880,8881,8882,8883,8884,8885,8886,8887,8888,8889,8890。

### 升星有礼993014三档

3次code1/msg空，目标status1逐项回0，红点2→1→0。

本地所有已领目标一律status2。

请求源行：5337,5339,5341；响应源行：9461,9462,9463,9464,9465,9466,9467,9468,9469,9470,9471,9472,9473,9474,9475,9476,9477,9478,9479,9480,9481,9482,9483,9484,9485。

### 月卡两次单领和额外16708

单领200钻/120钻；两卡已reward2后16707仍1；16708另领40钻+37001×1后status2。

本地16708被实现成两卡take-all，会在当前样本状态拒绝且不发这份额外奖励。

请求源行：5304,5306,5330；响应源行：9424,9425,9426,9427,9428,9429,9442,9443,9454,9455,9456,9457,9458。

### VIP等级2/1礼包

扣688/238，各发200000钻，净增199312/199762；原服资产→12742→新等级置顶列表→空确认。

本地列表升序，先列表确认后资产全包，无12742。

请求源行：5326,5328；响应源行：9446,9447,9448,9449,9450,9451,9452,9453。

### 每日充值奖励21011

num980/status1→2，奖励200钻、72003/10403/39089各1；原服资产/增量/12742在状态及空确认之前。

本地成功文字与顺序/全包不同。支付来源和资格阈值没有在本批证明。

请求源行：5312；响应源行：9433,9434,9435,9436,9437,9438,9439。

### 竞技场一次失败

请求is_auto0、开场is_auto1；无20001/20019/20062确认，18.877秒后背包10001+6、免费次数3→2、累计0→1、20210 result2、20006 result1空奖、空20063。

本地使用map20001/请求is_auto，等待回合确认才结算，失败发贝里12000并漏状态及结束包。

请求源行：5162；响应源行：8966,8967,8968,8969,8970,8971,8981,8982,8983,8984,8985,8986,8987。

### 天界group2一次免费维修

10510实例23/base11030103→25217 reward.id23→25219双组免费次数1→0且该组累计1/baodi19→25218主动日志。

本地group2永久关闭、score0；免费按组独立、id为序号、baodi计数/日计不同且刷新全包。

请求源行：6089；响应源行：10601,10602,10603,10604。

### 精灵一次单抽

recruit_type1/times1，得到112003，free_time0→1793894400、times0→1、day_count仍0，成功无26551。

本地kind1扣10415券、DayCount+1、全包刷新并额外26551，没有26550/26520通知。

请求源行：525；响应源行：2869,2870,2871,2872。

## 全部归属协议

| 协议 | 判定 | 核对结论 | 本地源码 | 代表源行 |
|---|---|---|---|---|
| 16601 | partial | 4次活动目录可完整解码；本批包含991037召唤豪礼、993014升星有礼，两者panel_type16/cli_type15。目录成员和开放时间依赖服务端活动配置，未与同配置本地环境逐值复现。 | internal/game/activity.go:312, internal/game/activity.go:325 | 2382, 4585, 8493, 13077 |
| 16602 | partial | 4次活动红点列表字段bid/can_get_num由本地具备；仅登录/读取状态，不据此确认全部活动目标规则。 | internal/game/activity.go:328, internal/game/activity.go:337 | 38, 2394, 12783, 13089 |
| 16603 | difference | 原服991037 finish为累计次数701，非布尔；993014领奖后目标status1→0，而本地finish仅0/1、所有已领目标统一status2。本地activityEvent不主动推16603详情，原包225次详情远多于7次请求和9次领奖，另有进度驱动详情推送。 | internal/game/activity.go:213, internal/game/activity.go:247, internal/game/activity.go:363, internal/game/activity.go:441 | 8848, 8854, 8890, 9461, 9470, 9477, 9484 |
| 16604 | difference | 9次领取均code1/msg空。原服背包增量/必要资产→12742→16606红点递减→16604→16603；本地强刷资产和全包、没有12742，16606在确认和详情之后。500档还出现实际神装与原盒子两层通知，需统一发奖展开语义，不可双算。 | internal/game/activity.go:390, internal/game/activity.go:426 | 5109, 5119, 5337, 5341, 8850, 8851, 8852, 8853, 8854, 8879, 8880, 8887, 8888, 8889, 9462, 9463, 9464, 9465 |
| 16606 | difference | 领奖红点从5/4/3/2/1/0或2/1/0在16604之前推送；本地claim最后才推。243个推送包含被动进度更新，不能只按本地主动查询或ready计数增长解释。 | internal/game/activity.go:285, internal/game/activity.go:438, internal/game/activity.go:475 | 8852, 8858, 8864, 8870, 8876, 8888, 9464, 9474, 9481 |
| 16653 | partial | 仅见两次is_buy0/end_time0/charge_id0/award_list空，与本地关闭窗口字面值一致；不证明购买/到期规则。 | internal/game/cross_closed.go:41 | 9376, 9395 |
| 16700 | partial | 2次只读返回7个充值档位；need_rmb以分、首充额外钻石字段可解码，本地按产品表生成。游戏帧未见10947订单请求；辅助HTTP存在pay_web请求200，但不能据此证明外部支付、金额单位或资格公式，见aux-review。 | internal/game/charge_proto.go:15, internal/game/charge_proto.go:106 | 9413, 9440 |
| 16705 | difference | 两张卡激活及单领后card*_acc均0，本地激活期间view返回98/30；原服单领后未逐次推16705，后续查询9442才显示两卡days1/is_reward2。本地单领即时推窗口。 | internal/game/monthcard.go:29, internal/game/monthcard.go:56, internal/game/monthcard.go:145 | 9353, 9357, 9421, 9422, 9442 |
| 16706 | difference | card1/2分别奖励200/120钻；原服10302→12742→16706(code1/msg空)，不强刷背包。本地先16706“领取成功”→16705→10302，无12742。金额表本身与观察一致。 | internal/game/monthcard.go:18, internal/game/monthcard.go:105, internal/game/monthcard.go:129 | 5304, 5306, 9424, 9425, 9426, 9427, 9428, 9429 |
| 16707 | difference | 两张16706都单领且16705已显示两卡reward2之后，原服16707仍status1；16708成功后才变2。本地用两张月卡聚合推导status，不能解释这个额外可领状态。 | internal/game/monthcard.go:153 | 9442, 9443, 9457 |
| 16708 | difference | 在两张月卡已单领之后另获40钻+37001×1，原服10302→10512→12742→16707(status2)→16708空msg成功。本地将其实现为两卡take-all，此时会直接code0无奖，属于业务含义差异，不是仅文字/顺序。 | internal/game/monthcard.go:168, internal/game/monthcard.go:181 | 5330, 9454, 9455, 9456, 9457, 9458 |
| 16710 | difference | 列表从[0]→[2,0]→[1,2,0]，新领取等级在头部；本地VIPGiftLevels按lev升序。成员集合相同但序列与本批不一致。 | internal/game/vip.go:17, internal/store/vip.go:4 | 9444, 9448, 9452 |
| 16711 | difference | lev2扣688/lev1扣238，各奖励200000钻（净增199312/199762），价格和奖励表一致。原服10302→12742→16710→空msg16711；本地先list/reply再资产/全包，无12742。 | internal/game/vip.go:54, internal/game/vip.go:69, internal/game/data/vip.json:1 | 5326, 5328, 9446, 9447, 9448, 9449, 9450, 9451, 9452, 9453 |
| 16712 | partial | 仅有一次charge_sum980及10→1降序累计充值礼包状态，列表构造方式与本地具备；没有16713领取或完整支付链，资格/重置未确认。 | internal/game/chargeacc.go:15, internal/game/chargeacc.go:19, internal/game/chargeacc.go:53 | 9352 |
| 16800 | unsupported | 观察到type1/idx0/两个arg数组为空的主动推送；在internal/**/*.go中未找到16800发送或业务处理，语义未知。 | internal/proto/protocols.json:1 | 13134 |
| 20000 | difference | 剧情开场combat_type3/map20001，本地相符；竞技场combat_type2/map0，本地通用begin仍用fightMap20001。 | internal/game/fight.go:140, internal/game/fight.go:916 | 2653, 8970 |
| 20005 | partial | 仅一次剧情段客户端确认，无同号回复；竞技场段没有此请求。不能按本地20001→20019→20005推进链假定本批也有这些确认。 | internal/game/drama_battle.go:102, internal/game/fight.go:1011 | 370 |
| 20006 | difference | 竞技场实际胜负在20210(result2)，其后20006为result1/combat_type2/空奖空伤害，只是结束通知；本地settleArena不发送该20006，不能把该result1当获胜或二次派奖。 | internal/game/arena.go:673, internal/game/arena.go:750, internal/game/fight.go:1020 | 8984, 8985 |
| 20027 | difference | 竞技场请求is_auto0但开场is_auto1；全包无20001/20019/20062，原服约18.877秒后自动结算。本地沿用请求is_auto，并必须等20001/19/05才settle，存在推进/等待差异。剧情和竞技场均仅开场，无逐回合20002/20004样本。 | internal/game/arena.go:642, internal/game/fight.go:916, internal/game/fight.go:989 | 5162, 2654, 8971, 8985 |
| 20060 | aligned | 41组请求/回包：combat_type3时type2并有20061；其他所见类型返回type0，和本地combatType分支一致。只能确认窗口/挂机场景路由，不能证明战斗公式。 | internal/game/battle.go:75, internal/game/battle.go:86 | 22, 355, 2385, 2641, 8962, 10598, 221, 13098, 13196 |
| 20061 | partial | 仅一条挂机战斗配置，含战斗类型3、关卡、阵型、伙伴及HP10怪物；本地hookBattle具备同种结构。伤害公式/单位完整配置不能由一条配置样本证明。 | internal/game/battle.go:95, internal/game/battle.go:102, internal/game/battle.go:136 | 2642 |
| 20063 | difference | 原服开战时type_list包含3或2，完成后为空；本地查询和通用begin始终发送空列表，不能表示这两次活跃战斗。 | internal/game/guide.go:20, internal/game/fight.go:928 | 2384, 2652, 2697, 8969, 8987 |
| 20200 | difference | 免费次数3→2、积分1000且失败不扣分，与本地部分数值策略一致；原服结算在20210之前推送20200，当前settleArena没有推送此窗口。 | internal/game/arena.go:310, internal/game/arena.go:673, internal/game/arena.go:747 | 2467, 8982, 8988 |
| 20201 | partial | 一次返回3名候选及5人阵容字段，可映射本地sendList结构；本批没有刷新/胜后列表更新，不能确认匹配或机器人公式。已隐去对手身份。 | internal/game/arena.go:328, internal/game/arena.go:590 | 8964 |
| 20203 | difference | 一次is_auto0挑战成功，13011→空msg20203→10409顺序与本地已有路径相符；但后续自动开场/结算链和本地需要逐回合确认不同，详见20027/20210/20006。 | internal/game/arena.go:575, internal/game/arena.go:649 | 5162, 8966, 8967, 8968, 8971, 8984, 8985 |
| 20208 | difference | had_combat_num0→1，num_list空；本地次数字段具备，但原服结算时在20210之前主动推20208，本地settleArena未推。没有20209领取样本。 | internal/game/arena.go:418, internal/game/arena.go:673 | 2466, 8963, 8983 |
| 20210 | difference | 本场失败result2，奖励10001×6，积分/增减积分均不变，双边伤害统计；本地失败固定发12000贝里并sendAssets，只生成单侧统计，且缺邻接20200/20208/20006收尾。未据单场推断全失败奖励分布。 | internal/game/arena.go:28, internal/game/arena.go:685, internal/game/arena.go:695, internal/game/arena.go:747 | 8981, 8982, 8983, 8984, 8985 |
| 20223 | partial | 4次flag0，与本地固定未读战报标志一致；没有读取/新增战报的闭环。 | internal/game/login_init.go:17 | 2469, 4667, 8575, 178, 12924, 13162 |
| 20250 | partial | 仅step0/round0预告状态和服务器给定时间字段；本地冠军赛视图具备字段。没有阶段跨界/竞猜/战斗，本批不足以验证本地日程与赛制。 | internal/game/arenachampion.go:307, internal/game/arenachampion_view.go:49 | 2468, 4666, 8574, 176, 12922, 13161 |
| 20251 | partial | 4次rank0/best_rank0/group0/can_bet60；本地roleInfo可生成这些字段，但额度依赖活动阶段/历史状态，不能只按登录值确认分配周期。 | internal/game/arenachampion_view.go:269 | 2483, 4681, 8589, 13173 |
| 20600 | partial | 4次均为空/关闭样式：id/current_id/map_id/start/end全0、is_all_finish1。字段结构具备，但本地loader通常newRound；需要开启/关闭规则，不能把这批当完成空岛玩法。 | internal/game/adventure.go:237, internal/game/adventure.go:263 | 2405, 4608, 8516, 13101 |
| 20601 | partial | 4次buff_list与holiday_buff_list均空，和本地空状态编码一致；不证明获得或到期buff行为。 | internal/game/adventure.go:272 | 2406, 4609, 8517, 13102 |
| 20604 | partial | 4次空阵容/id0/combat_num0，本地字段结构具备；没有选人/布阵/闯关，状态及战斗机制未确认。 | internal/game/adventure.go:297, internal/game/adventure.go:315 | 2407, 4610, 8518, 13103 |
| 20634 | partial | 4次kill_mon0、5档宝箱status0；与本地初始列表结构一致，没有20635，不能验证击杀阈值/领取及重置。 | internal/game/adventure.go:1075 | 2408, 4611, 8519, 13104 |
| 21002 | difference | 原服充值相关状态推送count0→1且重登仍1；本地welfare固定count0，无动态计数。该变化不证明支付方式。 | internal/game/welfare.go:17 | 2366, 9404, 13061 |
| 21005 | partial | 两次count0/gold0/next_gold1；本地占位固定next_gold0，字段含义仍待补证。 | internal/game/hero_stubs.go:62 | 9405, 9441 |
| 21006 | partial | 4次first_gift空列表；本地dailyGiftList无条件枚举配置礼包及count，缺当前原服空列表的过滤/开关语义；本批没有每日礼包购买。 | internal/game/dailygift.go:145, internal/game/dailygift.go:153 | 2438, 4636, 118, 12864, 13130 |
| 21010 | partial | num980/status1→2，和本地每日首充状态字段/已领标志结构相容；没有阈值边界或跨日，不能确认980产生方式或重置。 | internal/game/daycharge.go:51, internal/game/daycharge.go:88 | 9358, 9433, 9437 |
| 21011 | difference | 奖励200钻、72003×1、10403×1、39089×1；原服10302→10510→12742→21010(2)→10925→空msg21011。本地先“领取成功”确认，再状态、资产和全包，无12742。奖励对应本地开服151天以上表项，但未获同配置本地服时间，不确认年龄条件。 | internal/game/daycharge.go:95, internal/game/daycharge.go:132, internal/game/data/charge.json:1 | 5312, 9434, 9435, 9436, 9437, 9438, 9439 |
| 21021 | partial | 4次flag0；本地RegisterLoginInit固定flag1，属于确定的基线字面差异，标志完整业务含义未知。 | internal/game/login_init.go:18 | 2403, 4606, 8514, 13099 |
| 21024 | unsupported | 7条通知始终list=[{id:4}]；未找到internal/**/*.go中的21024发送实现，语义未确认。 | internal/proto/protocols.json:1 | 2426, 3445, 4692, 8600, 9033, 11227, 13187 |
| 21032 | partial | 两条first_gift六档均status0且end_time非0；本地firstSet.window对于该窗口无条件end_time0，期限字段不一致。没有21033领取。 | internal/game/firstcharge.go:49, internal/game/firstcharge.go:86 | 9397, 9401 |
| 21322 | partial | 登录请求及主动推送均code0，与本地双倍奖励关闭常量一致；不证明开启周期。 | internal/game/battle.go:19 | 13, 2367, 2379, 4339, 4570 |
| 24700 | difference | 原服ids101/102起初status3，之后status0；本地view只生成0/1/2，无status3。未捕获24702，不能将状态3自行解释为已领。 | internal/game/monthfund.go:51, internal/game/monthfund.go:129 | 2441, 9420 |
| 24701 | partial | 两条主动详情id101/group6、id102/group8，均inactive/current_day0/endtime0；本地基金组硬编码1/3，与捕获6/8不同。不能据此推出购买或选组公式。 | internal/game/monthfund.go:36, internal/game/monthfund.go:72, internal/game/charge_proto.go:86 | 9355, 9356 |
| 25200 | partial | 4次count2/buy_count0/max_dun_id0及一章信息；本地天界章节协议结构具备，没有25205/25208，不能评价挑战/扫荡奖励。 | internal/game/heaven.go:325 | 2377, 4580, 8488, 13072 |
| 25217 | difference | group2/recruit_type1/times1成功，先10510再25217，reward.id23与新增装备实例23一致，然后主动25219和25218。本地group2永远拒绝，gain强刷资产/全包，reward.id用抽次序号i+1，且不主动推两个状态。 | internal/game/heaven_fight.go:295, internal/game/heaven_fight.go:388, internal/game/heaven_fight.go:445, internal/game/heaven.go:219 | 6089, 10601, 10602, 10603, 10604 |
| 25218 | difference | 没有同号请求，却在抽取后收到type2/group2/20条日志主动推送；本地仅log请求时回复，pull不发该包。日志身份未导出。 | internal/game/heaven_fight.go:462, internal/game/heaven_fight.go:466 | 10604 |
| 25219 | difference | group2在评分提高后由未开变开；一次group2免费抽使两个group的free_times1→0，日计day_count仍0，baodi_count20→19，reset_time0→次日0点值。本地免费按组独立、day_count每次+1、baodi按已用计、group2永不开放；本批不能证明实际重置触发时刻。 | internal/game/heaven_fight.go:295, internal/game/heaven_fight.go:303, internal/game/heaven_fight.go:403, internal/game/heaven_fight.go:458 | 2378, 10567, 10574, 10581, 10589, 10590, 10603 |
| 25220 | partial | 登录及状态推送为num3/空holy_eqm_set_cell，和本地免费方案格数/空方案结构相容；没有保存/扩容请求，其他规则未确认。 | internal/game/holyeqm.go:663, internal/game/holyeqm.go:684 | 2414, 4617, 78, 12826, 13111 |
| 25232 | difference | 原服score0→1960→6061→8026→10287；本地固定0，不能正确驱动group2解锁或分数显示。精确评分公式未由这些状态单独证明。 | internal/game/heaven.go:173 | 2488, 10567, 10574, 10581, 10589, 10599 |
| 26500 | partial | 6个幽地状态可解码，其中id1/do_id10601/state1/end_time非0，其他未开；仅4次登录读取，没有26503/26505等孵化/领取事务。 | internal/game/elfin.go:311 | 2480, 4678, 8586, 200, 12948, 13174 |
| 26510 | partial | 原服lev1/def210但def_p0/def_s0；本地treeMessage把def_p和def_s都赋给def，确定字段表示不同。其余属性/战力公式仍未证明。 | internal/game/elfin.go:639, internal/game/elfin.go:672 | 2481, 2867 |
| 26520 | unsupported | 单抽成功后推awards=[{item_bid:112003}]；本地未找到26520发送，只有图鉴查询26509，缺少对应新增图鉴通知。 | internal/game/elfin.go:630, internal/proto/protocols.json:1 | 2872 |
| 26550 | difference | 单抽前free_time0/times0/day_count0，之后free_time变次日0点/times1/day_count0并主动推送；本地kind1扣券且不会更新FreeAt、DayCount必+1，summon不发26550。这是已确认样本分支差异，不推断整套收费映射。 | internal/game/elfin_team.go:22, internal/game/elfin_team.go:93, internal/game/elfin_team.go:162 | 2482, 2868, 2871 |
| 26551 | difference | 一次请求recruit_type1/times1，无同号成功确认；相邻响应为10510(112003)→26553→26550→26520，无相邻资产/票券扣减，初始背包亦无10415。本地kind1只走票券分支、刷新全包并额外回26551(flag1)，不能复现本例。 | internal/game/elfin_team.go:76, internal/game/elfin_team.go:93, internal/game/elfin_team.go:164 | 525, 2390, 2869, 2870, 2871, 2872 |
| 26553 | partial | 单次返回group110、112003×1、quality3/jie1；本地结果字段和固定group110具备，但其邻接通知/收费分支见26551。单样本不验证概率/保底。 | internal/game/elfin.go:25, internal/game/elfin_team.go:143, internal/game/elfin_team.go:167 | 2870 |
| 26555 | partial | 一次type0请求，返回team1/plan_id0/pos1空精灵；本地teams结构具备，没有改队/方案操作，不确认保存与切换。 | internal/game/elfin_team.go:230 | 361, 2645 |

## 边界

- 只核对本批观察到的协议；完全解码不等于全业务一致。
- 源行是一基完整私有JSONL位置；跨方向按时间排序，同tick不推断比单方向TCP顺序更细的因果。
- 本批无对应UI/动作日志，不据协议单独证明按钮、执行者、支付渠道或充值资格公式。
- 没有跨日/05:00边界、活动失败、月卡/礼包重复拒绝、天界付费维修、精灵十连及概率样本。
- 支付状态在14:34:55集中变化，辅助HTTP存在pay_web请求200；未建立独立外部支付结算/回调链，不宣称购买由AI执行，不从HTTP200推出金额单位或资格公式。
- 空岛、冠军赛、孵化及神装方案主要只有登录/只读基线，不能当完成玩法。
