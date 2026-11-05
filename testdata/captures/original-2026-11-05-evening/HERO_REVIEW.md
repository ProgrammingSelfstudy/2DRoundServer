# 晚间手动抓包：英雄域逐协议核对

核对范围：全部29个110xx命令。当前源码HEAD ddc71aa。未修改代码，未操作手机/网络。

状态只表示捕获分支与本地实现的核对边界，不代表未出现分支或所有字段都已验收。源行指固定完整JSONL的一基行。

| 协议 | c2s/s2c | 状态 | 核对结果 | 原包源行 | 本地来源 |
|---|---|---|---|---|---|
| 11001 | 0/1872 | partial | 1872条新增英雄通知，partners结构可解码；本地也生成英雄通知，但本批属性/战力公式不能视作已对齐，且原服部分新增英雄旁有11095图鉴通知（见11095）。 | 2498,2500,2502 | internal/game/partner.go:114、internal/game/summon.go:296 |
| 11002 | 0/537 | difference | 537条英雄增量。培养成功确认前的11002位置相符；但全部537条均def>0、def_p=def_s=0，本地partnerMessage令def_p/def_s都等于def，是明确字段映射差异；其他属性/战力仍属本地近似模型。 | 2661,2665,2685,4298 | internal/game/partner.go:72、internal/game/partner.go:126、internal/game/partner_growth.go:160 |
| 11003 | 502/502 | difference | 502次均result1/msg空；11次+5级、1次+4级、490次+1级，最高550。逐次核对coin/hero_exp实扣502/502匹配partner_growth表。原服货币升级不发全量包，本地成功后仍sendBackpack，属于多发10500/10501。 | 375,379,2660,2661,2662,2664,2665,2666 | internal/game/partner_growth.go:77、internal/game/partner_growth.go:139、internal/game/partner_growth.go:152、internal/game/partner_growth.go:155、internal/game/partner_growth.go:174 |
| 11004 | 6/6 | difference | 6次突破全成功/空msg，级别30/40/50/60/80/100，break0→6；六次coin与突破卡10001实扣均匹配本地表（卡50/100/200/500/1000/2000）。原服10302→10512→11002→11004；本地仍发全量背包。 | 395,405,419,429,477,4943,2683,2684,2685,2686,8625,8626,8627 | internal/game/partner_growth.go:152、internal/game/partner_growth.go:155、internal/game/partner_growth.go:199 |
| 11005 | 32/32 | difference | 32次：29成功、3失败；其中19次携item_expend成功，覆盖7至25星及20→21的融合材料形式；本地hasItems一律拒绝且拒绝self非目标前一星的融合路径。19次携魂石成功均带空asset_list的12742，本地sendGainList跳过空通知。三次缺卡失败是空msg确认→12745(10001,need400000,num293308)，本地仅文字失败。 | 2331,2337,4290,4295,4296,4297,4298,4302,4303,8431,12707,8459,12736,12737,8463,12740,12741,8465,12742,12743,12981,13200,13204 | internal/game/partner_star.go:20、internal/game/partner_star.go:28、internal/game/partner_star.go:31、internal/game/partner_star.go:168、internal/game/partner_star.go:205、internal/game/drama_milestones.go:131 |
| 11006 | 0/29 | aligned | 29次成功升星各有一条消耗英雄通知；逐笔返回的expend2英雄集合与请求expend1+expend2所选实体集合完全相符（29/29），本地也用实际consumed集合发送该结构。魂石替代支持另见11005。 | 4289,4302,8833 | internal/game/partner_star.go:248、internal/game/partner_star.go:252 |
| 11007 | 0/6 | unsupported | 6条ref_partners批量属性刷新：首条2英雄，后续5条均114英雄；其中4条位于神装穿戴11091之后、11093确认之前。源码未找到11007发送路径；现有共鸣刷新按英雄发11002，神装路径也遗漏此批量刷新。 | 2646,9410,10569,10576,10583,10592 | internal/game/resonance.go:170、internal/game/resonance.go:183、internal/game/holyeqm.go:407 |
| 11009 | 10/11 | difference | 10个扩容请求全部成功，原服每次10302→11009且文字“扩容成功”，buy_num1..10对应容量365..410。此前基线300/0，源9408无本次11009请求的自推已变360/0，额外60来源待核对；本地只有300+5*buy_num、文字“购买成功”且先回后资产，不能直接把初始容量改成360。 | 9406,9407,9408,8218,12486,12487,12489,12506,13097 | internal/game/partner.go:139、internal/game/partner.go:145、internal/game/partner.go:150、internal/game/partner.go:282、internal/game/partner.go:294 |
| 11010 | 3/3 | difference | 3次item_id0一键穿戴都成功/空msg。原服包增量(10511，替换时10510)→11012→11010；本地先回“穿戴成功”，后11012及全量10501，通知顺序、文案及包更新方式不同。 | 373,2657,2658,2659,4935,8613,8614,8615,8616,9489 | internal/game/equipment.go:105、internal/game/equipment.go:123、internal/game/equipment.go:246 |
| 11012 | 0/3 | difference | 3条普通装备穿戴后的英雄属性/eqms增量，均早于11010确认；本地有同号消息但在确认后发送，且def_p/def_s映射和属性模型存在前述差异。 | 2658,8615,9489 | internal/game/equipment.go:105、internal/game/equipment.go:111、internal/game/partner.go:126 |
| 11016 | 12/12 | partial | 12条突破属性预览均可按现有字段解码；本地有下一break步的partnerMessage预览路径。只捕获成功预览，不能据此确认上限/缺英雄/条件失败，属性公式差异仍待建模。 | 391,393,2681,2682 | internal/game/partner_growth.go:85、internal/game/partner_growth.go:99、internal/game/partner.go:72 |
| 11020 | 4/4 | partial | 4次皮肤列表均为空，与本地无皮肤角色的空列表形状一致；本批没有皮肤获取/穿戴/升级，不能确认非空分支及加成。 | 2415,4618,8527,13112 | internal/game/skin.go:76、internal/game/skin.go:82 |
| 11025 | 4/4 | difference | 4次简表包含sort_type/num/buy_num/partners，前三次容量300/0，末次410/10；本地简表形状存在，但仅300+5*buys会在末次给350而非410，未建模额外容量来源；英雄属性/图鉴等也非整体对齐。 | 2402,4605,8513,13097 | internal/game/partner.go:139、internal/game/partner.go:150、internal/game/partner.go:190 |
| 11026 | 9/9 | partial | 9次按partner_ids取英雄详细装备/战技和属性，字段与本地按请求ID选择并编码partnerMessage路径相符；真实属性公式及本批装备参数尚不代表本地数值验收。 | 2413,2635,4616 | internal/game/partner.go:255、internal/game/partner.go:261、internal/game/partner.go:268 |
| 11030 | 2/2 | difference | 两次战技穿戴成功：10511(storage1)→11031→11030，确认空msg。当前本地也空msg且11031在确认前，但先sendBackpack全量刷新，未用10511删除实例增量。 | 4937,4939,8617,8618,8619,8620,8621,8622 | internal/game/artifact.go:411、internal/game/artifact.go:472、internal/game/artifact.go:473、internal/game/artifact.go:479 |
| 11031 | 0/2 | partial | 两条战技穿戴后的属性/artifacts消息，本地有sendHero的对应结构；其先于11030确认已一致，但其前背包增量与属性公式未对齐，见11030/11002。 | 8618,8621 | internal/game/artifact.go:476、internal/game/partner.go:114 |
| 11037 | 4/4 | partial | 4次战技幸运值查询均lucky空，本地读取ArtifactsOf.Lucky形状对应；本包没有11036合成事务，不能确认合成消耗/成功率/非空幸运值。 | 2410,4623,8521,13117 | internal/game/artifact.go:735、internal/game/artifact.go:744 |
| 11040 | 4/4 | unsupported | 图鉴快照4条，all_star为15/877/877/1243，partners图鉴条目3/61/61/67；本地仍固定all_star0/partners空的stub，图鉴积累/属性效果未实现。 | 2409,4612,8520,13105 | internal/game/hero_stubs.go:40、internal/game/hero_stubs.go:48 |
| 11055 | 1760/0 | difference | 1760个红点写入请求（is_point1有1759次、0有1次），本包无任何同号响应。本地fixed空消息会每次回11055且不保存值，是应答/状态行为差异；本批不足以确认红点重置或业务触发规则。 | 230,232,234 | internal/game/hero_stubs.go:11、internal/game/hero_stubs.go:18、internal/game/hero_stubs.go:46 |
| 11056 | 4/4 | difference | 4次融合红点查询全is_point1，本地固定is_point0；仅结构可解码，不是业务对齐。 | 2451,4649,8557,13144 | internal/game/hero_stubs.go:47 |
| 11067 | 3/3 | partial | 3次重生状态查询均day_num0/end_time0，本地无使用记录时返回相同结构；本批无11068，不确认每天次数/冷却/重生奖励。 | 377,2663,4696,8612 | internal/game/reset.go:95、internal/game/reset.go:106 |
| 11075 | 2/2 | difference | 两次预览分别选100名1级英雄(99三星+1四星)和14名1级四星；奖励分别11/10001/22=1015/1015/50500与350/350/14000。当前refund仅累计培养/装备成本，1级无培养时不给这些星级基础献祭奖励，预览将漏发。 | 5167,8974,5175,8989 | internal/game/reset.go:20、internal/game/reset.go:34、internal/game/reset.go:43、internal/game/reset.go:198、internal/game/reset.go:210 |
| 11076 | 162/162 | difference | 162次均有响应：132成功/空msg、30失败“找不到该英雄”。132次成功累计献祭1346名1级英雄(1108三星、238四星)，全部奖励吻合三星=魂11×10+卡10001×10+经验22×500、四星=25/25/1000。原服10302→10512→12742→(偶有任务/活动通知)→空msg11076；本地缺星级基础奖励和12742，发全量包并回“献祭成功”，不存在英雄文案也不同。 | 576,2920,2921,2922,2924,602,2942,2943,2944,2945,732,3060,5167,8974,5175,8989 | internal/game/reset.go:23、internal/game/reset.go:179、internal/game/reset.go:212、internal/game/reset.go:232、internal/game/reset.go:235、internal/game/reset.go:242 |
| 11086 | 1/1 | difference | 1条英雄高级总属性：dam_s12/res_s17等非零，而本地11086直接编码partnerMessage，未给dam/res/cure等高级字段赋值，这些字段默认0；本地数值/效果仍不完整。 | 6080,10595 | internal/game/holyeqm.go:649、internal/game/holyeqm.go:660、internal/game/partner.go:126 |
| 11091 | 0/4 | difference | 4条神装穿戴后属性/holy_eqm增量，原服res50及后续res_s17等；本地同号结构存在，但partnerMessage未填这些高级属性，且后续批量11007遗漏。 | 10566,10573,10580,10588 | internal/game/holyeqm.go:414、internal/game/partner.go:126 |
| 11092 | 4/4 | partial | 4次按请求英雄列表读取holy_eqm，空/穿戴实例结构能解码，本地逐请求ID生成相同形状。真实实例身份和全部装备属性仍需本地真机验收。 | 2419,4622,8531,13116 | internal/game/holyeqm.go:631、internal/game/holyeqm.go:636、internal/game/holyeqm.go:646 |
| 11093 | 4/4 | difference | 4次神装穿戴都type1、result1/msg空，原服10511→11091→11007(114英雄)→11093，无10306（全包也未出现10306）。本地增量/空msg已部分对齐，但缺11007且确认后固定sendRolePower，多发10306。没有本批卸下样本。 | 6070,6072,6074,6078,10565,10566,10569,10570,10572,10573,10576,10577,10579,10580,10583,10584,10587,10588,10592,10593 | internal/game/holyeqm.go:407、internal/game/holyeqm.go:414、internal/game/holyeqm.go:417、internal/game/holyeqm.go:420 |
| 11095 | 0/140 | unsupported | 140条图鉴/英雄获取提示，含partner_bid/init_star/status/is_chips，常伴高星新增11001。源码没有找到此号发送路径，11040仍stub；只确认捕获字段和关联，不能由此断言所有触发/去重规则。 | 2497,2498,2499,2500,12697 | internal/game/hero_stubs.go:40、internal/game/hero_stubs.go:48、internal/game/partner.go:197 |
| 11099 | 16/16 | partial | 16次查询合计21个英雄的dower_skill均为空；本地按请求heroIDs返回talentMessages结构一致，但无学习/升级/非空天赋样本，不能确认天赋加成（本地注释仍未加到战斗属性）。 | 2436,2655 | internal/game/talent.go:78、internal/game/talent.go:82、internal/game/talent.go:98 |

## 最终升星结果

本批最终已成功到25星。22:39:49、22:39:57、22:39:59的三次失败均伴随12745：10001需要400000、当时持有293308，提示的是突破卡不足，没有提示英雄或魂石搭配错误。用户随后补足突破卡，22:40:43的最后一次11005返回result1；三次中间失败不能记成最终升星失败。对应完整解码源行12736/12737、12740/12741、12742/12743和13204。

本地对魂石及高星融合的拒绝属于实现缺口，不能归因于用户选错材料。

## 关键边界

- No current UI/action screenshots; semantic operation labels follow protocol/source, not observed clicks
- Four game streams are kept separate; source_line is original grouped JSONL line, not global wire order
- Cost verification uses per-stream request/response FIFO plus server hero state/asset order to avoid equal-timestamp misattribution
- Capacity extra60 appears near purchase/role refresh events; exact entitlement cause unconfirmed
- Only level1 3/4-star offering base rewards proven; no extrapolation to other stars, levelled heroes or locked heroes
- No skin action,11068 rebirth,11036 artifact compose or holy-unwear in this batch
- Attribute and power formulas, all high-star/fusion conditions, failure variants and server/client acceptance remain incomplete
- Ordinary local tool evidence only; no REA MCP Evidence and no code modifications
