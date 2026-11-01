# 后台数据覆盖注册表（更新至阶段 1）

本清单以仓库内 [ADMIN_COMPLETION_PLAN.md](ADMIN_COMPLETION_PLAN.md) 为执行计划，审查基线为 `24c5b765db14f1a0acb1e5507ddbf7c22da6d12d`。阶段0建立清单与完整性门禁；阶段1实现了107项运行时注册、玩家侧只读模块API及未启用白名单的重置机制，审查基线为 `ef272cef41dbe21a4c574f13cdc5516ae47f20da`。所有模块 `Editable=false`、`Resets=[]`，没有PUT编辑、全服数据接口、前端页签或结算入口。

## 口径与生成

- `role`：`LoadState/SaveState` 的模块参数；`global`：`LoadGlobal/SaveGlobal` 的模块参数。相同 key 在两个范围出现须分别登记，不按 key 去重。
- 源码扫描到 **343 处持久化调用、77 项玩家模块、30 项全服模块**；其中三项为动态前缀，代表整族模块而不是一个已存在的数据库行。另检查 85 个以 `Module`/`Mod` 结尾的字符串模块常量。计划中的“约65/31”是估计，不能当验收数量。
- `table`：92 张显式建表，来自 `internal/store/migrations/*.sql` 和 `store.go` 的启动建表；包括游戏相关表及4张安全/运维表 `admins`、`audit_logs`、`login_tokens`、`schema_migrations`，后者也列出以说明边界。SQLite 自动维护的 `sqlite_sequence` 等内部表不由项目注册，不计入。
- 下表的范围、键和源码列由 `tools/admincoverage` 扫描结果生成；中文窗口、当前能力、后续处置和原因经人工补充。窗口协议用于定位游戏功能，不等于后台 HTTP 路由。**“当前未接”是待补缺口，不是永久排除；“拟提供”不表示已经实现或已经授权具体写操作。**
- 本文机器可读表格是覆盖清单；阶段1已在各模块文件的 `init()` 中注册到 `internal/game/adminreg.go` 的 `AdminModules()`。完整性门禁同时核对源码扫描、本文及运行时注册，任一边缺项都失败。测试采用精确的 `(scope,key)` 对比，不能用一个任意 `*` 冒充全覆盖。

复现范围（命令不打开数据库，也不读写抓包）：

```sh
go run ./tools/admincoverage > /tmp/admin-coverage-inventory.json
go test ./internal/admincoverage ./internal/game -run 'Test(AdminCoverage|Scan|Registry)' -v
```

`TestAdminCoverageRegistryComplete` 扫描 `internal/game` 和 `internal/store` 非测试 Go AST（包括战技、神装、魂器、赋能的store封装），解析常量/别名、字面量、前缀拼接、返回前缀的函数及按接收者类型区分的模块字段。未能解析的新动态表达式直接失败，必须扩展扫描器；新增模块常量、调用、表、错误范围、缺项/过期项、缺失说明和不一致源码链接都会触发门禁。常量命名约定之外的模块仍由持久化调用检查。扫描器按包隔离常量和函数名字；单测覆盖store封装、局部常量遮蔽、新增常量/字面量/表、前缀、共用字段、错误文档和无法解析的表达式。

动态键的具体边界：

- `firstcharge_*`：`firstcharge_first`、`firstcharge_first_new`、`firstcharge_first_lwt`、`firstcharge_first_3`，来自 `firstSets`；后续新增窗口仍受同前缀登记覆盖，视图须验证真实窗口 key。
- `guildmarket:*`：`guildmarket:` 后为公会GID；`monopoly_g*`：`monopoly_g` 后为公会GID。它们写在全服状态容器中，不能当普通区服唯一文档或玩家模块处理。
- `champSvc.module` 当前有 `arenachamp` 和 `crosschamp` 两种赋值，两个 key 都有玩家/全服状态，已拆四行；`peakformations` 是另一个角色阵容字面量，不能因巅峰赛当前关闭而漏掉。

## 当前 HTTP 与权限边界

核对来源：[实际路由](../internal/admin/server.go)、[角色详情](../internal/admin/handlers.go)、[支付接口](../internal/admin/pay.go)、[新前端路由](../web/admin/src/routers/index.ts)、[角色详情界面](../web/admin/src/views/roles/RoleDialog.vue)。

- 现有 `read/write/manage` 对应 viewer/operator/superadmin；管理员、审计、备份读取仅 manage，支付写入及区服/管理员维护也仅 manage。原有业务权限不在本阶段改变。
- **阶段1已提供77项玩家模块的通用只读API，107项均可查询注册元数据；30项全服模块的数据读取仍留待阶段3。** 活动配置 `/api/activities` 不是角色 `activity` JSON；充值确认按规则更新状态，也不等于能查看/重置首充、月卡或限购。角色已有状态列表会标注未知遗留key；未知key详情返回404，已注册但尚无状态的key返回exists=false/data=null，不创建行。所有专用View/Rank暂未绑定。
- 当前角色详情仅基本资料、在线/封禁、资产和物品账本；已有发英雄、皮肤、外观按钮，但没有拥有列表、英雄属性/装备/阵容、任务/好友、收件及角色订单汇总。全局订单和发件历史也不能当作这些角色页签。
- 现有写操作并非全部执行新计划的“先查离线、不可达不写”；本阶段不更改旧接口或规则。后续新增玩家数据写必须先网关在线校验（在线409、未知502），再读before、写入、审计。不能用当前踢线选项代替这一前置条件。
- 全服数据后续必须经网关，在所属服务的锁/定时器边界内读取/修改；禁止后台直接调用 `SaveGlobal`。专用表若同样受网关服务锁保护，也不能因其是SQL表就绕开该边界。

## 玩家模块（77项）

当前阶段1已开放原始JSON只读，GET不调用可能写库的游戏load/view/roll。重置路由仅superadmin可调用；先校验原因和网关在线状态，再查显式白名单。当前全部白名单为空，item=all没有特殊越权含义；没有任何生产模块可实际重置或编辑。

| 范围 | 模块键 | 中文名/对应窗口 | 当前后台能力 | 后续拟提供 | 缺口或限制原因 | 源码依据 |
| --- | --- | --- | --- | --- | --- | --- |
| role | `activity` | 活动个人目标/领取（16601–16620、16687） | 原始JSON查看（read）；无可用重置/编辑 | 查看（阶段1/5）；重置/编辑待白名单审定 | 缺模块API；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [activity.go](../internal/game/activity.go) |
| role | `activitybox` | 日常活跃宝箱（20300/20301） | 原始JSON查看（read）；无可用重置/编辑 | 查看（阶段1/5）；重置/编辑待白名单审定 | 缺模块API；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [activitybox.go](../internal/game/activitybox.go) |
| role | `adventure` | 空岛冒险（20600–20636） | 原始JSON查看（read）；无可用重置/编辑 | 查看（阶段1/5）；重置/编辑待白名单审定 | 缺模块API；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [adventure.go](../internal/game/adventure.go)、[adventure_mine.go](../internal/game/adventure_mine.go) |
| role | `adventure_mine` | 空岛灵矿个人状态（20640–20660） | 原始JSON查看（read）；无可用重置/编辑 | 查看（阶段1/5）；重置/编辑待白名单审定 | 缺模块API；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [adventure_mine.go](../internal/game/adventure_mine.go) |
| role | `animate` | 元宵花灯/厨房（24801–24809） | 原始JSON查看（read）；无可用重置/编辑 | 查看（阶段1/5）；重置/编辑待白名单审定 | 缺模块API；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [animate.go](../internal/game/animate.go) |
| role | `arenachamp` | 本服冠军赛（20250–20282） | 原始JSON查看（read）；无可用重置/编辑 | 查看（阶段1/5）；重置/编辑待白名单审定 | 缺模块API；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [arenachampion.go](../internal/game/arenachampion.go)、[arenachampion_view.go](../internal/game/arenachampion_view.go) |
| role | `arenateamrole` | 组队竞技场个人状态（27200–27256） | 原始JSON查看（read）；无可用重置/编辑 | 查看（阶段1/5）；重置/编辑待白名单审定 | 缺模块API；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [arenateam.go](../internal/game/arenateam.go) |
| role | `artifact` | 战技实例/重铸/穿戴（11030–11036） | 原始JSON查看（read）；无可用重置/编辑 | 查看（阶段1/5）；重置/编辑待白名单审定 | 缺模块API；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [artifact.go](../internal/store/artifact.go) |
| role | `chargeacc` | 累计充值奖励（16700、16712/16713） | 原始JSON查看（read）；无可用重置/编辑 | 查看、充值专用视图；不提供任意编辑累计/领取 | 缺独立管理入口；累计充值和已发奖事实不能靠JSON编辑改写 | [charge_proto.go](../internal/game/charge_proto.go)、[chargeacc.go](../internal/game/chargeacc.go) |
| role | `crosschamp` | 跨服冠军赛（26200–26215） | 原始JSON查看（read）；无可用重置/编辑 | 查看（阶段1/5）；重置/编辑待白名单审定 | 缺模块API；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [arenachampion.go](../internal/game/arenachampion.go)、[arenachampion_view.go](../internal/game/arenachampion_view.go) |
| role | `cyclegift` | 每周福利/月度礼包限购（21007） | 原始JSON查看（read）；无可用重置/编辑 | 查看、充值专用视图、白名单重置（阶段1/2/5） | 缺首充/卡/限购状态视图；重置须保留已付基数并防止重复发奖 | [cyclegift.go](../internal/game/cyclegift.go) |
| role | `daycharge` | 每日首充（21010/21011） | 原始JSON查看（read）；无可用重置/编辑 | 查看、充值专用视图、白名单重置（阶段1/2/5） | 缺首充/卡/限购状态视图；重置须保留已付基数并防止重复发奖 | [daycharge.go](../internal/game/daycharge.go) |
| role | `daysrank` | 七天排行（22700–22704） | 原始JSON查看（read）；无可用重置/编辑 | 查看（阶段1/5）；重置/编辑待白名单审定 | 缺模块API；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [daysrank.go](../internal/game/daysrank.go) |
| role | `dial` | 转盘抽奖/记录（16670–16676） | 原始JSON查看（read）；无可用重置/编辑 | 查看（阶段1/5）；重置/编辑待白名单审定 | 缺模块API；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [dial.go](../internal/game/dial.go) |
| role | `element` | 元素圣殿/领地挑战（25000–25006） | 原始JSON查看（read）；无可用重置/编辑 | 查看（阶段1/5）；重置/编辑待白名单审定 | 缺模块API；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [element.go](../internal/game/element.go) |
| role | `elfin` | 精灵/古树/队伍（26500–26564） | 原始JSON查看（read）；无可用重置/编辑 | 查看（阶段1/5）；重置/编辑待白名单审定 | 缺模块API；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [elfin.go](../internal/game/elfin.go) |
| role | `elitematch` | 精英段位赛（24900–24946） | 原始JSON查看（read）；无可用重置/编辑 | 查看（阶段1/5）；重置/编辑待白名单审定 | 缺模块API；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [elitematch.go](../internal/game/elitematch.go)、[elitematch_season.go](../internal/game/elitematch_season.go) |
| role | `elitepass` | 段位赛令牌活动（24952–24955） | 原始JSON查看（read）；无可用重置/编辑 | 查看（阶段1/5）；重置/编辑待白名单审定 | 缺模块API；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [elitepass.go](../internal/game/elitepass.go) |
| role | `emoguoshi` | 恶魔果实图鉴（31101–31130） | 原始JSON查看（read）；无可用重置/编辑 | 查看（阶段1/5）；重置/编辑待白名单审定 | 缺模块API；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [emoguoshi.go](../internal/game/emoguoshi.go) |
| role | `empower` | 共鸣赋能（26420–26424） | 原始JSON查看（read）；无可用重置/编辑 | 查看（阶段1/5）；重置/编辑待白名单审定 | 缺模块API；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [horcrux.go](../internal/store/horcrux.go) |
| role | `encounter` | 英雄物语（27100–27104） | 原始JSON查看（read）；无可用重置/编辑 | 查看（阶段1/5）；重置/编辑待白名单审定 | 缺模块API；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [encounter.go](../internal/game/encounter.go) |
| role | `endless_trail` | 无尽试炼个人进度（23900–23913） | 原始JSON查看（read）；无可用重置/编辑 | 查看（阶段1/5）；重置/编辑待白名单审定 | 缺模块API；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [daysrank.go](../internal/game/daysrank.go)、[endless.go](../internal/game/endless.go)、[endless_run.go](../internal/game/endless_run.go) |
| role | `escort` | 护送个人状态（24000–24020） | 原始JSON查看（read）；无可用重置/编辑 | 查看（阶段1/5）；重置/编辑待白名单审定 | 缺模块API；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [escort.go](../internal/game/escort.go) |
| role | `firstcharge_*` | 四组首充窗口（21000/21001、21012–21014、21030–21033） | 原始JSON查看（read）；无可用重置/编辑 | 查看、充值专用视图、白名单重置（阶段1/2/5） | 缺首充/卡/限购状态视图；重置须保留已付基数并防止重复发奖 | [firstcharge.go](../internal/game/firstcharge.go) |
| role | `fortunebag` | 福袋翻牌（28300–28308） | 原始JSON查看（read）；无可用重置/编辑 | 查看（阶段1/5）；重置/编辑待白名单审定 | 缺模块API；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [fortunebag.go](../internal/game/fortunebag.go) |
| role | `growway` | 成长之路（25830/25832） | 原始JSON查看（read）；无可用重置/编辑 | 查看（阶段1/5）；重置/编辑待白名单审定 | 缺模块API；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [growway.go](../internal/game/growway.go) |
| role | `guildmarket` | 公会宝库个人凭证（26900–26903） | 原始JSON查看（read）；无可用重置/编辑 | 查看（阶段1/5）；重置/编辑待白名单审定 | 缺模块API；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [guildmarket.go](../internal/game/guildmarket.go) |
| role | `halidom` | 元素信念（22200–22205） | 原始JSON查看（read）；无可用重置/编辑 | 查看（阶段1/5）；重置/编辑待白名单审定 | 缺模块API；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [halidom.go](../internal/game/halidom.go) |
| role | `hallows` | 神技（24100–24135） | 原始JSON查看（read）；无可用重置/编辑 | 查看（阶段1/5）；重置/编辑待白名单审定 | 缺模块API；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [hallows.go](../internal/game/hallows.go) |
| role | `heaven` | 天界（25200–25232） | 原始JSON查看（read）；无可用重置/编辑 | 查看（阶段1/5）；重置/编辑待白名单审定 | 缺模块API；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [heaven.go](../internal/game/heaven.go) |
| role | `heroexpedit` | 英雄远征（24400–24415） | 原始JSON查看（read）；无可用重置/编辑 | 查看（阶段1/5）；重置/编辑待白名单审定 | 缺模块API；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [expedition.go](../internal/game/expedition.go)、[expedition_help.go](../internal/game/expedition_help.go) |
| role | `holelite` | 节日精英召唤/新春赐福（23220–23222） | 原始JSON查看（read）；无可用重置/编辑 | 查看（阶段1/5）；重置/编辑待白名单审定 | 缺模块API；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [holiday_elite.go](../internal/game/holiday_elite.go) |
| role | `holyeqm` | 神装实例/洗练/方案（11086–11094、25220–25224） | 原始JSON查看（read）；无可用重置/编辑 | 查看（阶段1/5）；重置/编辑待白名单审定 | 缺模块API；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [holyeqm.go](../internal/store/holyeqm.go) |
| role | `homepet` | 家园宠物旅行（26100–26113） | 原始JSON查看（read）；无可用重置/编辑 | 查看（阶段1/5）；重置/编辑待白名单审定 | 缺模块API；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [homepet.go](../internal/game/homepet.go) |
| role | `homeworld` | 家园个人状态（26001–26021） | 原始JSON查看（read）；无可用重置/编辑 | 查看（阶段1/5）；重置/编辑待白名单审定 | 缺模块API；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [homeworld.go](../internal/game/homeworld.go)、[yearmonster.go](../internal/game/yearmonster.go) |
| role | `horcrux` | 魂器觉醒/强化（11402–11405） | 原始JSON查看（read）；无可用重置/编辑 | 查看（阶段1/5）；重置/编辑待白名单审定 | 缺模块API；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [horcrux.go](../internal/store/horcrux.go) |
| role | `ladder` | 天梯（24300–24318） | 原始JSON查看（read）；无可用重置/编辑 | 查看（阶段1/5）；重置/编辑待白名单审定 | 缺模块API；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [ladder.go](../internal/game/ladder.go) |
| role | `levgift` | 升级礼包/全服限量（21200/21201） | 原始JSON查看（read）；无可用重置/编辑 | 查看（阶段1/5）；重置/编辑待白名单审定 | 缺模块API；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [levgift.go](../internal/game/levgift.go) |
| role | `limitexercise` | 试炼之境（25410–25414） | 原始JSON查看（read）；无可用重置/编辑 | 查看（阶段1/5）；重置/编辑待白名单审定 | 缺模块API；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [limitexercise.go](../internal/game/limitexercise.go) |
| role | `limitgift` | 养成/等级限时礼包（21210/21211） | 原始JSON查看（read）；无可用重置/编辑 | 查看、充值专用视图、白名单重置（阶段1/2/5） | 缺首充/卡/限购状态视图；重置须保留已付基数并防止重复发奖 | [limitgift.go](../internal/game/limitgift.go) |
| role | `loginday` | 登录天数/登录奖励（10922–10925、21100/21101） | 原始JSON查看（read）；无可用重置/编辑 | 查看（阶段1/5）；重置/编辑待白名单审定 | 缺模块API；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [loginday.go](../internal/game/loginday.go) |
| role | `luckydog` | 幸运锦鲤（28400–28403） | 原始JSON查看（read）；无可用重置/编辑 | 查看（阶段1/5）；重置/编辑待白名单审定 | 缺模块API；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [luckydog.go](../internal/game/luckydog.go) |
| role | `mergegoal` | 合服目标（27300/27301） | 原始JSON查看（read）；无可用重置/编辑 | 查看（阶段1/5）；重置/编辑待白名单审定 | 缺模块API；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [mergegoal.go](../internal/game/mergegoal.go) |
| role | `monopoly` | 和之国火祭个人状态（27400–27506） | 原始JSON查看（read）；无可用重置/编辑 | 查看（阶段1/5）；重置/编辑待白名单审定 | 缺模块API；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [monopoly.go](../internal/game/monopoly.go) |
| role | `monthcard` | 两档月卡（16705–16708） | 原始JSON查看（read）；无可用重置/编辑 | 查看、充值专用视图、白名单重置（阶段1/2/5） | 缺首充/卡/限购状态视图；重置须保留已付基数并防止重复发奖 | [monthcard.go](../internal/game/monthcard.go)、[recruithero.go](../internal/game/recruithero.go) |
| role | `monthfund` | 两档月基金（24700–24702） | 原始JSON查看（read）；无可用重置/编辑 | 查看、充值专用视图、白名单重置（阶段1/2/5） | 缺首充/卡/限购状态视图；重置须保留已付基数并防止重复发奖 | [charge_proto.go](../internal/game/charge_proto.go)、[monthfund.go](../internal/game/monthfund.go) |
| role | `newwarorder` | 海贼夺宝（28700–28708） | 原始JSON查看（read）；无可用重置/编辑 | 查看（阶段1/5）；重置/编辑待白名单审定 | 缺模块API；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [newwarorder.go](../internal/game/newwarorder.go) |
| role | `onecentgift` | 一元/一分钱礼包（16651/16652） | 原始JSON查看（read）；无可用重置/编辑 | 查看（阶段1/5）；重置/编辑待白名单审定 | 缺模块API；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [onecentgift.go](../internal/game/onecentgift.go) |
| role | `peakformations` | 巅峰冠军赛三队阵容（27725/27726） | 原始JSON查看（read）；无可用重置/编辑 | 查看（阶段1/5）；重置/编辑待白名单审定 | 存在独立保存状态，赛事关闭不等于无数据；不启用未实现赛程 | [cross_closed.go](../internal/game/cross_closed.go) |
| role | `petard` | 花火大会（27000–27008） | 原始JSON查看（read）；无可用重置/编辑 | 查看（阶段1/5）；重置/编辑待白名单审定 | 缺模块API；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [petard.go](../internal/game/petard.go) |
| role | `planes` | 秘境（23100–23123） | 原始JSON查看（read）；无可用重置/编辑 | 查看（阶段1/5）；重置/编辑待白名单审定 | 缺模块API；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [planes.go](../internal/game/planes.go) |
| role | `planesafk` | 长环岛冒险（28600–28626） | 原始JSON查看（read）；无可用重置/编辑 | 查看（阶段1/5）；重置/编辑待白名单审定 | 缺模块API；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [planesafk.go](../internal/game/planesafk.go) |
| role | `practisetower` | 兔丼监狱个人进度（29100–29107） | 原始JSON查看（read）；无可用重置/编辑 | 查看（阶段1/5）；重置/编辑待白名单审定 | 缺模块API；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [practisetower.go](../internal/game/practisetower.go) |
| role | `presage` | 预知召唤/松果宝藏（16690–16694） | 原始JSON查看（read）；无可用重置/编辑 | 查看（阶段1/5）；重置/编辑待白名单审定 | 缺模块API；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [presage.go](../internal/game/presage.go) |
| role | `primus` | 霸气修炼（20700–20706） | 原始JSON查看（read）；无可用重置/编辑 | 查看（阶段1/5）；重置/编辑待白名单审定 | 缺模块API；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [primus.go](../internal/game/primus.go) |
| role | `recruithero` | 新人招募（25100/25101） | 原始JSON查看（read）；无可用重置/编辑 | 查看（阶段1/5）；重置/编辑待白名单审定 | 缺模块API；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [recruithero.go](../internal/game/recruithero.go) |
| role | `redbagday` | 公会红包每日领取（13534–13546） | 原始JSON查看（read）；无可用重置/编辑 | 查看（阶段1/5）；重置/编辑待白名单审定 | 缺模块API；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [guild_redbag.go](../internal/game/guild_redbag.go) |
| role | `returnaction` | 回归活动（27900–27915） | 原始JSON查看（read）；无可用重置/编辑 | 查看（阶段1/5）；重置/编辑待白名单审定 | 缺模块API；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [returnaction.go](../internal/game/returnaction.go) |
| role | `sandybeach` | 沙滩Boss（25400–25405） | 原始JSON查看（read）；无可用重置/编辑 | 查看（阶段1/5）；重置/编辑待白名单审定 | 缺模块API；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [sandybeach.go](../internal/game/sandybeach.go) |
| role | `sevengoal` | 七日目标（13604–13609） | 原始JSON查看（read）；无可用重置/编辑 | 查看（阶段1/5）；重置/编辑待白名单审定 | 缺模块API；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [sevengoal.go](../internal/game/sevengoal.go) |
| role | `skinlottery` | 世界征兵（26600–26602） | 原始JSON查看（read）；无可用重置/编辑 | 查看（阶段1/5）；重置/编辑待白名单审定 | 缺模块API；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [skinlottery.go](../internal/game/skinlottery.go) |
| role | `smashegg` | 砸金蛋个人状态（16680–16685） | 原始JSON查看（read）；无可用重置/编辑 | 查看（阶段1/5）；重置/编辑待白名单审定 | 缺模块API；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [smashegg.go](../internal/game/smashegg.go) |
| role | `snatch` | 节日夺宝（25700–25704、26300/26301） | 原始JSON查看（read）；无可用重置/编辑 | 查看（阶段1/5）；重置/编辑待白名单审定 | 缺模块API；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [snatch.go](../internal/game/snatch.go) |
| role | `stoneshop` | 神秘商店（16688/16689） | 原始JSON查看（read）；无可用重置/编辑 | 查看（阶段1/5）；重置/编辑待白名单审定 | 缺模块API；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [stoneshop.go](../internal/game/stoneshop.go) |
| role | `sweet` | 甜蜜大作战（28500–28502） | 原始JSON查看（read）；无可用重置/编辑 | 查看（阶段1/5）；重置/编辑待白名单审定 | 缺模块API；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [sweet.go](../internal/game/sweet.go) |
| role | `termbegins` | 开学季（26700–26712） | 原始JSON查看（read）；无可用重置/编辑 | 查看（阶段1/5）；重置/编辑待白名单审定 | 缺模块API；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [termbegins.go](../internal/game/termbegins.go) |
| role | `timecollect` | 定时领取（16697/16698） | 原始JSON查看（read）；无可用重置/编辑 | 查看（阶段1/5）；重置/编辑待白名单审定 | 缺模块API；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [levgift.go](../internal/game/levgift.go) |
| role | `timesummon` | 限定召唤（23216–23219） | 原始JSON查看（read）；无可用重置/编辑 | 查看（阶段1/5）；重置/编辑待白名单审定 | 缺模块API；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [time_summon.go](../internal/game/time_summon.go) |
| role | `titlelev` | 称号合成等级（23304） | 原始JSON查看（read）；无可用重置/编辑 | 查看（阶段1/5）；重置/编辑待白名单审定 | 缺模块API；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [titlecompose.go](../internal/game/titlecompose.go) |
| role | `touchgold` | 点金手（23606/23607） | 原始JSON查看（read）；无可用重置/编辑 | 查看（阶段1/5）；重置/编辑待白名单审定 | 缺模块API；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [touchgold.go](../internal/game/touchgold.go) |
| role | `trainingcamp` | 训练营（27600–27603） | 原始JSON查看（read）；无可用重置/编辑 | 查看（阶段1/5）；重置/编辑待白名单审定 | 缺模块API；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [trainingcamp.go](../internal/game/trainingcamp.go) |
| role | `treasure` | 幸运/高级探宝（16637–16643） | 原始JSON查看（read）；无可用重置/编辑 | 查看（阶段1/5）；重置/编辑待白名单审定 | 缺模块API；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [daysrank.go](../internal/game/daysrank.go)、[treasure.go](../internal/game/treasure.go) |
| role | `vedio` | 录像个人收藏/点赞（19901–19908、20034/20036） | 原始JSON查看（read）；无可用重置/编辑 | 查看（阶段1/5）；重置/编辑待白名单审定 | 缺模块API；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [vedio.go](../internal/game/vedio.go) |
| role | `voyage` | 远航订单（23800–23806） | 原始JSON查看（read）；无可用重置/编辑 | 查看（阶段1/5）；重置/编辑待白名单审定 | 缺模块API；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [voyage.go](../internal/game/voyage.go) |
| role | `warorder` | 战令（25300–25309） | 原始JSON查看（read）；无可用重置/编辑 | 查看（阶段1/5）；重置/编辑待白名单审定 | 缺模块API；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [warorder.go](../internal/game/warorder.go) |
| role | `whiteday` | 女神试炼（28800–28803） | 原始JSON查看（read）；无可用重置/编辑 | 查看（阶段1/5）；重置/编辑待白名单审定 | 缺模块API；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [whiteday.go](../internal/game/whiteday.go) |
| role | `yearmonster` | 年兽活动（28200–28224） | 原始JSON查看（read）；无可用重置/编辑 | 查看（阶段1/5）；重置/编辑待白名单审定 | 缺模块API；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [yearmonster.go](../internal/game/yearmonster.go) |

## 全服模块（30项）

每一项已有真实state构造器和注册元数据，但仍缺网关管理读取入口。下列8个赛季状态项（两类冠军赛分列）才进入计划明确的结算审查集合；登记不是幂等性已验收。其他项的“仅查看”明确不提供强制结算，原因是没有计划批准、已验证的统一结算入口，不能新造发奖/赛季规则。

| 范围 | 模块键 | 中文名/对应窗口 | 当前后台能力 | 后续拟提供 | 缺口或限制原因 | 源码依据 |
| --- | --- | --- | --- | --- | --- | --- |
| global | `adventure_mine_world` | 空岛灵矿共享占领（20640–20660） | 注册元数据可读；未接网关数据API | 查看（阶段3/5，经网关）；不提供自由编辑/强制结算 | 共享状态/记录须经服务锁读取；无已审定的强制结算白名单，不改玩法规则 | [adventure_mine.go](../internal/game/adventure_mine.go) |
| global | `arenachamp` | 本服冠军赛（20250–20282） | 注册元数据可读；未接网关数据API | 查看、专用赛季视图/排名、赛季操作（阶段3/5，待幂等验收） | 仅允许网关持锁复用既有结算；重复调用不得重复邮件/发奖 | [arenachampion.go](../internal/game/arenachampion.go)、[arenachampion_view.go](../internal/game/arenachampion_view.go) |
| global | `arenamany` | 多人竞技场（29000–29035） | 注册元数据可读；未接网关数据API | 查看、专用赛季视图/排名、赛季操作（阶段3/5，待幂等验收） | 仅允许网关持锁复用既有结算；重复调用不得重复邮件/发奖 | [arenamany.go](../internal/game/arenamany.go) |
| global | `arenateam` | 组队竞技场全服赛季（27200–27256） | 注册元数据可读；未接网关数据API | 查看、专用赛季视图/排名、赛季操作（阶段3/5，待幂等验收） | 仅允许网关持锁复用既有结算；重复调用不得重复邮件/发奖 | [arenateam.go](../internal/game/arenateam.go) |
| global | `crossarena` | 跨服竞技场（25600–25618） | 注册元数据可读；未接网关数据API | 查看、专用赛季视图/排名、赛季操作（阶段3/5，待幂等验收） | 仅允许网关持锁复用既有结算；重复调用不得重复邮件/发奖 | [arenachampion.go](../internal/game/arenachampion.go)、[crossarena.go](../internal/game/crossarena.go) |
| global | `crosschamp` | 跨服冠军赛（26200–26215） | 注册元数据可读；未接网关数据API | 查看、专用赛季视图/排名、赛季操作（阶段3/5，待幂等验收） | 仅允许网关持锁复用既有结算；重复调用不得重复邮件/发奖 | [arenachampion.go](../internal/game/arenachampion.go)、[arenachampion_view.go](../internal/game/arenachampion_view.go) |
| global | `daysrank` | 七天排行（22700–22704） | 注册元数据可读；未接网关数据API | 查看、专用赛季视图/排名、赛季操作（阶段3/5，待幂等验收） | 仅允许网关持锁复用既有结算；重复调用不得重复邮件/发奖 | [daysrank.go](../internal/game/daysrank.go) |
| global | `dial` | 转盘抽奖/记录（16670–16676） | 注册元数据可读；未接网关数据API | 查看（阶段3/5，经网关）；不提供自由编辑/强制结算 | 共享状态/记录须经服务锁读取；无已审定的强制结算白名单，不改玩法规则 | [dial.go](../internal/game/dial.go) |
| global | `dramapass` | 推图通关录像/进度榜（13002、13015、13020） | 注册元数据可读；未接网关数据API | 查看（阶段3/5，经网关）；不提供自由编辑/强制结算 | 共享状态/记录须经服务锁读取；无已审定的强制结算白名单，不改玩法规则 | [drama_pass.go](../internal/game/drama_pass.go) |
| global | `elitematch` | 精英段位赛（24900–24946） | 注册元数据可读；未接网关数据API | 查看、专用赛季视图/排名、赛季操作（阶段3/5，待幂等验收） | 仅允许网关持锁复用既有结算；重复调用不得重复邮件/发奖 | [elitematch_season.go](../internal/game/elitematch_season.go) |
| global | `endless_rank` | 无尽试炼排行榜（23903/23907） | 注册元数据可读；未接网关数据API | 查看（阶段3/5，经网关）；不提供自由编辑/强制结算 | 共享状态/记录须经服务锁读取；无已审定的强制结算白名单，不改玩法规则 | [endless.go](../internal/game/endless.go) |
| global | `escort_board` | 护送共享记录/劫掠（24000–24020） | 注册元数据可读；未接网关数据API | 查看（阶段3/5，经网关）；不提供自由编辑/强制结算 | 共享状态/记录须经服务锁读取；无已审定的强制结算白名单，不改玩法规则 | [escort.go](../internal/game/escort.go) |
| global | `guildmarket:*` | 公会宝库共享库存（26900–26903） | 注册元数据可读；未接网关数据API | 查看（阶段3/5，经网关）；不提供自由编辑/强制结算 | 共享状态/记录须经服务锁读取；无已审定的强制结算白名单，不改玩法规则 | [guildmarket.go](../internal/game/guildmarket.go) |
| global | `guildredbag` | 公会红包簿（13534–13546） | 注册元数据可读；未接网关数据API | 查看（阶段3/5，经网关）；不提供自由编辑/强制结算 | 共享状态/记录须经服务锁读取；无已审定的强制结算白名单，不改玩法规则 | [guild_redbag.go](../internal/game/guild_redbag.go) |
| global | `guildwar` | 公会战（24200–24223） | 注册元数据可读；未接网关数据API | 查看、专用赛季视图/排名、赛季操作（阶段3/5，待幂等验收） | 仅允许网关持锁复用既有结算；重复调用不得重复邮件/发奖 | [guildwar.go](../internal/game/guildwar.go) |
| global | `homeworld_board` | 家园共享人气榜（26020/26021） | 注册元数据可读；未接网关数据API | 查看（阶段3/5，经网关）；不提供自由编辑/强制结算 | 共享状态/记录须经服务锁读取；无已审定的强制结算白名单，不改玩法规则 | [homeworld.go](../internal/game/homeworld.go) |
| global | `ladder` | 天梯（24300–24318） | 注册元数据可读；未接网关数据API | 查看（阶段3/5，经网关）；不提供自由编辑/强制结算 | 共享状态/记录须经服务锁读取；无已审定的强制结算白名单，不改玩法规则 | [ladder.go](../internal/game/ladder.go) |
| global | `levgift` | 升级礼包/全服限量（21200/21201） | 注册元数据可读；未接网关数据API | 查看（阶段3/5，经网关）；不提供自由编辑/强制结算 | 共享状态/记录须经服务锁读取；无已审定的强制结算白名单，不改玩法规则 | [levgift.go](../internal/game/levgift.go) |
| global | `monopoly_g*` | 和之国火祭公会状态（27400–27506） | 注册元数据可读；未接网关数据API | 查看（阶段3/5，经网关）；不提供自由编辑/强制结算 | 共享状态/记录须经服务锁读取；无已审定的强制结算白名单，不改玩法规则 | [monopoly.go](../internal/game/monopoly.go) |
| global | `petard` | 花火大会（27000–27008） | 注册元数据可读；未接网关数据API | 查看（阶段3/5，经网关）；不提供自由编辑/强制结算 | 共享状态/记录须经服务锁读取；无已审定的强制结算白名单，不改玩法规则 | [petard.go](../internal/game/petard.go) |
| global | `practisetower_rank` | 兔丼监狱共享记录/排行（29100–29107） | 注册元数据可读；未接网关数据API | 查看（阶段3/5，经网关）；不提供自由编辑/强制结算 | 共享状态/记录须经服务锁读取；无已审定的强制结算白名单，不改玩法规则 | [practisetower.go](../internal/game/practisetower.go) |
| global | `primus` | 霸气修炼（20700–20706） | 注册元数据可读；未接网关数据API | 查看（阶段3/5，经网关）；不提供自由编辑/强制结算 | 共享状态/记录须经服务锁读取；无已审定的强制结算白名单，不改玩法规则 | [primus.go](../internal/game/primus.go) |
| global | `returnaction` | 回归活动（27900–27915） | 注册元数据可读；未接网关数据API | 查看（阶段3/5，经网关）；不提供自由编辑/强制结算 | 共享状态/记录须经服务锁读取；无已审定的强制结算白名单，不改玩法规则 | [returnaction.go](../internal/game/returnaction.go) |
| global | `sandybeach` | 沙滩Boss（25400–25405） | 注册元数据可读；未接网关数据API | 查看（阶段3/5，经网关）；不提供自由编辑/强制结算 | 共享状态/记录须经服务锁读取；无已审定的强制结算白名单，不改玩法规则 | [sandybeach.go](../internal/game/sandybeach.go) |
| global | `smashegg_log` | 砸金蛋共享记录（16684/16685） | 注册元数据可读；未接网关数据API | 查看（阶段3/5，经网关）；不提供自由编辑/强制结算 | 共享状态/记录须经服务锁读取；无已审定的强制结算白名单，不改玩法规则 | [smashegg.go](../internal/game/smashegg.go) |
| global | `snatch` | 节日夺宝（25700–25704、26300/26301） | 注册元数据可读；未接网关数据API | 查看（阶段3/5，经网关）；不提供自由编辑/强制结算 | 共享状态/记录须经服务锁读取；无已审定的强制结算白名单，不改玩法规则 | [snatch.go](../internal/game/snatch.go) |
| global | `sweet` | 甜蜜大作战（28500–28502） | 注册元数据可读；未接网关数据API | 查看（阶段3/5，经网关）；不提供自由编辑/强制结算 | 共享状态/记录须经服务锁读取；无已审定的强制结算白名单，不改玩法规则 | [sweet.go](../internal/game/sweet.go) |
| global | `termbegins` | 开学季（26700–26712） | 注册元数据可读；未接网关数据API | 查看（阶段3/5，经网关）；不提供自由编辑/强制结算 | 共享状态/记录须经服务锁读取；无已审定的强制结算白名单，不改玩法规则 | [termbegins.go](../internal/game/termbegins.go) |
| global | `treasure` | 幸运/高级探宝（16637–16643） | 注册元数据可读；未接网关数据API | 查看（阶段3/5，经网关）；不提供自由编辑/强制结算 | 共享状态/记录须经服务锁读取；无已审定的强制结算白名单，不改玩法规则 | [treasure.go](../internal/game/treasure.go) |
| global | `yearmonster` | 年兽活动（28200–28224） | 注册元数据可读；未接网关数据API | 查看（阶段3/5，经网关）；不提供自由编辑/强制结算 | 共享状态/记录须经服务锁读取；无已审定的强制结算白名单，不改玩法规则 | [yearmonster.go](../internal/game/yearmonster.go) |

## 专用表与基础设施（92张）

表范围固定记为 `table`，标题标明业务归属。没有现成后台入口的游戏表均列为待接只读视图，不因阶段4只举了八类页签就从清单消失；下表也不授权新增通用SQL写入。多表事务、领奖幂等及服务锁必须保留，不能逐表清空。现有写入入口仅记录事实，不代表已符合新接口的离线写约束。

| 范围 | 表名 | 中文名/对应窗口 | 当前后台能力 | 后续拟提供 | 缺口或限制原因 | 源码依据 |
| --- | --- | --- | --- | --- | --- | --- |
| table | `accounts` | 游戏账号/账号页 | 账号页查看、封禁/解封（read/write） | 保留现有专用入口；补未展示的只读明细 | 现有能力仅限当前列；不得把写入副作用或聚合数当全量数据覆盖 | [001_init.sql](../internal/store/migrations/001_init.sql) |
| table | `activities` | 活动配置/活动页 | 活动页查看、新建/编辑/复制/删除（read/write） | 保留现有专用入口；补未展示的只读明细 | 现有能力仅限当前列；不得把写入副作用或聚合数当全量数据覆盖 | [048_activities.sql](../internal/store/migrations/048_activities.sql) |
| table | `admins` | 管理员账号/管理员页 | 管理员页查看、创建、角色/启停/改密（manage） | 沿用专用管理页；不提供原始密码哈希/通用SQL编辑 | 账号凭据必须脱敏，只通过既有改密/启停流程维护 | [001_init.sql](../internal/store/migrations/001_init.sql) |
| table | `arena_claims` | 竞技场周宝箱领取 | 未提供该表管理视图 | 查看（按角色/区服）；不提供任意逐表编辑/清空 | 需补只读视图；跨表约束/领奖记录不可绕业务函数修改 | [038_arena.sql](../internal/store/migrations/038_arena.sql) |
| table | `arena_logs` | 竞技场比赛记录 | 未提供该表管理视图 | 查看（按角色/区服）；不提供任意逐表编辑/清空 | 需补只读视图；跨表约束/领奖记录不可绕业务函数修改 | [038_arena.sql](../internal/store/migrations/038_arena.sql) |
| table | `arena_players` | 竞技场积分/次数/赛季 | 未提供该表管理视图 | 查看（按角色/区服）；不提供任意逐表编辑/清空 | 需补只读视图；跨表约束/领奖记录不可绕业务函数修改 | [038_arena.sql](../internal/store/migrations/038_arena.sql) |
| table | `arena_slots` | 竞技场对手列表 | 未提供该表管理视图 | 查看（按角色/区服）；不提供任意逐表编辑/清空 | 需补只读视图；跨表约束/领奖记录不可绕业务函数修改 | [038_arena.sql](../internal/store/migrations/038_arena.sql) |
| table | `audit_logs` | 后台审计/审计页 | 审计页只读（manage） | 沿用专用审计只读；不提供编辑/删除 | 审计是证据，不能被通用数据编辑改写 | [001_init.sql](../internal/store/migrations/001_init.sql) |
| table | `chat_offline` | 离线聊天消息 | 未提供该表管理视图 | 按角色查看（脱敏）；不提供改写聊天历史 | 私聊内容应限权脱敏；不把历史文本当可编辑玩法配置 | [007_chat.sql](../internal/store/migrations/007_chat.sql) |
| table | `checkin` | 签到及补签 | 未提供该表管理视图 | 查看（按角色/区服）；不提供任意逐表编辑/清空 | 需补只读视图；跨表约束/领奖记录不可绕业务函数修改 | [009_checkin.sql](../internal/store/migrations/009_checkin.sql) |
| table | `counters` | 成就累计计数 | 未提供该表管理视图 | 查看（按角色/区服）；不提供任意逐表编辑/清空 | 需补只读视图；跨表约束/领奖记录不可绕业务函数修改 | [021_feats.sql](../internal/store/migrations/021_feats.sql) |
| table | `daily_quests` | 日常任务进度/领取 | 未提供该表管理视图 | 查看（按角色/区服）；不提供任意逐表编辑/清空 | 需补只读视图；跨表约束/领奖记录不可绕业务函数修改 | [015_daily_quests.sql](../internal/store/migrations/015_daily_quests.sql) |
| table | `drama_daily` | 推图每日扫荡 | 未提供该表管理视图 | 查看（按角色/区服）；不提供任意逐表编辑/清空 | 需补只读视图；跨表约束/领奖记录不可绕业务函数修改 | [017_drama_extras.sql](../internal/store/migrations/017_drama_extras.sql) |
| table | `drama_hook` | 挂机收益状态 | 未提供该表管理视图 | 查看（按角色/区服）；不提供任意逐表编辑/清空 | 需补只读视图；跨表约束/领奖记录不可绕业务函数修改 | [017_drama_extras.sql](../internal/store/migrations/017_drama_extras.sql) |
| table | `drama_milestones` | 推图里程碑领取 | 未提供该表管理视图 | 查看（按角色/区服）；不提供任意逐表编辑/清空 | 需补只读视图；跨表约束/领奖记录不可绕业务函数修改 | [018_drama_milestones.sql](../internal/store/migrations/018_drama_milestones.sql) |
| table | `elite_summon` | 精英召唤保底/积分 | 未提供该表管理视图 | 查看（按角色/区服）；不提供任意逐表编辑/清空 | 需补只读视图；跨表约束/领奖记录不可绕业务函数修改 | [027_elite_summon.sql](../internal/store/migrations/027_elite_summon.sql) |
| table | `feats_claimed` | 成就领取记录 | 未提供该表管理视图 | 查看（按角色/区服）；不提供任意逐表编辑/清空 | 需补只读视图；跨表约束/领奖记录不可绕业务函数修改 | [021_feats.sql](../internal/store/migrations/021_feats.sql) |
| table | `forge_logs` | 锻造屋合成记录 | 未提供该表管理视图 | 查看（按角色/区服）；不提供任意逐表编辑/清空 | 需补只读视图；跨表约束/领奖记录不可绕业务函数修改 | [044_forge_logs.sql](../internal/store/migrations/044_forge_logs.sql) |
| table | `formation_slots` | 各玩法阵容槽位 | 未提供该表管理视图 | 查看（按角色/区服）；不提供任意逐表编辑/清空 | 需补只读视图；跨表约束/领奖记录不可绕业务函数修改 | [020_formations.sql](../internal/store/migrations/020_formations.sql) |
| table | `formations` | 各玩法阵容主记录 | 未提供该表管理视图 | 查看（按角色/区服）；不提供任意逐表编辑/清空 | 需补只读视图；跨表约束/领奖记录不可绕业务函数修改 | [020_formations.sql](../internal/store/migrations/020_formations.sql) |
| table | `friend_blacklist` | 好友黑名单 | 未提供该表管理视图 | 查看（按角色/区服）；不提供任意逐表编辑/清空 | 需补只读视图；跨表约束/领奖记录不可绕业务函数修改 | [047_friend_blacklist.sql](../internal/store/migrations/047_friend_blacklist.sql) |
| table | `friend_gifts` | 好友赠礼/领取 | 未提供该表管理视图 | 查看（按角色/区服）；不提供任意逐表编辑/清空 | 需补只读视图；跨表约束/领奖记录不可绕业务函数修改 | [011_friend_gifts.sql](../internal/store/migrations/011_friend_gifts.sql) |
| table | `friend_requests` | 好友申请 | 未提供该表管理视图 | 查看（按角色/区服）；不提供任意逐表编辑/清空 | 需补只读视图；跨表约束/领奖记录不可绕业务函数修改 | [006_friends.sql](../internal/store/migrations/006_friends.sql) |
| table | `friends` | 好友关系 | 未提供该表管理视图 | 查看（按角色/区服）；不提供任意逐表编辑/清空 | 需补只读视图；跨表约束/领奖记录不可绕业务函数修改 | [006_friends.sql](../internal/store/migrations/006_friends.sql) |
| table | `global_state` | 全服模块JSON容器 | 未提供该表管理视图 | 按全服模块注册表查看；不提供后台直接编辑 | 容器不是单一业务对象；必须转发网关，不能绕服务锁写库 | [045_module_state.sql](../internal/store/migrations/045_module_state.sql) |
| table | `guild_applies` | 公会申请 | 未提供该表管理视图 | 查看（按角色/区服）；不提供任意逐表编辑/清空 | 需补只读视图；跨表约束/领奖记录不可绕业务函数修改 | [036_guild.sql](../internal/store/migrations/036_guild.sql) |
| table | `guild_boss_dmg` | 公会Boss伤害记录 | 未提供该表管理视图 | 查看（按角色/区服）；不提供任意逐表编辑/清空 | 需补只读视图；跨表约束/领奖记录不可绕业务函数修改 | [039_guild_boss.sql](../internal/store/migrations/039_guild_boss.sql) |
| table | `guild_boss_kills` | 公会Boss击杀记录 | 未提供该表管理视图 | 查看（按角色/区服）；不提供任意逐表编辑/清空 | 需补只读视图；跨表约束/领奖记录不可绕业务函数修改 | [039_guild_boss.sql](../internal/store/migrations/039_guild_boss.sql) |
| table | `guild_boss_roles` | 公会Boss个人次数 | 未提供该表管理视图 | 查看（按角色/区服）；不提供任意逐表编辑/清空 | 需补只读视图；跨表约束/领奖记录不可绕业务函数修改 | [039_guild_boss.sql](../internal/store/migrations/039_guild_boss.sql) |
| table | `guild_boss_state` | 公会Boss共享血量/进度 | 未提供该表管理视图 | 查看（按角色/区服）；不提供任意逐表编辑/清空 | 需补只读视图；跨表约束/领奖记录不可绕业务函数修改 | [039_guild_boss.sql](../internal/store/migrations/039_guild_boss.sql) |
| table | `guild_box_claims` | 公会捐献宝箱领取 | 未提供该表管理视图 | 查看（按角色/区服）；不提供任意逐表编辑/清空 | 需补只读视图；跨表约束/领奖记录不可绕业务函数修改 | [036_guild.sql](../internal/store/migrations/036_guild.sql) |
| table | `guild_cooldowns` | 退会/会长冷却 | 未提供该表管理视图 | 查看（按角色/区服）；不提供任意逐表编辑/清空 | 需补只读视图；跨表约束/领奖记录不可绕业务函数修改 | [036_guild.sql](../internal/store/migrations/036_guild.sql) |
| table | `guild_logs` | 公会日志 | 未提供该表管理视图 | 查看（按角色/区服）；不提供任意逐表编辑/清空 | 需补只读视图；跨表约束/领奖记录不可绕业务函数修改 | [036_guild.sql](../internal/store/migrations/036_guild.sql) |
| table | `guild_members` | 公会成员/职位/捐献 | 公会列表仅人数/会长，未逐成员展示（read） | 保留现有专用入口；补未展示的只读明细 | 现有能力仅限当前列；不得把写入副作用或聚合数当全量数据覆盖 | [036_guild.sql](../internal/store/migrations/036_guild.sql) |
| table | `guild_secret` | 公会秘境共享进度 | 未提供该表管理视图 | 查看（按角色/区服）；不提供任意逐表编辑/清空 | 需补只读视图；跨表约束/领奖记录不可绕业务函数修改 | [041_guild_secret.sql](../internal/store/migrations/041_guild_secret.sql) |
| table | `guild_secret_claims` | 公会秘境奖励领取 | 未提供该表管理视图 | 查看（按角色/区服）；不提供任意逐表编辑/清空 | 需补只读视图；跨表约束/领奖记录不可绕业务函数修改 | [041_guild_secret.sql](../internal/store/migrations/041_guild_secret.sql) |
| table | `guild_secret_dmg` | 公会秘境伤害记录 | 未提供该表管理视图 | 查看（按角色/区服）；不提供任意逐表编辑/清空 | 需补只读视图；跨表约束/领奖记录不可绕业务函数修改 | [041_guild_secret.sql](../internal/store/migrations/041_guild_secret.sql) |
| table | `guild_secret_roles` | 公会秘境个人次数 | 未提供该表管理视图 | 查看（按角色/区服）；不提供任意逐表编辑/清空 | 需补只读视图；跨表约束/领奖记录不可绕业务函数修改 | [041_guild_secret.sql](../internal/store/migrations/041_guild_secret.sql) |
| table | `guild_skill_flags` | 公会技能标记 | 未提供该表管理视图 | 查看（按角色/区服）；不提供任意逐表编辑/清空 | 需补只读视图；跨表约束/领奖记录不可绕业务函数修改 | [037_guild_skill.sql](../internal/store/migrations/037_guild_skill.sql) |
| table | `guild_skill_pve` | 公会PvE技能 | 未提供该表管理视图 | 查看（按角色/区服）；不提供任意逐表编辑/清空 | 需补只读视图；跨表约束/领奖记录不可绕业务函数修改 | [037_guild_skill.sql](../internal/store/migrations/037_guild_skill.sql) |
| table | `guild_skill_pvp` | 公会PvP技能 | 未提供该表管理视图 | 查看（按角色/区服）；不提供任意逐表编辑/清空 | 需补只读视图；跨表约束/领奖记录不可绕业务函数修改 | [037_guild_skill.sql](../internal/store/migrations/037_guild_skill.sql) |
| table | `guilds` | 公会/公会页 | 公会列表/详情及解散（read/write） | 保留现有专用入口；补未展示的只读明细 | 现有能力仅限当前列；不得把写入副作用或聚合数当全量数据覆盖 | [036_guild.sql](../internal/store/migrations/036_guild.sql) |
| table | `hero_equips` | 英雄普通装备槽位 | 未提供该表管理视图 | 查看（按角色/区服）；不提供任意逐表编辑/清空 | 需补只读视图；跨表约束/领奖记录不可绕业务函数修改 | [019_hero_equips.sql](../internal/store/migrations/019_hero_equips.sql) |
| table | `invite_binds` | 邀请绑定 | 未提供该表管理视图 | 查看（按角色/区服）；不提供任意逐表编辑/清空 | 需补只读视图；跨表约束/领奖记录不可绕业务函数修改 | [010_invite.sql](../internal/store/migrations/010_invite.sql) |
| table | `invite_claims` | 邀请奖励领取 | 未提供该表管理视图 | 查看（按角色/区服）；不提供任意逐表编辑/清空 | 需补只读视图；跨表约束/领奖记录不可绕业务函数修改 | [010_invite.sql](../internal/store/migrations/010_invite.sql) |
| table | `invite_codes` | 邀请码 | 未提供该表管理视图 | 查看（按角色/区服）；不提供任意逐表编辑/清空 | 需补只读视图；跨表约束/领奖记录不可绕业务函数修改 | [010_invite.sql](../internal/store/migrations/010_invite.sql) |
| table | `login_tokens` | 玩家登录凭据 | 未提供该表管理视图 | 不提供凭据列表/原文；沿用账号封禁撤销 | 玩家登录凭据，公开token会造成会话冒用，不属玩法状态浏览 | [001_init.sql](../internal/store/migrations/001_init.sql) |
| table | `mail_attachments` | 邮件附件/邮件页 | 全局发件历史附件及领取汇总（read） | 保留现有专用入口；补未展示的只读明细 | 现有能力仅限当前列；不得把写入副作用或聚合数当全量数据覆盖 | [001_init.sql](../internal/store/migrations/001_init.sql) |
| table | `mails` | 邮件主记录/邮件页 | 发件历史、详情及发送（read/write） | 保留现有专用入口；补未展示的只读明细 | 现有能力仅限当前列；不得把写入副作用或聚合数当全量数据覆盖 | [001_init.sql](../internal/store/migrations/001_init.sql) |
| table | `module_state` | 玩家模块JSON容器 | 已有状态列表/原始JSON查看（read）；生产重置白名单为空 | 按玩家模块注册表查看；白名单重置/编辑后续审定 | 容器需按真实state类型校验，不能裸SQL/任意JSON更新 | [045_module_state.sql](../internal/store/migrations/045_module_state.sql) |
| table | `notices` | 公告/公告页 | 公告页查看及增改删（read/write） | 保留现有专用入口；补未展示的只读明细 | 现有能力仅限当前列；不得把写入副作用或聚合数当全量数据覆盖 | [001_init.sql](../internal/store/migrations/001_init.sql) |
| table | `online_gift_claims` | 在线奖励领取 | 未提供该表管理视图 | 查看（按角色/区服）；不提供任意逐表编辑/清空 | 需补只读视图；跨表约束/领奖记录不可绕业务函数修改 | [008_online_gift.sql](../internal/store/migrations/008_online_gift.sql) |
| table | `online_time` | 累计在线时长 | 未提供该表管理视图 | 查看（按角色/区服）；不提供任意逐表编辑/清空 | 需补只读视图；跨表约束/领奖记录不可绕业务函数修改 | [008_online_gift.sql](../internal/store/migrations/008_online_gift.sql) |
| table | `partner_changes` | 英雄置换预览 | 未提供该表管理视图 | 查看（按角色/区服）；不提供任意逐表编辑/清空 | 需补只读视图；跨表约束/领奖记录不可绕业务函数修改 | [025_partner_changes.sql](../internal/store/migrations/025_partner_changes.sql) |
| table | `partner_talents` | 英雄天赋 | 未提供该表管理视图 | 查看（按角色/区服）；不提供任意逐表编辑/清空 | 需补只读视图；跨表约束/领奖记录不可绕业务函数修改 | [029_partner_talents.sql](../internal/store/migrations/029_partner_talents.sql) |
| table | `partners` | 英雄实例/属性/培养 | 仅发放新英雄（write），无拥有列表/属性视图 | 保留现有专用入口；补未展示的只读明细 | 现有能力仅限当前列；不得把写入副作用或聚合数当全量数据覆盖 | [012_partners.sql](../internal/store/migrations/012_partners.sql) |
| table | `pay_config` | 支付收款配置/充值页 | 收款码/说明查看、修改（read/manage） | 保留现有专用入口；补未展示的只读明细 | 现有能力仅限当前列；不得把写入副作用或聚合数当全量数据覆盖 | [050_pay.sql](../internal/store/migrations/050_pay.sql) |
| table | `pay_orders` | 充值订单/充值页 | 全局最近订单/状态查看、创建/确认/取消（read/manage） | 保留现有专用入口；补未展示的只读明细 | 现有能力仅限当前列；不得把写入副作用或聚合数当全量数据覆盖 | [050_pay.sql](../internal/store/migrations/050_pay.sql) |
| table | `redeem_codes` | 兑换码配置/兑换码页 | 列表/创建/删除及已用次数（read/write） | 保留现有专用入口；补未展示的只读明细 | 现有能力仅限当前列；不得把写入副作用或聚合数当全量数据覆盖 | [035_role_misc.sql](../internal/store/migrations/035_role_misc.sql) |
| table | `redeem_uses` | 兑换码领取明细 | 仅兑换码已用次数汇总，无逐角色明细（read） | 保留现有专用入口；补未展示的只读明细 | 现有能力仅限当前列；不得把写入副作用或聚合数当全量数据覆盖 | [035_role_misc.sql](../internal/store/migrations/035_role_misc.sql) |
| table | `replay_collects` | 录像收藏关系 | 未提供该表管理视图 | 查看（按角色/区服）；不提供任意逐表编辑/清空 | 需补只读视图；跨表约束/领奖记录不可绕业务函数修改 | [046_replays.sql](../internal/store/migrations/046_replays.sql) |
| table | `replays` | 战斗录像元数据/脚本 | 未提供该表管理视图 | 查看录像元数据；不提供任意脚本编辑 | 脚本/参战信息需脱敏，编辑会伪造战斗证据 | [046_replays.sql](../internal/store/migrations/046_replays.sql) |
| table | `resonance_extract` | 共鸣炼金批次/领取 | 未提供该表管理视图 | 查看（按角色/区服）；不提供任意逐表编辑/清空 | 需补只读视图；跨表约束/领奖记录不可绕业务函数修改 | [034_resonance_stone.sql](../internal/store/migrations/034_resonance_stone.sql) |
| table | `resonance_slots` | 共鸣槽位/冷却 | 未提供该表管理视图 | 查看（按角色/区服）；不提供任意逐表编辑/清空 | 需补只读视图；跨表约束/领奖记录不可绕业务函数修改 | [033_resonance.sql](../internal/store/migrations/033_resonance.sql) |
| table | `role_assets` | 货币账本/角色详情 | 角色详情账本查看、发放（read/write） | 保留现有专用入口；补未展示的只读明细 | 现有能力仅限当前列；不得把写入副作用或聚合数当全量数据覆盖 | [003_mail.sql](../internal/store/migrations/003_mail.sql) |
| table | `role_badge_use` | 当前徽章 | 未提供该表管理视图 | 查看（按角色/区服）；不提供任意逐表编辑/清空 | 需补只读视图；跨表约束/领奖记录不可绕业务函数修改 | [040_space.sql](../internal/store/migrations/040_space.sql) |
| table | `role_badges` | 徽章获得记录 | 未提供该表管理视图 | 查看（按角色/区服）；不提供任意逐表编辑/清空 | 需补只读视图；跨表约束/领奖记录不可绕业务函数修改 | [040_space.sql](../internal/store/migrations/040_space.sql) |
| table | `role_charge` | 累计实付金额/VIP经验/商品购买计数 | 未提供该表管理视图 | 查看（按角色/区服）；不提供任意逐表编辑/清空 | 需补只读视图；实付记录不能改写，月卡Base另存于monthcard模块 | [050_pay.sql](../internal/store/migrations/050_pay.sql) |
| table | `role_drama` | 主线推图进度 | 未提供该表管理视图 | 查看（按角色/区服）；不提供任意逐表编辑/清空 | 需补只读视图；跨表约束/领奖记录不可绕业务函数修改 | [002_role_drama.sql](../internal/store/migrations/002_role_drama.sql) |
| table | `role_follows` | 空间关注关系 | 未提供该表管理视图 | 查看（按角色/区服）；不提供任意逐表编辑/清空 | 需补只读视图；跨表约束/领奖记录不可绕业务函数修改 | [040_space.sql](../internal/store/migrations/040_space.sql) |
| table | `role_items` | 物品账本/角色详情 | 角色详情账本查看、发放（read/write） | 保留现有专用入口；补未展示的只读明细 | 现有能力仅限当前列；不得把写入副作用或聚合数当全量数据覆盖 | [003_mail.sql](../internal/store/migrations/003_mail.sql) |
| table | `role_looks` | 外观拥有记录 | 仅发放外观（write），无拥有列表 | 保留现有专用入口；补未展示的只读明细 | 现有能力仅限当前列；不得把写入副作用或聚合数当全量数据覆盖 | [032_role_looks.sql](../internal/store/migrations/032_role_looks.sql) |
| table | `role_mails` | 角色收件/阅读/领取 | 仅发件历史收件/领取汇总，无角色收件列表（read） | 保留现有专用入口；补未展示的只读明细 | 现有能力仅限当前列；不得把写入副作用或聚合数当全量数据覆盖 | [001_init.sql](../internal/store/migrations/001_init.sql) |
| table | `role_notice_reads` | 角色公告已读 | 未提供该表管理视图 | 查看（按角色/区服）；不提供任意逐表编辑/清空 | 需补只读视图；跨表约束/领奖记录不可绕业务函数修改 | [004_notice_reads.sql](../internal/store/migrations/004_notice_reads.sql) |
| table | `role_reports` | 玩家举报/举报页 | 举报页只读（read） | 保留现有专用入口；补未展示的只读明细 | 现有能力仅限当前列；不得把写入副作用或聚合数当全量数据覆盖 | [035_role_misc.sql](../internal/store/migrations/035_role_misc.sql) |
| table | `role_resonance` | 共鸣核心/石碑等级 | 未提供该表管理视图 | 查看（按角色/区服）；不提供任意逐表编辑/清空 | 需补只读视图；跨表约束/领奖记录不可绕业务函数修改 | [033_resonance.sql](../internal/store/migrations/033_resonance.sql) |
| table | `role_settings` | 角色设置/当前外观 | 未提供该表管理视图 | 查看（按角色/区服）；不提供任意逐表编辑/清空 | 需补只读视图；跨表约束/领奖记录不可绕业务函数修改 | [032_role_looks.sql](../internal/store/migrations/032_role_looks.sql) |
| table | `role_skins` | 英雄皮肤拥有记录 | 仅发放皮肤（write），无拥有列表 | 保留现有专用入口；补未展示的只读明细 | 现有能力仅限当前列；不得把写入副作用或聚合数当全量数据覆盖 | [031_skins.sql](../internal/store/migrations/031_skins.sql) |
| table | `roles` | 角色基本资料/角色页 | 角色列表/详情、封禁/等级/VIP及踢线（read/write） | 保留现有专用入口；补未展示的只读明细 | 现有能力仅限当前列；不得把写入副作用或聚合数当全量数据覆盖 | [001_init.sql](../internal/store/migrations/001_init.sql) |
| table | `room_bbs` | 个人空间留言板 | 未提供该表管理视图 | 查看（按角色/区服）；不提供任意逐表编辑/清空 | 需补只读视图；跨表约束/领奖记录不可绕业务函数修改 | [040_space.sql](../internal/store/migrations/040_space.sql) |
| table | `schema_migrations` | 数据库迁移账本 | 未提供该表管理视图 | 不提供在线编辑；沿用部署迁移/备份流程 | 数据库基础设施元数据，任意编辑会破坏迁移一致性 | [store.go](../internal/store/store.go) |
| table | `servers` | 区服配置/区服页 | 区服查看、增改删（read/manage），含开服时间 | 保留现有专用入口；补未展示的只读明细 | 现有能力仅限当前列；不得把写入副作用或聚合数当全量数据覆盖 | [001_init.sql](../internal/store/migrations/001_init.sql) |
| table | `shop_buys` | 商城购买次数 | 未提供该表管理视图 | 查看（按角色/区服）；不提供任意逐表编辑/清空 | 需补只读视图；跨表约束/领奖记录不可绕业务函数修改 | [016_shop_buys.sql](../internal/store/migrations/016_shop_buys.sql) |
| table | `star_tower` | 海底监狱进度/次数 | 未提供该表管理视图 | 查看（按角色/区服）；不提供任意逐表编辑/清空 | 需补只读视图；跨表约束/领奖记录不可绕业务函数修改 | [042_star_tower.sql](../internal/store/migrations/042_star_tower.sql) |
| table | `star_tower_claims` | 海底监狱里程碑领取 | 未提供该表管理视图 | 查看（按角色/区服）；不提供任意逐表编辑/清空 | 需补只读视图；跨表约束/领奖记录不可绕业务函数修改 | [042_star_tower.sql](../internal/store/migrations/042_star_tower.sql) |
| table | `stone_dungeon_clears` | 材料副本首通 | 未提供该表管理视图 | 查看（按角色/区服）；不提供任意逐表编辑/清空 | 需补只读视图；跨表约束/领奖记录不可绕业务函数修改 | [043_stone_dungeon.sql](../internal/store/migrations/043_stone_dungeon.sql) |
| table | `stone_dungeon_state` | 材料副本每日次数 | 未提供该表管理视图 | 查看（按角色/区服）；不提供任意逐表编辑/清空 | 需补只读视图；跨表约束/领奖记录不可绕业务函数修改 | [043_stone_dungeon.sql](../internal/store/migrations/043_stone_dungeon.sql) |
| table | `summon_free` | 普通/高级召唤免费次数 | 未提供该表管理视图 | 查看（按角色/区服）；不提供任意逐表编辑/清空 | 需补只读视图；跨表约束/领奖记录不可绕业务函数修改 | [014_summon_free.sql](../internal/store/migrations/014_summon_free.sql) |
| table | `ticket_msgs` | 工单追问/回复 | 工单详情追问/历史回复及回复（read/write） | 保留现有专用入口；补未展示的只读明细 | 现有能力仅限当前列；不得把写入副作用或聚合数当全量数据覆盖 | [049_tickets.sql](../internal/store/migrations/049_tickets.sql) |
| table | `tickets` | 客服工单/工单页 | 工单列表/详情及回复（read/write） | 保留现有专用入口；补未展示的只读明细 | 现有能力仅限当前列；不得把写入副作用或聚合数当全量数据覆盖 | [049_tickets.sql](../internal/store/migrations/049_tickets.sql) |
| table | `variety_shop` | 随机商店货架/刷新 | 未提供该表管理视图 | 查看（按角色/区服）；不提供任意逐表编辑/清空 | 需补只读视图；跨表约束/领奖记录不可绕业务函数修改 | [026_variety_shop.sql](../internal/store/migrations/026_variety_shop.sql) |
| table | `vip_gifts` | VIP礼包领取 | 未提供该表管理视图 | 查看（按角色/区服）；不提供任意逐表编辑/清空 | 需补只读视图；跨表约束/领奖记录不可绕业务函数修改 | [024_vip_gifts.sql](../internal/store/migrations/024_vip_gifts.sql) |

## 供后续阶段审阅的缺口

1. **阶段1已实现运行时注册与玩家原始数据查看**：107项有真实state构造器，77项玩家模块可查原始JSON，三个前缀族验证窗口key或正数公会GID。所有GET不写库。具体只读专用视图、排名和充值重置白名单尚未实现；107项登记不等于后续业务功能全部完成。重置基础机制使用before值比较，数据变更与审计同一事务提交，审计失败回滚。
2. **充值状态汇总和安全重置（阶段2）**：六组优先模块为首充、每日首充、月卡、月基金、限时礼包、周/月礼包；`chargeacc`、`role_charge`应纳入只读汇总，不能混同可任意重置的已付金额。Base/roll规则仍按计划留待该阶段审定，本阶段未新定规则。
3. **全服与赛季入口缺失（阶段3）**：全服30项都没有管理代理；排名需绑定现有查询。计划指定的现有结算函数由服务锁保护，必须补幂等验收后才能开放。其余模块不提供强制结算，避免推导未知规则。
4. **世界等级是计算结果，不是现存模块**：`store.WorldLevel`及`openday.go`已有计算；区服的开服时间已能编辑。计划中的global键 `worldlev`、覆盖值/过期时间和 `/ops/worldlev` 均尚不存在，故不伪装成当前扫描项；阶段3新增后必须补注册行。现有 `global_state` 不能预先直接写入此键。
5. **专用表不止八类（阶段4）**：英雄/装备、阵容、公会职位、好友、任务、竞技场、角色邮件、角色订单是优先只读页签；签到、邀请、商店、召唤、共鸣、推图/副本、领奖流水等其余表同样在本表逐项验收。表缺视图是待补缺口，不是“16页已迁移”就算完成。
6. **前端和阶段6收口仍待做**：没有模块总表、玩法状态、充值状态及上述角色页签；本文不将ROADMAP标完成，不改前端权限或依赖，不更新UI快照，不进入后续阶段。

## 阶段0及阶段1验收边界

本地只读扫描与覆盖门禁通过，才代表“登记未遗漏”；不代表游戏规则、原服证据或后续写操作已验收。注册表外新模块/表必须先补人工说明，再通过测试。执行计划要求的vet、race测试、前端类型检查/原28项测试及构建结果随本阶段提交报告给出。阶段1提交运行时注册、玩家侧API、重置基础机制及测试/文档。生产白名单保持空，没有验证或启用阶段2的具体充值重置；全服网关ops、前端及真实玩家写入未涉及。不变更玩法、抓包、依赖锁文件或生产UI快照，不推送，停于阶段1等待审阅。
