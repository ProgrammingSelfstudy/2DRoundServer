# 战技（artifact，11030~11040、11048、11060）实现设计（2026-10-28，还没写代码）

读过客户端 `hero/hero_controller.lua`、`hero/view/artifact/*`、`partner_artifact_data.lua`、`artifact_summon_data.lua` 后的结论。

## 客户端怎么用
- 战技是物品表里 type 10（`ARTIFACTCHIPS`）的物品，基础编号 10451~10457（七个品阶），在 10500 背包里**一件一条**，带 `extra`（技能）、`extra_attr`、`attr`、`enchant`（星级）。强化训练 = 10450，重铸石 10430（紫）/10431（橙），幸运宝珠 10470。
- `extra`：`extra_k` 1、2、8 是当前技能，3、4、9 是重铸后待保存的新技能（`extra_v` 是技能编号）。`extra_attr` 是随机属性（`attr_id`/`attr_val`），`attr` 是星级加成。
- 每个英雄 2 个位置（`artifact_pos` 1、2）。11030：`type` 1 穿到 `pos_id`，0 卸下；11031 回英雄的属性加战技列表（`artifacts` 的字段同背包）；11012 的 `artifacts` 也要带。
- 11035 分解（按 `data_artifact_resolve` 给强化训练）、11036 合成（`data_artifact_compound[目标][材料数]` 有成功率 `rate`、失败 `award`、熔炼值 `add_num`；熔炼值满 1000 在 11038 换 `change_gift` 的 10454 一个，11037 回当前熔炼值）、11033 重铸（消耗 `ref_expend`，锁定的技能 `skills` 保留，`luck_item` 幸运宝珠）、11034 保存或放弃重铸、11032 升星（`expends` 是材料战技）。11048 回各类型的重铸计数 `current`/`limit`（保底：同类重铸到次数必出稀有技能）。
- 各品阶的属性数 `attr_num`、技能数 `skill_num` 和技能等级组合在 `data_artifact_data` 的 `skill_desc`（文字），技能池按等级在 `data_artifact_skill[1..4]`，属性数值在 `data_artifact_attr`。

## 要动的地方（比一个协议模块大）
1. **每件的存储**：现在背包是“物品编号 → 数量”的账本，没有每件的属性。方案：账本照旧记数量（卖、发奖励都不用改），`module_state "artifact"` 里按件存 `{star, attr, skills, pending, worn}`；一件的编号 = `base*10000 + 序号`；打开背包时和账本对齐（账本多了就随机生成新件，少了就删最大序号的）。
2. **10500 列表**：`backpackItems` 里 type 10 的物品改成逐件输出。`sell.go` 目前把编号当基础编号，战技的件编号要换算。
3. **穿戴进属性**：`partnerStats(p store.Partner)` 只看 `Partner`，没有角色上下文。要么给 `store.Partner` 加 `Artifacts` 字段（`PartnersOf`、`PartnerByID` 等 4 处查询要带上），要么把加成放进 `Partner` 的一个 `ExtraAttr`。战斗用的是同一个函数，所以接一次两边都对。战技的技能要进战斗脚本（`fight.go` 的被动技能），这一步另说。
4. **未确认**：升星（11032）的星级规则表里没有（`next_attr_star`/`next_skill_star` 全是 0）；重铸技能概率、保底次数、幸运重铸加成表里只有技能池和 `lucky_artifact_add_pro` 1500；技能的出现概率没有表。这些都得“我们定”并记进 `OPEN_QUESTIONS.md`。

## 神装、魂器、共鸣赋能
同样是“每件带随机属性”的物品，等 1~3 做完后照这套存储做（神装 `holy_eqm`、魂器 `horcrux`）。
