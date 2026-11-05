# 晚间抓包：奖励、背包、邮件、召唤与日常核对

本域覆盖35种命令、1217帧；结构均由完整解码读取，业务对照结论按下表分级。原包及完整解码不入库，本文不含认证/账号/角色/端点/聊天身份。仅核对，未修改代码或操作手机。

## 关键结论

1. 194次23201请求已有175成功回包和19独立拒绝通知解释，不能把19当成丢包。
2. 14101捕获额外签到：day1/status1→day1/status2，奖励99955×1；当前本地仅支持首次领取。
3. 9次10515包含4种本地未实现的自选礼包；每日签到礼包内容8项与本地表一致，但通知和实例/选择建模不同。
4. 任务、成就和邮件领取确认文字及奖励/增量顺序仍存在明确差异。
5. 神装外层4盒子和已展开的4件实物不能重复计奖。

## 逐命令覆盖

| 命令 | 状态 | 捕获/本地核对 | 源行 | 本地源码 |
|---|---|---|---|---|
| 10400 | difference | 4次完整列表均13任务；accept_time=0、progress.id=0；已领90003在末次为finish2/progress=[]。本地accept_time填日界、progress.id=1、已领仍保留progress。 | 2430,4631,8538,13124 | internal/game/quest.go:75; internal/game/quest.go:156 |
| 10406 | difference | 一次领取90003成功：10302(+20钻/+10活跃)→12761/12741奖励文字→10406 flag1/msg空；本地先回领取成功，再10403、10302和全量背包。奖励表数额相同。 | 5125,8896 | internal/game/quest.go:177; internal/game/data/quests.json:1 |
| 10409 | difference | 召唤90003达到3/3和竞技场90005达到1/3由10409增量推进；本地questEvent发10403而非10409。 | 2891,8968 | internal/game/quest.go:116; internal/game/quest.go:150 |
| 10500 | difference | 4次背包头volume9999/open_times0，所列29/26/26/29种物品均bind1；实例号与基础号不同且重登保持。本地volume100、普通实例号=base，仅两种物品bind1；缓存时间模型也不同，精确算法未知。 | 2390,4592,8500,13085 | internal/game/bag.go:85; internal/game/bag.go:105; internal/game/bag.go:161; internal/game/bag.go:196 |
| 10501 | difference | 4次装备背包volume9999、件数5/7/7/13，所见bind1；本地沿用volume100和普通装备bind0/合成实例ID策略。普通装备堆叠形态可由英雄域继续核对。 | 2391,4594,8501,13086 | internal/game/equipment.go:50; internal/game/bag.go:161 |
| 10503 | difference | 4次精灵背包均item_list=[]但volume9999；本地空列表可匹配，头部容量100/缓存时间模型不同。未见本批精灵背包物品使用闭环。 | 2392,4595,8503,13087 | internal/game/bag.go:138; internal/game/bag.go:161; internal/game/bag.go:177 |
| 10510 | partial | 24个新增推送，包含storage1普通物品与storage3装备/神装；本地sendBagDelta支持新增，但开礼包、邮件、召唤仍用旧全量刷新路径。原服实例需从实际背包追踪，不能硬编码为base。 | 4310,4315,4323,8879,9430 | internal/game/bag_delta.go:51; internal/game/use.go:109; internal/game/mail.go:202; internal/game/summon.go:305 |
| 10511 | partial | 18个删除推送，storage1/3各有，包含id/pos/storage；本地delta协议形态已支持，但并非所有业务调用且实例建模仍不同。 | 4308,4314,4322 | internal/game/bag_delta.go:51; internal/game/bag_delta.go:75 |
| 10512 | partial | 186个绝对状态推送，均storage1，226个条目；明确礼包、召唤券、邮件和培养材料的最终余额更新。本地delta生成器支持绝对状态，礼包/召唤/邮件调用路径尚不统一。 | 2684,4311,8874,13191 | internal/game/bag_delta.go:51; internal/game/use.go:109; internal/game/mail.go:202; internal/game/summon.go:305 |
| 10515 | difference | 9次成功均msg空，按实例id使用；641236三次64/42/34自选共140英雄；99955内容与本地表8项相同；88828/39528/39179自选成功但不在本地giftTable，args被本地忽略。原服先扣包/新增或余额/12742后确认，本地先“使用成功”再资产/全包。 | 228,2626,2346,4313,2348,4317,2354,4325,5183,9000,5187,9005,5189,9009,8273,12628,8359,12700 | internal/game/use.go:75; internal/game/use.go:83; internal/game/use.go:99; internal/game/data/gifts.json:1 |
| 10528 | unsupported | 出现一次minu_exp100/time600推送；本地game源码未发现10528生产路径。触发与其他域进程推进相关，需由根审阅对应上下文，不将其归因到使用礼包。 | 2691 | 未找到显式生产路径 |
| 10800 | partial | 4次邮件列表，前三次空、末次1封未读；本地具有同形列表/归属校验。邮件正文、发送者和服务器标识属于私有身份/内容，不导出也不以模板文本推断业务。 | 2442,4640,8548,13135 | internal/game/mail.go:74 |
| 10801 | difference | 一次附件10001×1000000领取：10512余额293308→1293308→活动通知→12742→10801 code1/msg收取附件成功；本地先“领取成功”确认，再无条件10302和全包，缺12742。 | 12972,13194 | internal/game/mail.go:124; internal/game/mail.go:141; internal/game/mail.go:200 |
| 10804 | difference | 两次删除：空ids也回复删除成功，1个已领取邮件删除同文案；本地非空成功一致，但空完成列表回没有可删除的邮件。 | 4556,4700,12974,13195 | internal/game/mail.go:175; internal/game/mail.go:190 |
| 10805 | difference | 读取成功code1/msg读取邮件成功、is_delete0及read_time有值；本地成功msg为空，状态字段结构一致。 | 12970,13190 | internal/game/mail.go:90; internal/game/mail.go:100 |
| 10806 | partial | 一次详情给出未读status0/read_time0、assets空、10001×1000000附件；本地同形详情结构支持。其余邮件身份/文本/超时属于该邮件状态，非通用已确认规则。 | 12968,13189 | internal/game/mail.go:44; internal/game/mail.go:108 |
| 10926 | partial | 5请求6响应，累计812→840→855→1038→1072→1692秒、已领150/60保持降序；源2634无请求主动推送，本地只在进度请求或10927成功后推送。无10927领取，本批不补证奖励/重置周期。 | 2404,2634,2643,4607,8515,13100 | internal/game/onlinegift.go:87; internal/game/onlinegift.go:136; internal/game/onlinegift.go:158 |
| 12741 | difference | 29次通用提示：16次英雄包满、1次未通关解锁、11次任务/成就奖励文字、1次购买成功；本地召唤拒绝静默、任务/成就确认文字内嵌，因此调用路径缺少这些独立提示。 | 2639,8895,8900,9399,12153,12481 | internal/game/summon.go:222; internal/game/quest.go:202; internal/game/feat.go:145 |
| 12742 | partial | 181次source0、451条num均字符串；19次asset_list为空，6条奖励条目id非0（唯一实例/特殊物品）。本地sendGainList强制id0并跳过空列表，不能当作本批所有12742的完整通用模型。活动四件神装与外层四盒子展示不可双计入账。 | 2625,4312,4324,8880,8882,8884,8886,8887,9473,13200 | internal/game/drama_milestones.go:118; internal/game/drama_milestones.go:131 |
| 12745 | difference | 5次缺材料通知：召唤券10401需10有0、10403需10有1；另三次10001需400000有293308归培养域。当前只在共鸣缺料路径发送，本地召唤缺券静默。 | 2638,10628,12737,12741,12743 | internal/game/empower.go:254; internal/game/summon.go:261; internal/game/summon.go:283 |
| 14100 | difference | 8次状态：前7次day1/status1，额外领取后重登day1/status2。当前本地已领状态只有1，缺少第二阶段已领2。 | 2387,4593,8502,8839,9343,9414,9418,13082 | internal/game/checkin.go:30; internal/game/checkin.go:68 |
| 14101 | difference | 唯一请求是额外领取：day保持1、status1→2、code1/msg空，10510新增99955×1→12742→确认。当前本地today即拒绝，成功只count+1/status1，无法表达此路径。资格条件需结合购买/charge_sum域，不推断免费可无限重领。 | 5308,9432 | internal/game/checkin.go:94; internal/game/checkin.go:107; internal/game/checkin.go:122 |
| 16400 | difference | 4次均39个阶段性成就；领取后的已完成档由下一档替换，progress.id0。当前本地枚举全部199个表项、progress.id1，列表与状态计算范围不同。 | 2431,4632,8540,13125 | internal/game/feat.go:59; internal/game/feat.go:80; internal/game/feat.go:95 |
| 16401 | difference | 58次增量覆盖培养/招募等进度；领取11010后推下一档11020、领取18060后后续显示18070。当前领奖推已领同档，progress.id1；evt_partner等列表计算未覆盖本批阶段链。 | 2628,2632,8901,8907,8931 | internal/game/feat.go:80; internal/game/feat.go:148; internal/game/feat.go:158 |
| 16402 | difference | 14请求=10成功（空msg）+4重复拒绝奖励已领取；10次合计900000贝里和72001×90。原服资产/背包→通用奖励文字→下一档16401→确认；本地先领取成功→同档16401→资产/全包，重复拒绝文字一致。 | 5127,8902,8932,8933,8939,8940,8946,8947,8953,8954,8960 | internal/game/feat.go:115; internal/game/feat.go:139; internal/game/feat.go:145 |
| 20300 | partial | 5次activity_box均空；领取日常后活跃由5→15，未出现20301。仅空列表形态与本地匹配，宝箱领奖、拒绝和重置未证实。 | 2434,4633,8541,8891,13127 | internal/game/activitybox.go:67; internal/game/activitybox.go:78 |
| 21100 | difference | 4次仅包含day1/status3、type1；当前本地固定输出8天（未达/可领/已领全列）。这里只补列表形态，未出现21101，不能据此改日期/开服天数规则。 | 2383,4586,8494,13078 | internal/game/loginday.go:81; internal/game/loginday.go:128 |
| 23200 | difference | 4次状态：初次must_five_num10，之后0；普通免费剩0且key5给次日05:00；高级剩1即使先前免费已用后重登仍报1。本地must_five_num恒0、全组key5恒0。不能从本批快照矛盾推断实际免费重置周期。 | 2429,4629,8537,13123 | internal/game/summon.go:154; internal/game/summon.go:176; internal/game/summon.go:202 |
| 23201 | difference | 194请求=175成功（173十连+2单抽，共1732英雄）+19独立拒绝；19=16包满12741+2缺券12745+1解锁门槛12741。成功167钻十连/6券十连/1免费/1券单抽；原服扣费/逐英雄11001/资产→23201→23204，本地先23201→聚合11001→资产/全包且拒绝静默。1732个hero-card映射均与本地公式一致，概率/保底未证明。 | 351,353,530,2887,4923,8604,6103,6129,10743,7787,8213 | internal/game/summon.go:216; internal/game/summon.go:222; internal/game/summon.go:243; internal/game/summon.go:288; internal/game/summon.go:322 |
| 23202 | aligned | 免费高级单抽后主动推送group300/free_times0/free_cd_end1793912400（次日05:00），字段及推送条件与本地freeState相符。未捕获跨界重置，23200后续快照仍有差异。 | 8605 | internal/game/summon.go:183; internal/game/summon.go:308 |
| 23204 | unsupported | 173次付费/券十连成功后推recruit_group单组更新；当前本地普通召唤未发送23204，也无其他显式生产路径。 | 2888,2912,2938,12465 | internal/game/summon.go:288; internal/game/summon.go:308 |
| 23214 | difference | 空置换状态partner_id0/new_partner_bid0，但partner_ids=[{id:1}]；本地总为空数组。id1的业务含义本批未证明，不能自动解读为可置换英雄列表。 | 5102,8846 | internal/game/seerchange.go:20; internal/game/seerchange.go:29 |
| 23230 | difference | 两次camp50051/times1/must_count149；本地elitePity110使状态must最大110，无法表达149。其余静态时段/item3686/lucky29400/空reward_list与表面状态相符；无23231抽取，不能完整确认为150保底算法。 | 5104,8847,5332,9459 | internal/game/elite_summon.go:29; internal/game/elite_summon.go:157; internal/game/elite_summon.go:177 |
| 23234 | aligned | 一次打开先23235再23234，均flag1/msg空，与本地显式实现一致。 | 5098,8844 | internal/game/seerpalace.go:141; internal/game/seerpalace.go:149 |
| 23235 | aligned | 对应23234的先行标志通知，flag1/msg空；本批未见登录主动推该号，日界策略不受此样本证明。 | 8843 | internal/game/seerpalace.go:149; internal/game/login_init.go:19 |

## 事务与边界

- summon_accounting：{"request_count": 194, "same_command_success": 175, "successful_ten_pulls": 173, "successful_singles": 2, "hero_results": 1732, "successful_payment_kinds": {"diamond_ten": 167, "ticket_ten": 6, "free_single": 1, "ticket_single": 1}, "refused": {"capacity": 16, "ticket_shortage": 2, "stage_locked": 1}, "diamond_ten_cost": 2000, "diamond_ten_total": 334000, "source_lines": [530, 2875, 2887, 2888, 6129, 10727, 10731, 10743, 12481], "note": "167次钻十连均可由同流资产快照定位-2000；其中6129窗口先夹入另一笔分解的10302，必须用10731/10742的扣费快照而非10727。所有1732个hero card映射与local公式一致，随机分布不作为概率证据。"}
- gift_boxes：{"requests": 9, "success": 9, "hero_choice_total": 140, "hero_choice_quantities": [64, 42, 34], "sign_in_gift_content_matches_table": true, "unsupported_choice_gift_bases": [641236, 88828, 39528, 39179], "source_lines": [228, 2625, 2626, 4308, 4312, 4313, 4317, 4325, 9000, 9005, 9009, 12628, 12700], "note": "args.name1选择物品/英雄与结果相符；args.name2为1/2/3/4/50时最终奖励仍可为1，不能将其泛化为领奖数量。"}
- extra_checkin：{"day_before": 1, "status_before": 1, "day_after": 1, "status_after": 2, "item_base_id": 99955, "gained": 1, "source_lines": [9418, 9430, 9431, 9432, 13082], "note": "首次签到样本不在本批；前面购买/累计金额变化仅相关线索，资格结论交活动域交叉核对。"}
- daily_task：{"task_id": 90003, "diamonds": 20, "activity": 10, "source_lines": [5125, 8893, 8895, 8896]}
- achievement_claims：{"requests": 14, "success": 10, "duplicate_refusals": 4, "coin_reward_total": 900000, "item_base_id": 72001, "item_reward_total": 90, "source_lines": [8902, 8933, 8940, 8947, 8954, 8960]}
- mail_attachment：{"item_base_id": 10001, "amount": 1000000, "before": 293308, "after": 1293308, "source_lines": [13189, 13190, 13191, 13193, 13194, 13195]}
- activity_reward_unboxing：{"source_lines": [8879, 8880, 8881, 8882, 8883, 8884, 8885, 8886, 8887, 8889], "actual_storage3_entities": 4, "outer_box_bases": [99110501, 99110502, 99110503, 99110504], "boxes_observed_in_bag": false, "separate_10515_for_these_boxes": false, "note": "同次领取实际只证实四个新唯一神装实体。随后外层四盒子奖励清单属于展示，不能与已展开实物双算；通用展开机制仍未知。"}

## 未确认

- Actual reset periods and cross-boundary behavior remain unobserved.
- No 10927 or 20301 in this batch: online-gift claim and activity-chest transactions not newly confirmed.
- No elite draw23231, divination23213 or swap23215: probability/pity/free conditions beyond observed states remain unknown.
- Extra signin eligibility must be resolved with the charge/purchase domain; do not enable unlimited repeat claims.
- Per-instance inventory, choice-gift semantics and singleton reward IDs require model-level follow-up, not literal copying of original instance numbers.
- No manual screenshots/action log for this batch; packet facts cannot establish UI text, exact human clicks or universal absence of stalls.
