# 后台数据覆盖注册表（更新至阶段6文档收口）

执行计划见 [ADMIN_COMPLETION_PLAN.md](ADMIN_COMPLETION_PLAN.md)。阶段3拉取基线为 `c140a39`；当前111项运行时注册（80玩家、31全服；11-05源码复核）。阶段2的六类充值视图/重置保持不变；阶段3已接全服状态、16项现有排名、赛季及区服概览、8个审定赛季状态项的自然到期补结算和世界等级覆盖。全服操作只经网关，在真实服务锁内执行；补结算和世界等级修改与审计同事务。全部 `Editable=false`，不提供全局JSON编辑；前端已接角色数据页签、玩法状态和模块总表；区服列表“玩法状态”链接和公会列表“数据表”按钮可直达对应视图。

阶段4新增82项静态表视图注册，10项明确保留专用页或不提供通用查询；每张表均在下表注明。API及归属/截断约定见 [ADMIN_TABLE_API.md](ADMIN_TABLE_API.md)。表接口只有GET，不改变任何既有写权限；前端已接角色数据页签、玩法状态和模块总表；区服列表“玩法状态”链接和公会列表“数据表”按钮可直达对应视图。

## 口径与生成

- `role`：`LoadState/SaveState` 的模块参数；`global`：`LoadGlobal/SaveGlobal` 的模块参数。相同 key 在两个范围出现须分别登记，不按 key 去重。
- 源码扫描到 **360 处持久化调用、80 项玩家模块、31 项全服模块**；其中三项为动态前缀，代表整族模块而不是一个已存在的数据库行。另检查88个以 `Module`/`Mod` 结尾的字符串模块常量。阶段3新增 `worldlev`；计划中的“约65/31”是估计，不能当验收数量。
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
- **80项玩家模块和31项全服模块均有只读API，111项可查询注册元数据。** 全服读取只经网关；无网关或缺服务绑定返回502，不降级读库。GET不创建默认行、不触发结算；原始data与内存view分别返回。阶段2六类充值重置保持原白名单，阶段3全服16项Rank复用现有排序；`GET /api/roles/{id}/charge`仍汇总9个具体充值模块与该角色订单。
- 当前角色详情保留基本资料、发放及账本，另有“玩法与只读数据”：充值状态与角色订单、模块JSON及计算视图、全部role表分组。英雄/装备/天赋、阵容、任务/好友、收件等经通用白名单表读取；没有专用投影的字段显示存储值，不虚构游戏计算属性。
- 现有写操作并非全部执行新计划的“先查离线、不可达不写”；本阶段不更改旧接口或规则。后续新增玩家数据写必须先网关在线校验（在线409、未知502），再读before、写入、审计。不能用当前踢线选项代替这一前置条件。
- 全服模块数据必须经网关，在所属服务的锁/定时器边界内读取/修改；禁止后台直接调用 `SaveGlobal`。专用表若同样受网关服务锁保护，也不能因其是SQL表就绕开该边界。

## 玩家模块（80项）

2026-11-05新增 `role_power` 的只读登记，用于查看10306历史战力峰值；不开放编辑或重置。当前源码扫描为360处持久化调用、80项玩家模块、31项全服模块、96张显式表、88个模块常量。前文阶段统计保留历史口径，当前运行时共111项登记。

原始JSON和充值专用视图均只读。专用视图在新解码的内存副本中复用现有activate/view/roll；不调用会保存或发奖的游戏loader。重置仅superadmin，先核对网关离线状态，再持同一角色付款锁检查白名单/读before，最后比较before并将写入与审计同事务提交。`all`只在首充窗口显式开放；不提供JSON编辑。

| 范围 | 模块键 | 中文名/对应窗口 | 当前后台能力 | 后续拟提供 | 缺口或限制原因 | 源码依据 |
| --- | --- | --- | --- | --- | --- | --- |
| role | `activity` | 活动个人目标/领取（16601–16620、16687） | 原始JSON查看（read）；无可用重置/编辑 | 查看（模块数据页签）；重置/编辑待白名单审定 | 已接模块只读API/前端；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [activity.go](../internal/game/activity.go) |
| role | `activitybox` | 日常活跃宝箱（20300/20301） | 原始JSON查看（read）；无可用重置/编辑 | 查看（模块数据页签）；重置/编辑待白名单审定 | 已接模块只读API/前端；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [activitybox.go](../internal/game/activitybox.go) |
| role | `adventure` | 空岛冒险（20600–20636） | 原始JSON查看（read）；无可用重置/编辑 | 查看（模块数据页签）；重置/编辑待白名单审定 | 已接模块只读API/前端；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [adventure.go](../internal/game/adventure.go)、[adventure_mine.go](../internal/game/adventure_mine.go) |
| role | `adventure_mine` | 空岛灵矿个人状态（20640–20660） | 原始JSON查看（read）；无可用重置/编辑 | 查看（模块数据页签）；重置/编辑待白名单审定 | 已接模块只读API/前端；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [adventure_mine.go](../internal/game/adventure_mine.go) |
| role | `animate` | 元宵花灯/厨房（24801–24809） | 原始JSON查看（read）；无可用重置/编辑 | 查看（模块数据页签）；重置/编辑待白名单审定 | 已接模块只读API/前端；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [animate.go](../internal/game/animate.go) |
| role | `arenachamp` | 本服冠军赛（20250–20282） | 原始JSON查看（read）；无可用重置/编辑 | 查看（模块数据页签）；重置/编辑待白名单审定 | 已接模块只读API/前端；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [arenachampion.go](../internal/game/arenachampion.go)、[arenachampion_view.go](../internal/game/arenachampion_view.go) |
| role | `arenateamrole` | 组队竞技场个人状态（27200–27256） | 原始JSON查看（read）；无可用重置/编辑 | 查看（模块数据页签）；重置/编辑待白名单审定 | 已接模块只读API/前端；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [arenateam.go](../internal/game/arenateam.go) |
| role | `artifact` | 战技实例/重铸/穿戴（11030–11036） | 原始JSON查看（read）；无可用重置/编辑 | 查看（模块数据页签）；重置/编辑待白名单审定 | 已接模块只读API/前端；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [artifact.go](../internal/store/artifact.go) |
| role | `chargeacc` | 累计充值奖励（16700、16712/16713） | 原始JSON查看（read）；无可用重置/编辑 | 查看、充值专用视图；不提供任意编辑累计/领取 | 已接只读模块入口；累计充值和已发奖事实不能靠JSON编辑改写 | [charge_proto.go](../internal/game/charge_proto.go)、[chargeacc.go](../internal/game/chargeacc.go) |
| role | `crosschamp` | 跨服冠军赛（26200–26215） | 原始JSON查看（read）；无可用重置/编辑 | 查看（模块数据页签）；重置/编辑待白名单审定 | 已接模块只读API/前端；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [arenachampion.go](../internal/game/arenachampion.go)、[arenachampion_view.go](../internal/game/arenachampion_view.go) |
| role | `cyclegift` | 每周福利/月度礼包限购（21007） | 原始JSON、周/月礼包视图（read）；配置内charge ID重置（manage） | 已接查看/专用视图/单商品计数重置 | 白名单从12项配置生成；roll仅在查看副本中执行，重置只删指定Count键 | [cyclegift.go](../internal/game/cyclegift.go) |
| role | `dailygift` | 每日礼包购买次数（21006） | 原始JSON查看（read）；无可用重置/编辑 | 查看（模块数据页签）；重置/编辑待白名单审定 | 已接模块只读API/前端；改购买次数会让限购和已发奖励对不上，不默认开放 | [dailygift.go](../internal/game/dailygift.go) |
| role | `daycharge` | 每日首充（21010/21011） | 原始JSON、每日首充视图（read）；today重置（manage） | 已接查看/专用视图/当日重置 | 仅清当日Num/Taken并对齐Day；既有实付历史不变，须新充值达标 | [daycharge.go](../internal/game/daycharge.go) |
| role | `daysrank` | 七天排行（22700–22704） | 原始JSON查看（read）；无可用重置/编辑 | 查看（模块数据页签）；重置/编辑待白名单审定 | 已接模块只读API/前端；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [daysrank.go](../internal/game/daysrank.go) |
| role | `dial` | 转盘抽奖/记录（16670–16676） | 原始JSON查看（read）；无可用重置/编辑 | 查看（模块数据页签）；重置/编辑待白名单审定 | 已接模块只读API/前端；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [dial.go](../internal/game/dial.go) |
| role | `element` | 元素圣殿/领地挑战（25000–25006） | 原始JSON查看（read）；无可用重置/编辑 | 查看（模块数据页签）；重置/编辑待白名单审定 | 已接模块只读API/前端；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [element.go](../internal/game/element.go) |
| role | `elfin` | 精灵/古树/队伍（26500–26564） | 原始JSON查看（read）；无可用重置/编辑 | 查看（模块数据页签）；重置/编辑待白名单审定 | 已接模块只读API/前端；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [elfin.go](../internal/game/elfin.go) |
| role | `elitematch` | 精英段位赛（24900–24946） | 原始JSON查看（read）；无可用重置/编辑 | 查看（模块数据页签）；重置/编辑待白名单审定 | 已接模块只读API/前端；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [elitematch.go](../internal/game/elitematch.go)、[elitematch_season.go](../internal/game/elitematch_season.go) |
| role | `elitepass` | 段位赛令牌活动（24952–24955） | 原始JSON查看（read）；无可用重置/编辑 | 查看（模块数据页签）；重置/编辑待白名单审定 | 已接模块只读API/前端；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [elitepass.go](../internal/game/elitepass.go) |
| role | `emoguoshi` | 恶魔果实图鉴（31101–31130） | 原始JSON查看（read）；无可用重置/编辑 | 查看（模块数据页签）；重置/编辑待白名单审定 | 已接模块只读API/前端；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [emoguoshi.go](../internal/game/emoguoshi.go) |
| role | `empower` | 共鸣赋能（26420–26424） | 原始JSON查看（read）；无可用重置/编辑 | 查看（模块数据页签）；重置/编辑待白名单审定 | 已接模块只读API/前端；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [horcrux.go](../internal/store/horcrux.go) |
| role | `encounter` | 英雄物语（27100–27104） | 原始JSON查看（read）；无可用重置/编辑 | 查看（模块数据页签）；重置/编辑待白名单审定 | 已接模块只读API/前端；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [encounter.go](../internal/game/encounter.go) |
| role | `endless_trail` | 无尽试炼个人进度（23900–23913） | 原始JSON查看（read）；无可用重置/编辑 | 查看（模块数据页签）；重置/编辑待白名单审定 | 已接模块只读API/前端；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [daysrank.go](../internal/game/daysrank.go)、[endless.go](../internal/game/endless.go)、[endless_run.go](../internal/game/endless_run.go) |
| role | `escort` | 护送个人状态（24000–24020） | 原始JSON查看（read）；无可用重置/编辑 | 查看（模块数据页签）；重置/编辑待白名单审定 | 已接模块只读API/前端；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [escort.go](../internal/game/escort.go) |
| role | `firstcharge_*` | 四组首充窗口（21000/21001、21012–21014、21030–21033） | 原始JSON、逐窗口首充视图（read）；整窗重置（manage） | 已接查看/专用视图/白名单all重置 | 四个真实窗口后缀；重置删除该行且不发奖，后续领取仍受原规则约束 | [firstcharge.go](../internal/game/firstcharge.go) |
| role | `fortunebag` | 福袋翻牌（28300–28308） | 原始JSON查看（read）；无可用重置/编辑 | 查看（模块数据页签）；重置/编辑待白名单审定 | 已接模块只读API/前端；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [fortunebag.go](../internal/game/fortunebag.go) |
| role | `growway` | 成长之路（25830/25832） | 原始JSON查看（read）；无可用重置/编辑 | 查看（模块数据页签）；重置/编辑待白名单审定 | 已接模块只读API/前端；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [growway.go](../internal/game/growway.go) |
| role | `guildmarket` | 公会宝库个人凭证（26900–26903） | 原始JSON查看（read）；无可用重置/编辑 | 查看（模块数据页签）；重置/编辑待白名单审定 | 已接模块只读API/前端；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [guildmarket.go](../internal/game/guildmarket.go) |
| role | `halidom` | 元素信念（22200–22205） | 原始JSON查看（read）；无可用重置/编辑 | 查看（模块数据页签）；重置/编辑待白名单审定 | 已接模块只读API/前端；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [halidom.go](../internal/game/halidom.go) |
| role | `hallows` | 神技（24100–24135） | 原始JSON查看（read）；无可用重置/编辑 | 查看（模块数据页签）；重置/编辑待白名单审定 | 已接模块只读API/前端；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [hallows.go](../internal/game/hallows.go) |
| role | `heaven` | 天界（25200–25232） | 原始JSON查看（read）；无可用重置/编辑 | 查看（模块数据页签）；重置/编辑待白名单审定 | 已接模块只读API/前端；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [heaven.go](../internal/game/heaven.go) |
| role | `heroexpedit` | 英雄远征（24400–24415） | 原始JSON查看（read）；无可用重置/编辑 | 查看（模块数据页签）；重置/编辑待白名单审定 | 已接模块只读API/前端；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [expedition.go](../internal/game/expedition.go)、[expedition_help.go](../internal/game/expedition_help.go) |
| role | `holelite` | 节日精英召唤/新春赐福（23220–23222） | 原始JSON查看（read）；无可用重置/编辑 | 查看（模块数据页签）；重置/编辑待白名单审定 | 已接模块只读API/前端；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [holiday_elite.go](../internal/game/holiday_elite.go) |
| role | `holyeqm` | 神装实例/洗练/方案（11086–11094、25220–25224） | 原始JSON查看（read）；无可用重置/编辑 | 查看（模块数据页签）；重置/编辑待白名单审定 | 已接模块只读API/前端；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [holyeqm.go](../internal/store/holyeqm.go) |
| role | `homepet` | 家园宠物旅行（26100–26113） | 原始JSON查看（read）；无可用重置/编辑 | 查看（模块数据页签）；重置/编辑待白名单审定 | 已接模块只读API/前端；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [homepet.go](../internal/game/homepet.go) |
| role | `homeworld` | 家园个人状态（26001–26021） | 原始JSON查看（read）；无可用重置/编辑 | 查看（模块数据页签）；重置/编辑待白名单审定 | 已接模块只读API/前端；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [homeworld.go](../internal/game/homeworld.go)、[yearmonster.go](../internal/game/yearmonster.go) |
| role | `horcrux` | 魂器觉醒/强化（11402–11405） | 原始JSON查看（read）；无可用重置/编辑 | 查看（模块数据页签）；重置/编辑待白名单审定 | 已接模块只读API/前端；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [horcrux.go](../internal/store/horcrux.go) |
| role | `ladder` | 天梯（24300–24318） | 原始JSON查看（read）；无可用重置/编辑 | 查看（模块数据页签）；重置/编辑待白名单审定 | 已接模块只读API/前端；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [ladder.go](../internal/game/ladder.go) |
| role | `levgift` | 升级礼包/全服限量（21200/21201） | 原始JSON查看（read）；无可用重置/编辑 | 查看（模块数据页签）；重置/编辑待白名单审定 | 已接模块只读API/前端；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [levgift.go](../internal/game/levgift.go) |
| role | `limitexercise` | 试炼之境（25410–25414） | 原始JSON查看（read）；无可用重置/编辑 | 查看（模块数据页签）；重置/编辑待白名单审定 | 已接模块只读API/前端；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [limitexercise.go](../internal/game/limitexercise.go) |
| role | `limitgift` | 养成/等级限时礼包（21210/21211） | 原始JSON、23项礼包状态视图（read）；配置内ID重置（manage） | 已接查看/专用视图/单礼包重置 | 白名单从23项配置生成；4项Kind3仍不发售，清记录不解锁原本未实现的玩法 | [limitgift.go](../internal/game/limitgift.go) |
| role | `loginday` | 登录天数/登录奖励（10922–10925、21100/21101） | 原始JSON查看（read）；无可用重置/编辑 | 查看（模块数据页签）；重置/编辑待白名单审定 | 已接模块只读API/前端；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [loginday.go](../internal/game/loginday.go) |
| role | `luckydog` | 幸运锦鲤（28400–28403） | 原始JSON查看（read）；无可用重置/编辑 | 查看（模块数据页签）；重置/编辑待白名单审定 | 已接模块只读API/前端；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [luckydog.go](../internal/game/luckydog.go) |
| role | `mergegoal` | 合服目标（27300/27301） | 原始JSON查看（read）；无可用重置/编辑 | 查看（模块数据页签）；重置/编辑待白名单审定 | 已接模块只读API/前端；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [mergegoal.go](../internal/game/mergegoal.go) |
| role | `monopoly` | 和之国火祭个人状态（27400–27506） | 原始JSON查看（read）；无可用重置/编辑 | 查看（模块数据页签）；重置/编辑待白名单审定 | 已接模块只读API/前端；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [monopoly.go](../internal/game/monopoly.go) |
| role | `monthcard` | 两档月卡（16705–16708） | 原始JSON、两档月卡视图（read）；0/1单卡重置（manage） | 已接查看/专用视图/单卡重置 | 只清指定卡Start/Last/Days，Base=当前实付分；这是未获原服确认的管理策略 | [monthcard.go](../internal/game/monthcard.go)、[recruithero.go](../internal/game/recruithero.go) |
| role | `monthfund` | 两档月基金（24700–24702） | 原始JSON、两档基金视图（read）；0/1单基金重置（manage） | 已接查看/专用视图/单基金重置 | 只清指定基金Start/Last；不改实付和订单，需要新付款激活 | [charge_proto.go](../internal/game/charge_proto.go)、[monthfund.go](../internal/game/monthfund.go) |
| role | `newwarorder` | 海贼夺宝（28700–28708） | 原始JSON查看（read）；无可用重置/编辑 | 查看（模块数据页签）；重置/编辑待白名单审定 | 已接模块只读API/前端；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [newwarorder.go](../internal/game/newwarorder.go) |
| role | `onecentgift` | 一元/一分钱礼包（16651/16652） | 原始JSON查看（read）；无可用重置/编辑 | 查看（模块数据页签）；重置/编辑待白名单审定 | 已接模块只读API/前端；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [onecentgift.go](../internal/game/onecentgift.go) |
| role | `peakformations` | 巅峰冠军赛三队阵容（27725/27726） | 原始JSON查看（read）；无可用重置/编辑 | 查看（模块数据页签）；重置/编辑待白名单审定 | 存在独立保存状态，赛事关闭不等于无数据；不启用未实现赛程 | [cross_closed.go](../internal/game/cross_closed.go) |
| role | `petard` | 花火大会（27000–27008） | 原始JSON查看（read）；无可用重置/编辑 | 查看（模块数据页签）；重置/编辑待白名单审定 | 已接模块只读API/前端；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [petard.go](../internal/game/petard.go) |
| role | `privilege` | 特权（24500/24501/24502） | 原始JSON查看（read）；无可用重置/编辑 | 查看（模块数据页签）；重置/编辑待白名单审定 | 已接模块只读API/前端；改到期时间会让已发奖励和有效期对不上，不默认开放 | [privilege.go](../internal/game/privilege.go) |
| role | `planes` | 秘境（23100–23123） | 原始JSON查看（read）；无可用重置/编辑 | 查看（模块数据页签）；重置/编辑待白名单审定 | 已接模块只读API/前端；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [planes.go](../internal/game/planes.go) |
| role | `planesafk` | 长环岛冒险（28600–28626） | 原始JSON查看（read）；无可用重置/编辑 | 查看（模块数据页签）；重置/编辑待白名单审定 | 已接模块只读API/前端；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [planesafk.go](../internal/game/planesafk.go) |
| role | `practisetower` | 兔丼监狱个人进度（29100–29107） | 原始JSON查看（read）；无可用重置/编辑 | 查看（模块数据页签）；重置/编辑待白名单审定 | 已接模块只读API/前端；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [practisetower.go](../internal/game/practisetower.go) |
| role | `presage` | 预知召唤/松果宝藏（16690–16694） | 原始JSON查看（read）；无可用重置/编辑 | 查看（模块数据页签）；重置/编辑待白名单审定 | 已接模块只读API/前端；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [presage.go](../internal/game/presage.go) |
| role | `primus` | 霸气修炼（20700–20706） | 原始JSON查看（read）；无可用重置/编辑 | 查看（模块数据页签）；重置/编辑待白名单审定 | 已接模块只读API/前端；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [primus.go](../internal/game/primus.go) |
| role | `recruithero` | 新人招募（25100/25101） | 原始JSON查看（read）；无可用重置/编辑 | 查看（模块数据页签）；重置/编辑待白名单审定 | 已接模块只读API/前端；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [recruithero.go](../internal/game/recruithero.go) |
| role | `redbagday` | 公会红包每日领取（13534–13546） | 原始JSON查看（read）；无可用重置/编辑 | 查看（模块数据页签）；重置/编辑待白名单审定 | 已接模块只读API/前端；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [guild_redbag.go](../internal/game/guild_redbag.go) |
| role | `returnaction` | 回归活动（27900–27915） | 原始JSON查看（read）；无可用重置/编辑 | 查看（模块数据页签）；重置/编辑待白名单审定 | 已接模块只读API/前端；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [returnaction.go](../internal/game/returnaction.go) |
| role | `role_power` | 角色历史战力峰值（10306） | 原始JSON查看（read）；无重置/编辑 | 已接只读模块页签 | 卸下神装保留历史峰值，不开放修改峰值的写权限 | [role_power.go](../internal/game/role_power.go) |
| role | `sandybeach` | 沙滩Boss（25400–25405） | 原始JSON查看（read）；无可用重置/编辑 | 查看（模块数据页签）；重置/编辑待白名单审定 | 已接模块只读API/前端；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [sandybeach.go](../internal/game/sandybeach.go) |
| role | `sevengoal` | 七日目标（13604–13609） | 原始JSON查看（read）；无可用重置/编辑 | 查看（模块数据页签）；重置/编辑待白名单审定 | 已接模块只读API/前端；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [sevengoal.go](../internal/game/sevengoal.go) |
| role | `skinlottery` | 世界征兵（26600–26602） | 原始JSON查看（read）；无可用重置/编辑 | 查看（模块数据页签）；重置/编辑待白名单审定 | 已接模块只读API/前端；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [skinlottery.go](../internal/game/skinlottery.go) |
| role | `smashegg` | 砸金蛋个人状态（16680–16685） | 原始JSON查看（read）；无可用重置/编辑 | 查看（模块数据页签）；重置/编辑待白名单审定 | 已接模块只读API/前端；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [smashegg.go](../internal/game/smashegg.go) |
| role | `snatch` | 节日夺宝（25700–25704、26300/26301） | 原始JSON查看（read）；无可用重置/编辑 | 查看（模块数据页签）；重置/编辑待白名单审定 | 已接模块只读API/前端；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [snatch.go](../internal/game/snatch.go) |
| role | `stoneshop` | 神秘商店（16688/16689） | 原始JSON查看（read）；无可用重置/编辑 | 查看（模块数据页签）；重置/编辑待白名单审定 | 已接模块只读API/前端；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [stoneshop.go](../internal/game/stoneshop.go) |
| role | `sweet` | 甜蜜大作战（28500–28502） | 原始JSON查看（read）；无可用重置/编辑 | 查看（模块数据页签）；重置/编辑待白名单审定 | 已接模块只读API/前端；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [sweet.go](../internal/game/sweet.go) |
| role | `termbegins` | 开学季（26700–26712） | 原始JSON查看（read）；无可用重置/编辑 | 查看（模块数据页签）；重置/编辑待白名单审定 | 已接模块只读API/前端；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [termbegins.go](../internal/game/termbegins.go) |
| role | `timecollect` | 定时领取（16697/16698） | 原始JSON查看（read）；无可用重置/编辑 | 查看（模块数据页签）；重置/编辑待白名单审定 | 已接模块只读API/前端；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [levgift.go](../internal/game/levgift.go) |
| role | `timesummon` | 限定召唤（23216–23219） | 原始JSON查看（read）；无可用重置/编辑 | 查看（模块数据页签）；重置/编辑待白名单审定 | 已接模块只读API/前端；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [time_summon.go](../internal/game/time_summon.go) |
| role | `titlelev` | 称号合成等级（23304） | 原始JSON查看（read）；无可用重置/编辑 | 查看（模块数据页签）；重置/编辑待白名单审定 | 已接模块只读API/前端；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [titlecompose.go](../internal/game/titlecompose.go) |
| role | `touchgold` | 点金手（23606/23607） | 原始JSON查看（read）；无可用重置/编辑 | 查看（模块数据页签）；重置/编辑待白名单审定 | 已接模块只读API/前端；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [touchgold.go](../internal/game/touchgold.go) |
| role | `trainingcamp` | 训练营（27600–27603） | 原始JSON查看（read）；无可用重置/编辑 | 查看（模块数据页签）；重置/编辑待白名单审定 | 已接模块只读API/前端；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [trainingcamp.go](../internal/game/trainingcamp.go) |
| role | `treasure` | 幸运/高级探宝（16637–16643） | 原始JSON查看（read）；无可用重置/编辑 | 查看（模块数据页签）；重置/编辑待白名单审定 | 已接模块只读API/前端；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [daysrank.go](../internal/game/daysrank.go)、[treasure.go](../internal/game/treasure.go) |
| role | `vedio` | 录像个人收藏/点赞（19901–19908、20034/20036） | 原始JSON查看（read）；无可用重置/编辑 | 查看（模块数据页签）；重置/编辑待白名单审定 | 已接模块只读API/前端；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [vedio.go](../internal/game/vedio.go) |
| role | `voyage` | 远航订单（23800–23806） | 原始JSON查看（read）；无可用重置/编辑 | 查看（模块数据页签）；重置/编辑待白名单审定 | 已接模块只读API/前端；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [voyage.go](../internal/game/voyage.go) |
| role | `warorder` | 战令（25300–25309） | 原始JSON查看（read）；无可用重置/编辑 | 查看（模块数据页签）；重置/编辑待白名单审定 | 已接模块只读API/前端；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [warorder.go](../internal/game/warorder.go) |
| role | `whiteday` | 女神试炼（28800–28803） | 原始JSON查看（read）；无可用重置/编辑 | 查看（模块数据页签）；重置/编辑待白名单审定 | 已接模块只读API/前端；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [whiteday.go](../internal/game/whiteday.go) |
| role | `yearmonster` | 年兽活动（28200–28224） | 原始JSON查看（read）；无可用重置/编辑 | 查看（模块数据页签）；重置/编辑待白名单审定 | 已接模块只读API/前端；自由编辑/整行删除会破坏进度和领奖约束，不能默认开放 | [yearmonster.go](../internal/game/yearmonster.go) |

## 全服模块（31项）

31项均已接网关所属服务锁内的原始JSON/状态投影读取；16项现有排名已绑定Rank。下列8个赛季状态项（两类冠军赛分列）已验证到期补结算、重复调用及原生滚动/定时器并发；其余23项不提供结算（405）。其中worldlev另有专用PUT覆盖入口，其余保持只读；不新造活动结算或发奖规则。

| 范围 | 模块键 | 中文名/对应窗口 | 当前后台能力 | 后续拟提供 | 缺口或限制原因 | 源码依据 |
| --- | --- | --- | --- | --- | --- | --- |
| global | `adventure_mine_world` | 空岛灵矿共享占领（20640–20660） | 网关服务锁内只读原始JSON及状态投影（read） | 查看（玩法状态页，经网关）；不提供自由编辑/强制结算 | 共享状态/记录须经服务锁读取；无已审定的强制结算白名单，不改玩法规则 | [adventure_mine.go](../internal/game/adventure_mine.go) |
| global | `arenachamp` | 本服冠军赛（20250–20282） | Gateway-owned read/season/rank; manage: due-only catch-up | Implemented in 3b; no early settlement | Shared native service lock; mail/state/audit in one admin transaction; repeat is idempotent | [arenachampion.go](../internal/game/arenachampion.go)、[arenachampion_view.go](../internal/game/arenachampion_view.go)、[adminops_settle.go](../internal/game/adminops_settle.go) |
| global | `arenamany` | 多人竞技场（29000–29035） | Gateway-owned read/season/rank; manage: due-only catch-up | Implemented in 3b; no early settlement | Shared native service lock; mail/state/audit in one admin transaction; repeat is idempotent | [arenamany.go](../internal/game/arenamany.go)、[adminops_settle.go](../internal/game/adminops_settle.go) |
| global | `arenateam` | 组队竞技场全服赛季（27200–27256） | Gateway-owned read/season/rank; manage: due-only catch-up | Implemented in 3b; no early settlement | Shared native service lock; mail/state/audit in one admin transaction; repeat is idempotent | [arenateam.go](../internal/game/arenateam.go)、[adminops_settle.go](../internal/game/adminops_settle.go) |
| global | `crossarena` | 跨服竞技场（25600–25618） | Gateway-owned read/season/rank; manage: due-only catch-up | Implemented in 3b; no early settlement | Shared native service lock; mail/state/audit in one admin transaction; repeat is idempotent | [arenachampion.go](../internal/game/arenachampion.go)、[crossarena.go](../internal/game/crossarena.go) |
| global | `crosschamp` | 跨服冠军赛（26200–26215） | Gateway-owned read/season/rank; manage: due-only catch-up | Implemented in 3b; no early settlement | Shared native service lock; mail/state/audit in one admin transaction; repeat is idempotent | [arenachampion.go](../internal/game/arenachampion.go)、[arenachampion_view.go](../internal/game/arenachampion_view.go)、[adminops_settle.go](../internal/game/adminops_settle.go) |
| global | `daysrank` | 七天排行（22700–22704） | Gateway-owned read/season/rank; manage: due-only catch-up | Implemented in 3b; no early settlement | Shared native service lock; mail/state/audit in one admin transaction; repeat is idempotent | [daysrank.go](../internal/game/daysrank.go)、[adminops_settle.go](../internal/game/adminops_settle.go) |
| global | `dial` | 转盘抽奖/记录（16670–16676） | 网关服务锁内只读原始JSON及状态投影（read） | 查看（玩法状态页，经网关）；不提供自由编辑/强制结算 | 共享状态/记录须经服务锁读取；无已审定的强制结算白名单，不改玩法规则 | [dial.go](../internal/game/dial.go) |
| global | `dramapass` | 推图通关录像/进度榜（13002、13015、13020） | 网关服务锁内只读原始JSON及状态投影（read） | 查看（玩法状态页，经网关）；不提供自由编辑/强制结算 | 共享状态/记录须经服务锁读取；无已审定的强制结算白名单，不改玩法规则 | [drama_pass.go](../internal/game/drama_pass.go) |
| global | `elitematch` | 精英段位赛（24900–24946） | Gateway-owned read/season/rank; manage: due-only catch-up | Implemented in 3b; no early settlement | Shared native service lock; mail/state/audit in one admin transaction; repeat is idempotent | [elitematch_season.go](../internal/game/elitematch_season.go) |
| global | `endless_rank` | 无尽试炼排行榜（23903/23907） | 网关服务锁内只读原始JSON及状态投影（read） | 查看（玩法状态页，经网关）；不提供自由编辑/强制结算 | 共享状态/记录须经服务锁读取；无已审定的强制结算白名单，不改玩法规则 | [endless.go](../internal/game/endless.go) |
| global | `escort_board` | 护送共享记录/劫掠（24000–24020） | 网关服务锁内只读原始JSON及状态投影（read） | 查看（玩法状态页，经网关）；不提供自由编辑/强制结算 | 共享状态/记录须经服务锁读取；无已审定的强制结算白名单，不改玩法规则 | [escort.go](../internal/game/escort.go) |
| global | `guildmarket:*` | 公会宝库共享库存（26900–26903） | 网关服务锁内只读原始JSON及状态投影（read） | 查看（玩法状态页，经网关）；不提供自由编辑/强制结算 | 共享状态/记录须经服务锁读取；无已审定的强制结算白名单，不改玩法规则 | [guildmarket.go](../internal/game/guildmarket.go) |
| global | `guildredbag` | 公会红包簿（13534–13546） | 网关服务锁内只读原始JSON及状态投影（read） | 查看（玩法状态页，经网关）；不提供自由编辑/强制结算 | 共享状态/记录须经服务锁读取；无已审定的强制结算白名单，不改玩法规则 | [guild_redbag.go](../internal/game/guild_redbag.go) |
| global | `guildwar` | 公会战（24200–24223） | Gateway-owned read/season/rank; manage: due-only catch-up | Implemented in 3b; no early settlement | Shared native service lock; mail/state/audit in one admin transaction; repeat is idempotent | [guildwar.go](../internal/game/guildwar.go)、[adminops_settle.go](../internal/game/adminops_settle.go) |
| global | `homeworld_board` | 家园共享人气榜（26020/26021） | 网关服务锁内只读原始JSON及状态投影（read） | 查看（玩法状态页，经网关）；不提供自由编辑/强制结算 | 共享状态/记录须经服务锁读取；无已审定的强制结算白名单，不改玩法规则 | [homeworld.go](../internal/game/homeworld.go) |
| global | `ladder` | 天梯（24300–24318） | 网关服务锁内只读原始JSON及状态投影（read） | 查看（玩法状态页，经网关）；不提供自由编辑/强制结算 | 共享状态/记录须经服务锁读取；无已审定的强制结算白名单，不改玩法规则 | [ladder.go](../internal/game/ladder.go) |
| global | `levgift` | 升级礼包/全服限量（21200/21201） | 网关服务锁内只读原始JSON及状态投影（read） | 查看（玩法状态页，经网关）；不提供自由编辑/强制结算 | 共享状态/记录须经服务锁读取；无已审定的强制结算白名单，不改玩法规则 | [levgift.go](../internal/game/levgift.go) |
| global | `monopoly_g*` | 和之国火祭公会状态（27400–27506） | 网关服务锁内只读原始JSON及状态投影（read） | 查看（玩法状态页，经网关）；不提供自由编辑/强制结算 | 共享状态/记录须经服务锁读取；无已审定的强制结算白名单，不改玩法规则 | [monopoly.go](../internal/game/monopoly.go) |
| global | `petard` | 花火大会（27000–27008） | 网关服务锁内只读原始JSON及状态投影（read） | 查看（玩法状态页，经网关）；不提供自由编辑/强制结算 | 共享状态/记录须经服务锁读取；无已审定的强制结算白名单，不改玩法规则 | [petard.go](../internal/game/petard.go) |
| global | `practisetower_rank` | 兔丼监狱共享记录/排行（29100–29107） | 网关服务锁内只读原始JSON及状态投影（read） | 查看（玩法状态页，经网关）；不提供自由编辑/强制结算 | 共享状态/记录须经服务锁读取；无已审定的强制结算白名单，不改玩法规则 | [practisetower.go](../internal/game/practisetower.go) |
| global | `primus` | 霸气修炼（20700–20706） | 网关服务锁内只读原始JSON及状态投影（read） | 查看（玩法状态页，经网关）；不提供自由编辑/强制结算 | 共享状态/记录须经服务锁读取；无已审定的强制结算白名单，不改玩法规则 | [primus.go](../internal/game/primus.go) |
| global | `returnaction` | 回归活动（27900–27915） | 网关服务锁内只读原始JSON及状态投影（read） | 查看（玩法状态页，经网关）；不提供自由编辑/强制结算 | 共享状态/记录须经服务锁读取；无已审定的强制结算白名单，不改玩法规则 | [returnaction.go](../internal/game/returnaction.go) |
| global | `sandybeach` | 沙滩Boss（25400–25405） | 网关服务锁内只读原始JSON及状态投影（read） | 查看（玩法状态页，经网关）；不提供自由编辑/强制结算 | 共享状态/记录须经服务锁读取；无已审定的强制结算白名单，不改玩法规则 | [sandybeach.go](../internal/game/sandybeach.go) |
| global | `smashegg_log` | 砸金蛋共享记录（16684/16685） | 网关服务锁内只读原始JSON及状态投影（read） | 查看（玩法状态页，经网关）；不提供自由编辑/强制结算 | 共享状态/记录须经服务锁读取；无已审定的强制结算白名单，不改玩法规则 | [smashegg.go](../internal/game/smashegg.go) |
| global | `snatch` | 节日夺宝（25700–25704、26300/26301） | 网关服务锁内只读原始JSON及状态投影（read） | 查看（玩法状态页，经网关）；不提供自由编辑/强制结算 | 共享状态/记录须经服务锁读取；无已审定的强制结算白名单，不改玩法规则 | [snatch.go](../internal/game/snatch.go) |
| global | `sweet` | 甜蜜大作战（28500–28502） | 网关服务锁内只读原始JSON及状态投影（read） | 查看（玩法状态页，经网关）；不提供自由编辑/强制结算 | 共享状态/记录须经服务锁读取；无已审定的强制结算白名单，不改玩法规则 | [sweet.go](../internal/game/sweet.go) |
| global | `termbegins` | 开学季（26700–26712） | 网关服务锁内只读原始JSON及状态投影（read） | 查看（玩法状态页，经网关）；不提供自由编辑/强制结算 | 共享状态/记录须经服务锁读取；无已审定的强制结算白名单，不改玩法规则 | [termbegins.go](../internal/game/termbegins.go) |
| global | `treasure` | 幸运/高级探宝（16637–16643） | 网关服务锁内只读原始JSON及状态投影（read） | 查看（玩法状态页，经网关）；不提供自由编辑/强制结算 | 共享状态/记录须经服务锁读取；无已审定的强制结算白名单，不改玩法规则 | [treasure.go](../internal/game/treasure.go) |
| global | `yearmonster` | 年兽活动（28200–28224） | 网关服务锁内只读原始JSON及状态投影（read） | 查看（玩法状态页，经网关）；不提供自由编辑/强制结算 | 共享状态/记录须经服务锁读取；无已审定的强制结算白名单，不改玩法规则 | [yearmonster.go](../internal/game/yearmonster.go) |
| global | `worldlev` | World level override (10192/10905) | Gateway-owned read; manage PUT with reason/audit and online push | Implemented in phase 3c | 0 disables; until=0 persistent; expiry uses game clock; original top-10 average remains the fallback | [worldlevel.go](../internal/store/worldlevel.go) |

## 专用表与基础设施（92张）

表范围固定记为 `table`，标题标明业务归属。没有现成后台入口的游戏表均列为待接只读视图，不因阶段4只举了八类页签就从清单消失；下表也不授权新增通用SQL写入。多表事务、领奖幂等及服务锁必须保留，不能逐表清空。现有写入入口仅记录事实，不代表已符合新接口的离线写约束。

| 范围 | 表名 | 中文名/对应窗口 | 当前后台能力 | 后续拟提供 | 缺口或限制原因 | 源码依据 |
| --- | --- | --- | --- | --- | --- | --- |
| table | `accounts` | 游戏账号/账号页 | 账号页查看、封禁/解封（read/write）；有专用页：账号管理，避免暴露密码及第三方身份凭据 | 保留专用入口/不提供通用表视图 | 有专用页：账号管理，避免暴露密码及第三方身份凭据 | [001_init.sql](../internal/store/migrations/001_init.sql) |
| table | `activities` | 活动配置/活动页 | 活动页查看、新建/编辑/复制/删除（read/write）；表视图注册：只读白名单分页/total；role表含COUNT摘要（read） | 已接API及通用表浏览，不提供任意逐表编辑/清空 | 归属隔离、稳定主键排序；文本上限4KB并标记truncated | [048_activities.sql](../internal/store/migrations/048_activities.sql) |
| table | `admins` | 管理员账号/管理员页 | 管理员页查看、创建、角色/启停/改密（manage）；不提供：管理员凭据；沿用manage专用页 | 保留专用入口/不提供通用表视图 | 不提供：管理员凭据；沿用manage专用页 | [001_init.sql](../internal/store/migrations/001_init.sql) |
| table | `arena_claims` | 竞技场周宝箱领取 | 表视图注册：只读白名单分页/total；role表含COUNT摘要（read） | 已接API及通用表浏览，不提供任意逐表编辑/清空 | 归属隔离、稳定主键排序；文本上限4KB并标记truncated | [038_arena.sql](../internal/store/migrations/038_arena.sql) |
| table | `arena_logs` | 竞技场比赛记录 | 表视图注册：只读白名单分页/total；role表含COUNT摘要（read） | 已接API及通用表浏览，不提供任意逐表编辑/清空 | 归属隔离、稳定主键排序；文本上限4KB并标记truncated | [038_arena.sql](../internal/store/migrations/038_arena.sql) |
| table | `arena_players` | 竞技场积分/次数/赛季 | 表视图注册：只读白名单分页/total；role表含COUNT摘要（read） | 已接API及通用表浏览，不提供任意逐表编辑/清空 | 归属隔离、稳定主键排序；文本上限4KB并标记truncated | [038_arena.sql](../internal/store/migrations/038_arena.sql) |
| table | `arena_slots` | 竞技场对手列表 | 表视图注册：只读白名单分页/total；role表含COUNT摘要（read） | 已接API及通用表浏览，不提供任意逐表编辑/清空 | 归属隔离、稳定主键排序；文本上限4KB并标记truncated | [038_arena.sql](../internal/store/migrations/038_arena.sql) |
| table | `audit_logs` | 后台审计/审计页 | 审计页只读（manage）；有专用页：manage审计查询 | 保留专用入口/不提供通用表视图 | 有专用页：manage审计查询 | [001_init.sql](../internal/store/migrations/001_init.sql) |
| table | `chat_offline` | 离线聊天消息 | 表视图注册：只读白名单分页/total；role表含COUNT摘要（read） | 已接API及通用表浏览，不提供任意逐表编辑/清空 | 归属隔离、稳定主键排序；文本上限4KB并标记truncated；不返回正文 | [007_chat.sql](../internal/store/migrations/007_chat.sql) |
| table | `checkin` | 签到及补签 | 表视图注册：只读白名单分页/total；role表含COUNT摘要（read） | 已接API及通用表浏览，不提供任意逐表编辑/清空 | 归属隔离、稳定主键排序；文本上限4KB并标记truncated | [009_checkin.sql](../internal/store/migrations/009_checkin.sql) |
| table | `cmd_daily` | 每日协议使用次数 | 汇总为最常用的前 50 个协议（read）；有专用页：运营数据 | 保留专用入口/不提供通用表视图 | 有专用页：运营数据；只统计汇总，不逐角色展示 | [052_analytics.sql](../internal/store/migrations/052_analytics.sql) |
| table | `counters` | 成就累计计数 | 表视图注册：只读白名单分页/total；role表含COUNT摘要（read） | 已接API及通用表浏览，不提供任意逐表编辑/清空 | 归属隔离、稳定主键排序；文本上限4KB并标记truncated | [021_feats.sql](../internal/store/migrations/021_feats.sql) |
| table | `daily_quests` | 日常任务进度/领取 | 表视图注册：只读白名单分页/total；role表含COUNT摘要（read） | 已接API及通用表浏览，不提供任意逐表编辑/清空 | 归属隔离、稳定主键排序；文本上限4KB并标记truncated | [015_daily_quests.sql](../internal/store/migrations/015_daily_quests.sql) |
| table | `drama_daily` | 推图每日扫荡 | 表视图注册：只读白名单分页/total；role表含COUNT摘要（read） | 已接API及通用表浏览，不提供任意逐表编辑/清空 | 归属隔离、稳定主键排序；文本上限4KB并标记truncated | [017_drama_extras.sql](../internal/store/migrations/017_drama_extras.sql) |
| table | `drama_hook` | 挂机收益状态 | 表视图注册：只读白名单分页/total；role表含COUNT摘要（read） | 已接API及通用表浏览，不提供任意逐表编辑/清空 | 归属隔离、稳定主键排序；文本上限4KB并标记truncated | [017_drama_extras.sql](../internal/store/migrations/017_drama_extras.sql) |
| table | `drama_milestones` | 推图里程碑领取 | 表视图注册：只读白名单分页/total；role表含COUNT摘要（read） | 已接API及通用表浏览，不提供任意逐表编辑/清空 | 归属隔离、稳定主键排序；文本上限4KB并标记truncated | [018_drama_milestones.sql](../internal/store/migrations/018_drama_milestones.sql) |
| table | `elite_summon` | 精英召唤保底/积分 | 表视图注册：只读白名单分页/total；role表含COUNT摘要（read） | 已接API及通用表浏览，不提供任意逐表编辑/清空 | 归属隔离、稳定主键排序；文本上限4KB并标记truncated | [027_elite_summon.sql](../internal/store/migrations/027_elite_summon.sql) |
| table | `events` | 登录/进入/下线事件 | 汇总为日活、登录账号、在线时长、留存（read）；有专用页：运营数据 | 保留专用入口/不提供通用表视图 | 有专用页：运营数据；只统计汇总，不逐条展示玩家行为 | [052_analytics.sql](../internal/store/migrations/052_analytics.sql) |
| table | `feats_claimed` | 成就领取记录 | 表视图注册：只读白名单分页/total；role表含COUNT摘要（read） | 已接API及通用表浏览，不提供任意逐表编辑/清空 | 归属隔离、稳定主键排序；文本上限4KB并标记truncated | [021_feats.sql](../internal/store/migrations/021_feats.sql) |
| table | `forge_logs` | 锻造屋合成记录 | 表视图注册：只读白名单分页/total；role表含COUNT摘要（read） | 已接API及通用表浏览，不提供任意逐表编辑/清空 | 归属隔离、稳定主键排序；文本上限4KB并标记truncated | [044_forge_logs.sql](../internal/store/migrations/044_forge_logs.sql) |
| table | `formation_slots` | 各玩法阵容槽位 | 表视图注册：只读白名单分页/total；role表含COUNT摘要（read） | 已接API及通用表浏览，不提供任意逐表编辑/清空 | 归属隔离、稳定主键排序；文本上限4KB并标记truncated | [020_formations.sql](../internal/store/migrations/020_formations.sql) |
| table | `formations` | 各玩法阵容主记录 | 表视图注册：只读白名单分页/total；role表含COUNT摘要（read） | 已接API及通用表浏览，不提供任意逐表编辑/清空 | 归属隔离、稳定主键排序；文本上限4KB并标记truncated | [020_formations.sql](../internal/store/migrations/020_formations.sql) |
| table | `friend_blacklist` | 好友黑名单 | 表视图注册：只读白名单分页/total；role表含COUNT摘要（read） | 已接API及通用表浏览，不提供任意逐表编辑/清空 | 归属隔离、稳定主键排序；文本上限4KB并标记truncated | [047_friend_blacklist.sql](../internal/store/migrations/047_friend_blacklist.sql) |
| table | `friend_gifts` | 好友赠礼/领取 | 表视图注册：只读白名单分页/total；role表含COUNT摘要（read） | 已接API及通用表浏览，不提供任意逐表编辑/清空 | 归属隔离、稳定主键排序；文本上限4KB并标记truncated | [011_friend_gifts.sql](../internal/store/migrations/011_friend_gifts.sql) |
| table | `friend_requests` | 好友申请 | 表视图注册：只读白名单分页/total；role表含COUNT摘要（read） | 已接API及通用表浏览，不提供任意逐表编辑/清空 | 归属隔离、稳定主键排序；文本上限4KB并标记truncated | [006_friends.sql](../internal/store/migrations/006_friends.sql) |
| table | `friends` | 好友关系 | 表视图注册：只读白名单分页/total；role表含COUNT摘要（read） | 已接API及通用表浏览，不提供任意逐表编辑/清空 | 归属隔离、稳定主键排序；文本上限4KB并标记truncated | [006_friends.sql](../internal/store/migrations/006_friends.sql) |
| table | `global_state` | 全服模块JSON容器 | 有专用页：网关全服模块API | 保留专用入口/不提供通用表视图 | 有专用页：网关全服模块API | [045_module_state.sql](../internal/store/migrations/045_module_state.sql) |
| table | `guild_applies` | 公会申请 | 表视图注册：只读白名单分页/total；role表含COUNT摘要（read） | 已接API及通用表浏览，不提供任意逐表编辑/清空 | 归属隔离、稳定主键排序；文本上限4KB并标记truncated | [036_guild.sql](../internal/store/migrations/036_guild.sql) |
| table | `guild_boss_dmg` | 公会Boss伤害记录 | 表视图注册：只读白名单分页/total；role表含COUNT摘要（read） | 已接API及通用表浏览，不提供任意逐表编辑/清空 | 归属隔离、稳定主键排序；文本上限4KB并标记truncated | [039_guild_boss.sql](../internal/store/migrations/039_guild_boss.sql) |
| table | `guild_boss_kills` | 公会Boss击杀记录 | 表视图注册：只读白名单分页/total；role表含COUNT摘要（read） | 已接API及通用表浏览，不提供任意逐表编辑/清空 | 归属隔离、稳定主键排序；文本上限4KB并标记truncated | [039_guild_boss.sql](../internal/store/migrations/039_guild_boss.sql) |
| table | `guild_boss_roles` | 公会Boss个人次数 | 表视图注册：只读白名单分页/total；role表含COUNT摘要（read） | 已接API及通用表浏览，不提供任意逐表编辑/清空 | 归属隔离、稳定主键排序；文本上限4KB并标记truncated | [039_guild_boss.sql](../internal/store/migrations/039_guild_boss.sql) |
| table | `guild_boss_state` | 公会Boss共享血量/进度 | 表视图注册：只读白名单分页/total；role表含COUNT摘要（read） | 已接API及通用表浏览，不提供任意逐表编辑/清空 | 归属隔离、稳定主键排序；文本上限4KB并标记truncated | [039_guild_boss.sql](../internal/store/migrations/039_guild_boss.sql) |
| table | `guild_box_claims` | 公会捐献宝箱领取 | 表视图注册：只读白名单分页/total；role表含COUNT摘要（read） | 已接API及通用表浏览，不提供任意逐表编辑/清空 | 归属隔离、稳定主键排序；文本上限4KB并标记truncated | [036_guild.sql](../internal/store/migrations/036_guild.sql) |
| table | `guild_cooldowns` | 退会/会长冷却 | 表视图注册：只读白名单分页/total；role表含COUNT摘要（read） | 已接API及通用表浏览，不提供任意逐表编辑/清空 | 归属隔离、稳定主键排序；文本上限4KB并标记truncated | [036_guild.sql](../internal/store/migrations/036_guild.sql) |
| table | `guild_logs` | 公会日志 | 表视图注册：只读白名单分页/total；role表含COUNT摘要（read） | 已接API及通用表浏览，不提供任意逐表编辑/清空 | 归属隔离、稳定主键排序；文本上限4KB并标记truncated | [036_guild.sql](../internal/store/migrations/036_guild.sql) |
| table | `guild_members` | 公会成员/职位/捐献 | 公会列表仅人数/会长，未逐成员展示（read）；表视图注册：只读白名单分页/total；role表含COUNT摘要（read） | 已接API及通用表浏览，不提供任意逐表编辑/清空 | 归属隔离、稳定主键排序；文本上限4KB并标记truncated | [036_guild.sql](../internal/store/migrations/036_guild.sql) |
| table | `guild_secret` | 公会秘境共享进度 | 表视图注册：只读白名单分页/total；role表含COUNT摘要（read） | 已接API及通用表浏览，不提供任意逐表编辑/清空 | 归属隔离、稳定主键排序；文本上限4KB并标记truncated | [041_guild_secret.sql](../internal/store/migrations/041_guild_secret.sql) |
| table | `guild_secret_claims` | 公会秘境奖励领取 | 表视图注册：只读白名单分页/total；role表含COUNT摘要（read） | 已接API及通用表浏览，不提供任意逐表编辑/清空 | 归属隔离、稳定主键排序；文本上限4KB并标记truncated | [041_guild_secret.sql](../internal/store/migrations/041_guild_secret.sql) |
| table | `guild_secret_dmg` | 公会秘境伤害记录 | 表视图注册：只读白名单分页/total；role表含COUNT摘要（read） | 已接API及通用表浏览，不提供任意逐表编辑/清空 | 归属隔离、稳定主键排序；文本上限4KB并标记truncated | [041_guild_secret.sql](../internal/store/migrations/041_guild_secret.sql) |
| table | `guild_secret_roles` | 公会秘境个人次数 | 表视图注册：只读白名单分页/total；role表含COUNT摘要（read） | 已接API及通用表浏览，不提供任意逐表编辑/清空 | 归属隔离、稳定主键排序；文本上限4KB并标记truncated | [041_guild_secret.sql](../internal/store/migrations/041_guild_secret.sql) |
| table | `guild_skill_flags` | 公会技能标记 | 表视图注册：只读白名单分页/total；role表含COUNT摘要（read） | 已接API及通用表浏览，不提供任意逐表编辑/清空 | 归属隔离、稳定主键排序；文本上限4KB并标记truncated | [037_guild_skill.sql](../internal/store/migrations/037_guild_skill.sql) |
| table | `guild_skill_pve` | 公会PvE技能 | 表视图注册：只读白名单分页/total；role表含COUNT摘要（read） | 已接API及通用表浏览，不提供任意逐表编辑/清空 | 归属隔离、稳定主键排序；文本上限4KB并标记truncated | [037_guild_skill.sql](../internal/store/migrations/037_guild_skill.sql) |
| table | `guild_skill_pvp` | 公会PvP技能 | 表视图注册：只读白名单分页/total；role表含COUNT摘要（read） | 已接API及通用表浏览，不提供任意逐表编辑/清空 | 归属隔离、稳定主键排序；文本上限4KB并标记truncated | [037_guild_skill.sql](../internal/store/migrations/037_guild_skill.sql) |
| table | `guilds` | 公会/公会页 | 公会列表/详情及解散（read/write）；表视图注册：只读白名单分页/total；role表含COUNT摘要（read） | 已接API及通用表浏览，不提供任意逐表编辑/清空 | 归属隔离、稳定主键排序；文本上限4KB并标记truncated | [036_guild.sql](../internal/store/migrations/036_guild.sql) |
| table | `hero_equips` | 英雄普通装备槽位 | 表视图注册：只读白名单分页/total；role表含COUNT摘要（read） | 已接API及通用表浏览，不提供任意逐表编辑/清空 | 归属隔离、稳定主键排序；文本上限4KB并标记truncated | [019_hero_equips.sql](../internal/store/migrations/019_hero_equips.sql) |
| table | `invite_binds` | 邀请绑定 | 表视图注册：只读白名单分页/total；role表含COUNT摘要（read） | 已接API及通用表浏览，不提供任意逐表编辑/清空 | 归属隔离、稳定主键排序；文本上限4KB并标记truncated | [010_invite.sql](../internal/store/migrations/010_invite.sql) |
| table | `invite_claims` | 邀请奖励领取 | 表视图注册：只读白名单分页/total；role表含COUNT摘要（read） | 已接API及通用表浏览，不提供任意逐表编辑/清空 | 归属隔离、稳定主键排序；文本上限4KB并标记truncated | [010_invite.sql](../internal/store/migrations/010_invite.sql) |
| table | `invite_codes` | 邀请码 | 表视图注册：只读白名单分页/total；role表含COUNT摘要（read） | 已接API及通用表浏览，不提供任意逐表编辑/清空 | 归属隔离、稳定主键排序；文本上限4KB并标记truncated | [010_invite.sql](../internal/store/migrations/010_invite.sql) |
| table | `login_tokens` | 玩家登录凭据 | 不提供：登录凭据 | 保留专用入口/不提供通用表视图 | 不提供：登录凭据 | [001_init.sql](../internal/store/migrations/001_init.sql) |
| table | `mail_attachments` | 邮件附件/邮件页 | 全局发件历史附件及领取汇总（read）；表视图注册：只读白名单分页/total；role表含COUNT摘要（read） | 已接API及通用表浏览，不提供任意逐表编辑/清空 | 归属隔离、稳定主键排序；文本上限4KB并标记truncated | [001_init.sql](../internal/store/migrations/001_init.sql) |
| table | `mails` | 邮件主记录/邮件页 | 发件历史、详情及发送（read/write）；表视图注册：只读白名单分页/total；role表含COUNT摘要（read） | 已接API及通用表浏览，不提供任意逐表编辑/清空 | 归属隔离、稳定主键排序；文本上限4KB并标记truncated | [001_init.sql](../internal/store/migrations/001_init.sql) |
| table | `module_state` | 玩家模块JSON容器 | 已有状态列表/原始JSON查看（read）；生产重置白名单为空；有专用页：玩家模块API | 保留专用入口/不提供通用表视图 | 有专用页：玩家模块API | [045_module_state.sql](../internal/store/migrations/045_module_state.sql) |
| table | `notices` | 公告/公告页 | 公告页查看及增改删（read/write）；表视图注册：只读白名单分页/total；role表含COUNT摘要（read） | 已接API及通用表浏览，不提供任意逐表编辑/清空 | 归属隔离、稳定主键排序；文本上限4KB并标记truncated | [001_init.sql](../internal/store/migrations/001_init.sql) |
| table | `online_gift_claims` | 在线奖励领取 | 表视图注册：只读白名单分页/total；role表含COUNT摘要（read） | 已接API及通用表浏览，不提供任意逐表编辑/清空 | 归属隔离、稳定主键排序；文本上限4KB并标记truncated | [008_online_gift.sql](../internal/store/migrations/008_online_gift.sql) |
| table | `online_samples` | 每分钟在线人数采样 | 汇总为每日在线峰值（read）；有专用页：运营数据 | 保留专用入口/不提供通用表视图 | 有专用页：运营数据；只统计汇总 | [052_analytics.sql](../internal/store/migrations/052_analytics.sql) |
| table | `online_time` | 累计在线时长 | 表视图注册：只读白名单分页/total；role表含COUNT摘要（read） | 已接API及通用表浏览，不提供任意逐表编辑/清空 | 归属隔离、稳定主键排序；文本上限4KB并标记truncated | [008_online_gift.sql](../internal/store/migrations/008_online_gift.sql) |
| table | `partner_changes` | 英雄置换预览 | 表视图注册：只读白名单分页/total；role表含COUNT摘要（read） | 已接API及通用表浏览，不提供任意逐表编辑/清空 | 归属隔离、稳定主键排序；文本上限4KB并标记truncated | [025_partner_changes.sql](../internal/store/migrations/025_partner_changes.sql) |
| table | `partner_talents` | 英雄天赋 | 表视图注册：只读白名单分页/total；role表含COUNT摘要（read） | 已接API及通用表浏览，不提供任意逐表编辑/清空 | 归属隔离、稳定主键排序；文本上限4KB并标记truncated | [029_partner_talents.sql](../internal/store/migrations/029_partner_talents.sql) |
| table | `partners` | 英雄实例/属性/培养 | 仅发放新英雄（write），无拥有列表/属性视图；表视图注册：只读白名单分页/total；role表含COUNT摘要（read） | 已接API及通用表浏览，不提供任意逐表编辑/清空 | 归属隔离、稳定主键排序；文本上限4KB并标记truncated | [012_partners.sql](../internal/store/migrations/012_partners.sql) |
| table | `pay_config` | 支付收款配置/充值页 | 收款码/说明查看、修改（read/manage）；有专用页：支付配置 | 保留专用入口/不提供通用表视图 | 有专用页：支付配置 | [050_pay.sql](../internal/store/migrations/050_pay.sql) |
| table | `pay_orders` | 充值订单/充值页 | 全局订单及角色充值汇总的真实分页订单（read）；创建/确认/取消（manage）；有专用页：角色charge接口真实分页订单及全局支付页 | 保留专用入口/不提供通用表视图 | 有专用页：角色charge接口真实分页订单及全局支付页 | [050_pay.sql](../internal/store/migrations/050_pay.sql) |
| table | `pay_proofs` | 付款截图 | 订单表“查看截图”按订单号取单张图（read）；有专用页：支付页 | 保留专用入口/不提供通用表视图 | 有专用页：支付页；图片是二进制，不适合表浏览，玩家自传不能当收款证据 | [051_pay_proof.sql](../internal/store/migrations/051_pay_proof.sql) |
| table | `redeem_codes` | 兑换码配置/兑换码页 | 列表/创建/删除及已用次数（read/write）；有专用页：兑换码管理 | 保留专用入口/不提供通用表视图 | 有专用页：兑换码管理 | [035_role_misc.sql](../internal/store/migrations/035_role_misc.sql) |
| table | `redeem_uses` | 兑换码领取明细 | 仅兑换码已用次数汇总，无逐角色明细（read）；表视图注册：只读白名单分页/total；role表含COUNT摘要（read） | 已接API及通用表浏览，不提供任意逐表编辑/清空 | 归属隔离、稳定主键排序；文本上限4KB并标记truncated | [035_role_misc.sql](../internal/store/migrations/035_role_misc.sql) |
| table | `replay_collects` | 录像收藏关系 | 表视图注册：只读白名单分页/total；role表含COUNT摘要（read） | 已接API及通用表浏览，不提供任意逐表编辑/清空 | 归属隔离、稳定主键排序；文本上限4KB并标记truncated | [046_replays.sql](../internal/store/migrations/046_replays.sql) |
| table | `replays` | 战斗录像元数据/脚本 | 表视图注册：只读白名单分页/total；role表含COUNT摘要（read） | 已接API及通用表浏览，不提供任意逐表编辑/清空 | 归属隔离、稳定主键排序；文本上限4KB并标记truncated；不返回战斗脚本 | [046_replays.sql](../internal/store/migrations/046_replays.sql) |
| table | `resonance_extract` | 共鸣炼金批次/领取 | 表视图注册：只读白名单分页/total；role表含COUNT摘要（read） | 已接API及通用表浏览，不提供任意逐表编辑/清空 | 归属隔离、稳定主键排序；文本上限4KB并标记truncated | [034_resonance_stone.sql](../internal/store/migrations/034_resonance_stone.sql) |
| table | `resonance_slots` | 共鸣槽位/冷却 | 表视图注册：只读白名单分页/total；role表含COUNT摘要（read） | 已接API及通用表浏览，不提供任意逐表编辑/清空 | 归属隔离、稳定主键排序；文本上限4KB并标记truncated | [033_resonance.sql](../internal/store/migrations/033_resonance.sql) |
| table | `role_assets` | 货币账本/角色详情 | 角色详情账本查看、发放（read/write）；表视图注册：只读白名单分页/total；role表含COUNT摘要（read） | 已接API及通用表浏览，不提供任意逐表编辑/清空 | 归属隔离、稳定主键排序；文本上限4KB并标记truncated | [003_mail.sql](../internal/store/migrations/003_mail.sql) |
| table | `role_badge_use` | 当前徽章 | 表视图注册：只读白名单分页/total；role表含COUNT摘要（read） | 已接API及通用表浏览，不提供任意逐表编辑/清空 | 归属隔离、稳定主键排序；文本上限4KB并标记truncated | [040_space.sql](../internal/store/migrations/040_space.sql) |
| table | `role_badges` | 徽章获得记录 | 表视图注册：只读白名单分页/total；role表含COUNT摘要（read） | 已接API及通用表浏览，不提供任意逐表编辑/清空 | 归属隔离、稳定主键排序；文本上限4KB并标记truncated | [040_space.sql](../internal/store/migrations/040_space.sql) |
| table | `role_charge` | 累计实付金额/VIP经验/商品购买计数 | 角色充值汇总只读（read）；表视图注册：只读白名单分页/total；role表含COUNT摘要（read） | 已接API及通用表浏览，不提供任意逐表编辑/清空 | 归属隔离、稳定主键排序；文本上限4KB并标记truncated | [050_pay.sql](../internal/store/migrations/050_pay.sql) |
| table | `role_drama` | 主线推图进度 | 表视图注册：只读白名单分页/total；role表含COUNT摘要（read） | 已接API及通用表浏览，不提供任意逐表编辑/清空 | 归属隔离、稳定主键排序；文本上限4KB并标记truncated | [002_role_drama.sql](../internal/store/migrations/002_role_drama.sql) |
| table | `role_follows` | 空间关注关系 | 表视图注册：只读白名单分页/total；role表含COUNT摘要（read） | 已接API及通用表浏览，不提供任意逐表编辑/清空 | 归属隔离、稳定主键排序；文本上限4KB并标记truncated | [040_space.sql](../internal/store/migrations/040_space.sql) |
| table | `role_items` | 物品账本/角色详情 | 角色详情账本查看、发放（read/write）；表视图注册：只读白名单分页/total；role表含COUNT摘要（read） | 已接API及通用表浏览，不提供任意逐表编辑/清空 | 归属隔离、稳定主键排序；文本上限4KB并标记truncated | [003_mail.sql](../internal/store/migrations/003_mail.sql) |
| table | `role_looks` | 外观拥有记录 | 仅发放外观（write），无拥有列表；表视图注册：只读白名单分页/total；role表含COUNT摘要（read） | 已接API及通用表浏览，不提供任意逐表编辑/清空 | 归属隔离、稳定主键排序；文本上限4KB并标记truncated | [032_role_looks.sql](../internal/store/migrations/032_role_looks.sql) |
| table | `role_mails` | 角色收件/阅读/领取 | 仅发件历史收件/领取汇总，无角色收件列表（read）；表视图注册：只读白名单分页/total；role表含COUNT摘要（read） | 已接API及通用表浏览，不提供任意逐表编辑/清空 | 归属隔离、稳定主键排序；文本上限4KB并标记truncated | [001_init.sql](../internal/store/migrations/001_init.sql) |
| table | `role_notice_reads` | 角色公告已读 | 表视图注册：只读白名单分页/total；role表含COUNT摘要（read） | 已接API及通用表浏览，不提供任意逐表编辑/清空 | 归属隔离、稳定主键排序；文本上限4KB并标记truncated | [004_notice_reads.sql](../internal/store/migrations/004_notice_reads.sql) |
| table | `role_reports` | 玩家举报/举报页 | 举报页只读（read）；表视图注册：只读白名单分页/total；role表含COUNT摘要（read） | 已接API及通用表浏览，不提供任意逐表编辑/清空 | 归属隔离、稳定主键排序；文本上限4KB并标记truncated | [035_role_misc.sql](../internal/store/migrations/035_role_misc.sql) |
| table | `role_resonance` | 共鸣核心/石碑等级 | 表视图注册：只读白名单分页/total；role表含COUNT摘要（read） | 已接API及通用表浏览，不提供任意逐表编辑/清空 | 归属隔离、稳定主键排序；文本上限4KB并标记truncated | [033_resonance.sql](../internal/store/migrations/033_resonance.sql) |
| table | `role_settings` | 角色设置/当前外观 | 表视图注册：只读白名单分页/total；role表含COUNT摘要（read） | 已接API及通用表浏览，不提供任意逐表编辑/清空 | 归属隔离、稳定主键排序；文本上限4KB并标记truncated | [032_role_looks.sql](../internal/store/migrations/032_role_looks.sql) |
| table | `role_skins` | 英雄皮肤拥有记录 | 仅发放皮肤（write），无拥有列表；表视图注册：只读白名单分页/total；role表含COUNT摘要（read） | 已接API及通用表浏览，不提供任意逐表编辑/清空 | 归属隔离、稳定主键排序；文本上限4KB并标记truncated | [031_skins.sql](../internal/store/migrations/031_skins.sql) |
| table | `roles` | 角色基本资料/角色页 | 角色列表/详情、封禁/等级/VIP及踢线（read/write）；表视图注册：只读白名单分页/total；role表含COUNT摘要（read） | 已接API及通用表浏览，不提供任意逐表编辑/清空 | 归属隔离、稳定主键排序；文本上限4KB并标记truncated | [001_init.sql](../internal/store/migrations/001_init.sql) |
| table | `room_bbs` | 个人空间留言板 | 表视图注册：只读白名单分页/total；role表含COUNT摘要（read） | 已接API及通用表浏览，不提供任意逐表编辑/清空 | 归属隔离、稳定主键排序；文本上限4KB并标记truncated | [040_space.sql](../internal/store/migrations/040_space.sql) |
| table | `schema_migrations` | 数据库迁移账本 | 不提供：迁移基础设施 | 保留专用入口/不提供通用表视图 | 不提供：迁移基础设施 | [store.go](../internal/store/store.go) |
| table | `servers` | 区服配置/区服页 | 区服查看、增改删（read/manage），含开服时间；表视图注册：只读白名单分页/total；role表含COUNT摘要（read） | 已接API及通用表浏览，不提供任意逐表编辑/清空 | 归属隔离、稳定主键排序；文本上限4KB并标记truncated | [001_init.sql](../internal/store/migrations/001_init.sql) |
| table | `shop_buys` | 商城购买次数 | 表视图注册：只读白名单分页/total；role表含COUNT摘要（read） | 已接API及通用表浏览，不提供任意逐表编辑/清空 | 归属隔离、稳定主键排序；文本上限4KB并标记truncated | [016_shop_buys.sql](../internal/store/migrations/016_shop_buys.sql) |
| table | `star_tower` | 海底监狱进度/次数 | 表视图注册：只读白名单分页/total；role表含COUNT摘要（read） | 已接API及通用表浏览，不提供任意逐表编辑/清空 | 归属隔离、稳定主键排序；文本上限4KB并标记truncated | [042_star_tower.sql](../internal/store/migrations/042_star_tower.sql) |
| table | `star_tower_claims` | 海底监狱里程碑领取 | 表视图注册：只读白名单分页/total；role表含COUNT摘要（read） | 已接API及通用表浏览，不提供任意逐表编辑/清空 | 归属隔离、稳定主键排序；文本上限4KB并标记truncated | [042_star_tower.sql](../internal/store/migrations/042_star_tower.sql) |
| table | `stone_dungeon_clears` | 材料副本首通 | 表视图注册：只读白名单分页/total；role表含COUNT摘要（read） | 已接API及通用表浏览，不提供任意逐表编辑/清空 | 归属隔离、稳定主键排序；文本上限4KB并标记truncated | [043_stone_dungeon.sql](../internal/store/migrations/043_stone_dungeon.sql) |
| table | `stone_dungeon_state` | 材料副本每日次数 | 表视图注册：只读白名单分页/total；role表含COUNT摘要（read） | 已接API及通用表浏览，不提供任意逐表编辑/清空 | 归属隔离、稳定主键排序；文本上限4KB并标记truncated | [043_stone_dungeon.sql](../internal/store/migrations/043_stone_dungeon.sql) |
| table | `summon_free` | 普通/高级召唤免费次数 | 表视图注册：只读白名单分页/total；role表含COUNT摘要（read） | 已接API及通用表浏览，不提供任意逐表编辑/清空 | 归属隔离、稳定主键排序；文本上限4KB并标记truncated | [014_summon_free.sql](../internal/store/migrations/014_summon_free.sql) |
| table | `ticket_msgs` | 工单追问/回复 | 工单详情追问/历史回复及回复（read/write）；表视图注册：只读白名单分页/total；role表含COUNT摘要（read） | 已接API及通用表浏览，不提供任意逐表编辑/清空 | 归属隔离、稳定主键排序；文本上限4KB并标记truncated | [049_tickets.sql](../internal/store/migrations/049_tickets.sql) |
| table | `tickets` | 客服工单/工单页 | 工单列表/详情及回复（read/write）；表视图注册：只读白名单分页/total；role表含COUNT摘要（read） | 已接API及通用表浏览，不提供任意逐表编辑/清空 | 归属隔离、稳定主键排序；文本上限4KB并标记truncated | [049_tickets.sql](../internal/store/migrations/049_tickets.sql) |
| table | `variety_shop` | 随机商店货架/刷新 | 表视图注册：只读白名单分页/total；role表含COUNT摘要（read） | 已接API及通用表浏览，不提供任意逐表编辑/清空 | 归属隔离、稳定主键排序；文本上限4KB并标记truncated | [026_variety_shop.sql](../internal/store/migrations/026_variety_shop.sql) |
| table | `vip_gifts` | VIP礼包领取 | 表视图注册：只读白名单分页/total；role表含COUNT摘要（read） | 已接API及通用表浏览，不提供任意逐表编辑/清空 | 归属隔离、稳定主键排序；文本上限4KB并标记truncated | [024_vip_gifts.sql](../internal/store/migrations/024_vip_gifts.sql) |

## 最终覆盖与限制

1. **阶段1已实现运行时注册与玩家原始数据查看**：当前108项有真实state构造器，77项玩家模块可查原始JSON，三个前缀族验证窗口key或正数公会GID。所有GET不写库。阶段2六类充值专用视图及白名单重置已实现；阶段3a已接全服状态投影与16项原有排名，现有前端通过通用只读视图覆盖，108项登记不代表开放任意写入。重置基础机制使用before值比较，数据变更与审计同一事务提交，审计失败回滚。
2. **阶段2已接充值状态汇总和白名单重置**：六组模块覆盖9个具体key；汇总附真实role_charge和仅该角色的订单分页。累计实付、VIP经验、历史购买次数和其他模块不可编辑。月卡Base策略已注明并登记OPEN_QUESTIONS；View中的协议字段来自现有游戏逻辑，状态投影不写回。
3. **阶段3全服入口已接入**：31项经真实服务锁读取，16项Rank复用现有排序；赛季/区服概览只读。8个审定赛季状态项已验证自然到期补结算、重复与原生滚动/实际冠军赛tick并发；其他23项无结算入口（405），不提前结束或取消进行中的赛季。
4. **阶段3c已接世界等级覆盖**：新增并登记 `worldlev`，专用GET/PUT经网关。0表示不覆盖，until=0长期生效，过期按游戏时钟自动恢复原前10名平均等级；读取不清理存档。登录10192、请求10905、PUT后的10905推送均使用相同有效值。后台不直接改全服状态，PUT与带原因的审计同事务；返回计算值、配置值及有效值。
5. **阶段4专用表后端已接**：82项列白名单、归属分页与role COUNT摘要；英雄装备/天赋按英雄归属联接、角色收件联主题，签到/邀请/商店/召唤/共鸣/推图等均逐表登记。10项使用专用页或安全排除；覆盖门禁核对所有92张表。聊天正文、录像脚本及登录凭据不返回，文本上限4KB并标记truncated。角色订单复用charge接口；前端已接角色分组及区服/公会表浏览。
6. **前端已接入，阶段6完成文档收口**：模块总表108项、玩法状态、充值状态与角色表分组均有入口。公会范围表可从公会列表的“数据表”按钮直达（带公会GID），也可在玩法状态页手输GID；区服列表每行有“玩法状态”链接。
7. **权限与验证边界**：只读表包括 `tickets.phone/email/phone_info`，viewer可读，审核已接受；聊天正文、录像脚本及凭据不返回。表分页单请求内一致，多次offset请求不提供跨请求快照；截断JSON仅预览。补结算事务较长、邮件可在提交前推送，跨进程锁未验证。最终测试及仍未验证的范围见 [ADMIN_COMPLETION_PLAN.md](ADMIN_COMPLETION_PLAN.md)。

## 阶段0–2验收边界

覆盖门禁证明登记完整，不代表所有玩法已开放写操作或原服规则已确认。阶段2新增六类只读View和显式充值重置，所有Editable仍为false；View的state是内存投影，data才是原始已保存JSON。限时礼包仅展示已存报价，不调用CheckLimitGifts触发新报价；神装Kind3仍保持游戏原有不发售行为。重置本身不发奖，重新达成/领取与新订单继续受原规则和幂等约束控制。

阶段2在e222311隔离基线上复现了两个既有补单缺陷：旧已付订单重放仍执行可写钩子，以及并发确认同一待付订单执行两次钩子。最小修复为同Store实例的角色付款/重置共享锁、锁内重读订单并提前拒绝非待付状态；保留原有SQL原子状态检查、before比较及事务审计，不改奖励/限购规则。此进程内锁不是分布式锁，不声称覆盖多admin实例/多Store跨进程并发确认。

阶段3–5已在隔离SQLite、httptest及本机TCP验证网关读取、补结算、世界等级和部分前端流程；未操作生产玩家/付款，未验证跨进程服务锁。阶段5已更新生产UI快照，阶段6仅改文档，不改抓包、功能或依赖。

## Phase 3b: due-only catch-up

The eight approved season keys above now support catch-up through their live
service locks. Other global modules have no settlement entry (405); worldlev has
its separate typed PUT control from phase 3c. No future
clock is injected, no active championship is advanced/cancelled, and no new
championship or guild war is created. Existing calendars, eligibility, rewards and
mail functions are reused. Cross/multi arena payout still waits for rollover.

The gateway commits administrative settlement mail, role/global state and the
trusted actor/reason audit in one transaction. The admin proxy never writes global
state or a duplicate audit. Partial mail failure and audit failure roll back all
three. This atomic unit applies to the new admin operation; existing native timer
failure boundaries and multi-gateway/cross-process locking are not claimed changed.

Team-arena AdminCatchup records a processed admin request in this calendar cycle;
it does not mark the current live season settled or affect gameplay rules. Multi
arena reuses its existing Settled field. Tests compare actual payout counts from
single-reference settlement with simultaneous native/admin settlement for all
eight keys, including each champion variant. Running seasons remain unchanged;
repeats do not send mail again. Phase 3a text above is its historical snapshot;
this section and the updated rows describe the current 3b capability.
