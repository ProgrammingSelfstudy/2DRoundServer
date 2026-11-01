# 后台补全计划：所有没接的都接（Codex 执行，Claude 审核验收）

## 当前任务：阶段 1（审核方已授权，2026-11-01）

**阶段 0 已通过审核。现在只做阶段 1，做完单独 commit 并停下，不要继续阶段 2。** 拉到本段说明后直接按下面执行。

审核方的决定：
- `ADMIN_COVERAGE.md` 里标了"不提供自由编辑/强制结算"的 **23 项**（`sweet`、`termbegins`、`sandybeach`、`petard`、`snatch`、`treasure`、`yearmonster` 等活动和排行榜），这一轮**只做查看**，不做结算和编辑。原因：它们的结算规则大多没抓包确认，强行结算可能发错奖。
- 阶段 1 里 `Editable` 全部为 `false`，**不开放整份 JSON 编辑**，`PUT /api/roles/{id}/modules/{key}` 不做。
- `Resets` 只给阶段 2 点名的充值类模块。阶段 1 只把重置的机制和白名单校验做好，先不接具体模块。

阶段 1 要求：
1. 新增 `internal/game/adminreg.go`，定义 `AdminModule` 和 `AdminModules()`，每个模块文件在 `init()` 里注册。覆盖 `ADMIN_COVERAGE.md` 的全部 107 项，包括三个前缀族 `firstcharge_*`、`guildmarket:*`、`monopoly_g*`。
2. 把阶段 0 的完整性门禁升级为：`ADMIN_COVERAGE.md` 的每一项都必须在 `AdminModules()` 里有注册，反之亦然，不一致就让测试失败。
3. 新增 `internal/admin/modules.go`，只做玩家侧接口：`GET /api/modules`、`GET /api/roles/{id}/modules`、`GET /api/roles/{id}/modules/{key}`、`POST /api/roles/{id}/modules/{key}/reset`。
   - reset 的顺序固定：先查网关在线（在线 409，网关不可达 502，两种情况都不写库）→ 读 before → 写入 → 带原因写审计。原因必填，1–200 字。
   - 全服侧 `/api/servers/...` 留给阶段 3，前端留给阶段 5。
4. 所有 GET 不得写库。视图必须复用模块已有的 `view()`/`roll()`，不要另写规则。
5. 测试：三档权限、在线 409、网关不可达 502 且库未变、读接口不写库、未知 key 返回 404、reset 的 item 不在白名单返回 422。
6. 约束：不改游戏规则，不碰 `testdata/captures`，不加前端依赖，**不 push**。

验证：`go vet ./...`、`go test -race ./internal/game ./internal/admin ./internal/store ./internal/admincoverage`、`make build` 全部通过。完成后在下面的进度段更新阶段 1 的提交号和验证结果，并如实写出没验证的部分。

## 最新进度与交接（2026-11-01）

**阶段 0 已通过审核；阶段 1 已完成实现和验证，等待审阅；阶段 2–6 尚未开始。** 用户后续明确授权：将阶段1实现及本次交接备注推送到现有 `main`，不新建分支。此次推送仅同步审阅材料，不代表阶段1已经审核通过，也不是阶段2的开工授权；其他阶段默认不推送的约束继续有效。

- 阶段1实现提交：`98965e830663083448fdf55cdbc3a44bcd772055`（`Register admin modules and add read-only role state APIs`）。该提交包含90个文件，其中79个模块文件追加注册声明；阶段1拉取基线为 `ef272cef41dbe21a4c574f13cdc5516ae47f20da`。
- 已完成107项运行时注册（77个玩家模块项、30个全服模块项），各模块文件init注册真实state构造器，三个前缀族验证真实窗口key/正数公会GID。覆盖门禁现在同时比对源码、`ADMIN_COVERAGE.md` 与 `AdminModules()`。
- 新增玩家侧4个API：`GET /api/modules`、`GET /api/roles/{id}/modules`、`GET /api/roles/{id}/modules/{key}`、`POST /api/roles/{id}/modules/{key}/reset`。GET不写库；未注册key为404；已注册但未保存的状态不创建默认行。
- 所有 `Editable=false`、`Resets=[]`，没有PUT编辑。重置机制先核对网关离线状态（在线409、不可达/状态未知502），再验证显式item白名单；`all`不例外。当前离线请求因无生产白名单返回422，没有启用任何具体充值重置。
- 已验证隔离测试描述符的重置成功路径：先读before、在内存应用白名单操作、比较before后写入；数据与带key/item/reason及before/after摘要的审计在同一事务提交。并发变化返回409，审计失败回滚数据。测试描述符不进入生产注册表。
- 阶段1已通过：`go vet ./...`；`go test -race ./internal/game ./internal/admin ./internal/store ./internal/admincoverage`（game约161秒）；注册表/构造器/前缀及API专项测试；`make admin-ui-check`、原28项前端测试、`make build`。继续使用指定Node 22.23.4和Corepack pnpm 10.11.0；UI快照及锁文件没有变化。
- 验证边界：HTTP测试使用临时SQLite与httptest网关，事务失败用隔离数据库触发器注入。没有对真实玩家启用重置，也未做实际在线玩家的阶段2充值重置联动；专用View/Rank、全服ops、PUT编辑和前端均未实现。玩法语句和抓包未改；`tcRole`仅从函数局部移到包作用域供注册使用。

- 阶段0实现提交：`b1f0dc3d11e15bebecd7d9a6f4a859f588980b48`（`Inventory admin data coverage and enforce registry completeness`），基线为计划提交 `24c5b765db14f1a0acb1e5507ddbf7c22da6d12d`。
- 审阅入口：[ADMIN_COVERAGE.md](ADMIN_COVERAGE.md)。按源码登记 **77 个玩家模块项、30 个全服模块项、92 张表**，覆盖 **343 处持久化调用及 85 个模块常量**。数量含动态前缀族，不是后台功能完成率。
- 扫描范围包括 `internal/game` 及 `internal/store` 的持久化封装，避免漏掉战技、神装、魂器、赋能；清单分别记录当前能力、后续拟提供能力及限制原因。
- 阶段0完整性门禁为 `TestAdminCoverageRegistryComplete`；扫描器与文档注册表检查位于 `internal/admincoverage`，只读扫描工具为 `tools/admincoverage`。阶段0当时未实现运行时注册表、模块API或写库入口；阶段1增量见上。
- 阶段0已通过：`go vet ./...`、`go test -race ./internal/game ./internal/admin ./internal/store ./internal/admincoverage`、注册表及扫描器专项测试、`make admin-ui-check`、原有 28 项前端测试、`make build`。
- 验证使用 `~/.local/node/node-v22.23.4-darwin-x64/bin` 下的 Node `22.23.4`，项目内 Corepack pnpm `10.11.0`。玩法、`testdata/captures`、前端依赖/锁文件和生产 UI 快照均未改动。
- 后续动作：仅按本次授权同步阶段1及备注到 `main`，随后停在阶段1等待审核；阶段2–6仍须审核方逐阶段另行授权，不要自动继续。

拉取后可复核：

```sh
git show --stat b1f0dc3d11e15bebecd7d9a6f4a859f588980b48
git show --stat 98965e830663083448fdf55cdbc3a44bcd772055
go run ./tools/admincoverage
go test ./internal/admincoverage ./internal/game -run 'Test(AdminCoverage|Scan|Registry)' -v
```

## Context

新版后台（`web/admin`，Geeker-Admin，16 页）已经覆盖了 Go 现有的全部管理接口。没接进后台的是**游戏侧数据**：
- 后端约 65 个玩家模块状态（`module_state` 表）
- 约 31 个全服模块状态（`global_state` 表）
- 一批专用表：竞技场、公会、任务、好友、阵容、装备等
- 赛季结算、世界等级

`docs/ROADMAP.md`"后台缺口"第二类（首充/基金/限时礼包、周礼包、月卡月基金、活动排名和结算、竞技场/冠军赛赛季控制、世界等级和开服时间）是其中的一部分。

**用户要求：还没接的全部要做。** 已定规则：
- **离线才写**：所有修改玩家数据的接口先问网关 `GET /online?rid=N`。在线返回 409，页面提示"先踢下线"；网关查不到返回 502，**不写**。
- **全服数据和赛季操作必须在网关进程里做**：这些服务（`amSvc`、`atSvc`、`eliteSvc`、`caSvc`、`drWorld`、`gwSvc`、冠军赛等）的锁和定时器都在网关里。后台只能通过网关控制口调用，**不许直接写 `global_state`**。

95 个模块不能一个模块一个页面地手写。做法是**一个模块注册表 + 通用查看/编辑**覆盖全部模块，再给重要的模块做专用视图。

## 关键事实（已核对）

- 玩家状态：`store.LoadState/SaveState(rid, module)`；全服状态：`LoadGlobal/SaveGlobal(srvID, module)`（`internal/store/module_state.go`）。玩家状态每次请求都从库读，不在内存缓存，所以玩家离线时后台写库是安全的。
- 状态结构都没导出，模块名常量分散在各文件里。例如：
  - `monthCardModule`、`limitGiftModule`、`cycleGiftModule`、`"firstcharge_"+key`
  - 全服的 `champModule`、`drModule`、`atModule`、`caModule`、`guildWarModule`
- 已有的结算函数：
  - `atSvc.settleSeason`（`arenateam.go:302`）
  - `eliteSvc.settleSeasons/settleHalf`（`elitematch_season.go`）
  - `caSvc.settle`（`crossarena.go:297`）
  - `amSvc.settle`（`arenamany.go:259`）
  - `drWorld.settle`（`daysrank.go:168`）
  - `gwSvc.settleAll`（`guildwar.go:345`）
  - 冠军赛 ticker（`arenachampion.go:332`）、活动 ticker（`activity.go:521`）
- 世界等级：`store.WorldLevel` 按前 10 名平均等级算，没有覆盖值。开服时间已经能在 `/servers` 页编辑；开服天数在 `openday.go`。
- 网关控制口：`internal/game/adminhttp.go`，只绑 loopback、没有鉴权。`/push/charge`、`/push/refresh`、`/kick`、`/online` 都可以复用。
- 后台调网关用 `s.gateway(...)`，参考 `kickRole`（`internal/admin/handlers.go:842`）；审计用 `s.audit(...)`。`internal/admin` 已经 import `internal/game`。

## 阶段

### 阶段 0：覆盖清单（先交，作为"全部"的验收标准）

新建 `docs/ADMIN_COVERAGE.md`，脚本生成加人工补充，覆盖：
- `LoadState/SaveState` 和 `LoadGlobal/SaveGlobal` 的**每个**模块名
- `internal/store` 里每张跟游戏相关的表

每行写：模块名、中文名和对应窗口、范围（玩家/全服/表）、后台能做什么（查看 / 专用视图 / 重置 / 编辑 / 赛季操作 / 不提供）。选"不提供"必须写原因。

再加一个 Go 测试：扫描 `internal/game/*.go` 里的模块名常量，**任何没在注册表里的模块都让测试失败**。这样以后新加模块也不会漏接后台。

### 阶段 1：模块注册表和通用查看/编辑

新文件 `internal/game/adminreg.go`：

```go
type AdminModule struct {
    Key, Title, Window string
    Scope   string            // "role" | "global"
    New     func() any        // 返回该模块真实 state 结构的指针，用于反序列化和校验
    View    func(st *store.Store, id string, now time.Time) (any, error) // 可选：人看得懂的视图，复用模块的 view()/roll()
    Resets  []AdminReset      // 可选：白名单单项重置，每项一个函数
    Rank    func(st *store.Store, srv string, limit int) ([]AdminRankRow, error) // 可选：排名
    Editable bool             // 是否允许整份 JSON 编辑（默认 false）
}
func AdminModules() []AdminModule
```

- 每个模块文件在 `init()` 里注册自己，结构类型留在原文件，不导出。
- 前缀模块（`firstcharge_<key>`）按前缀注册。
- 只读视图、重置函数都**复用**模块已有的 `view()`、`roll()` 和规则，不另写一套逻辑。

接口（`internal/admin/modules.go`）：

| 路由 | 权限 | 说明 |
| --- | --- | --- |
| `GET /api/modules` | read | 注册表：key、名字、范围，以及有没有 View/Resets/Rank/Editable |
| `GET /api/roles/{id}/modules` | read | 该角色已有的 module_state 行，按注册表标注 |
| `GET /api/roles/{id}/modules/{key}` | read | 原始 JSON；有 View 的同时给视图 |
| `POST /api/roles/{id}/modules/{key}/reset` | manage | body `{item, reason}`，item 必须在白名单；`item:"all"` 表示删整行 |
| `PUT /api/roles/{id}/modules/{key}` | manage | 只限 `Editable` 的模块。先 `json.Unmarshal` 进 `New()`，**DisallowUnknownFields**，校验通过才保存 |
| `GET /api/servers/{id}/modules`、`/{key}` | read | 全服状态只读，通过网关读，见阶段 3 |

写接口的统一顺序：
1. 查在线：在线返回 409，网关不可达返回 502
2. 读出 before
3. 写入
4. 审计，action 为 `role.module.reset` 或 `role.module.edit`，detail 带 key、item、reason 和 before/after 的 JSON diff 摘要

原因必填，1–200 字。

### 阶段 2：充值类专用视图和重置（ROADMAP 点名项）

用注册表的 View/Resets 实现：
- **首充**（每个窗口 key）：两档是否达成、已领天数、选的英雄、每天的状态。可重置：整个窗口。
- **每日首充**：当天累计、是否已领。可重置：当天。
- **月卡 ×2**：激活、剩余天数、当天状态、累计金额。可重置：单卡；Base 设为当前已付金额，规则写注释并记入 `OPEN_QUESTIONS.md`。
- **月基金 ×2**：可重置单个基金。
- **限时礼包**：每个 id 的开放、过期、是否已买。可重置单条。
- **周/月礼包**：先 `roll(now)` 再显示，但只在内存里 roll，**不写回**。可重置单个 charge id 的计数。

另加 `GET /api/roles/{id}/charge`：把以上汇总在一起，并附上该角色的充值订单（复用 pay 的 store 查询）。

### 阶段 3：全服数据、排名、结算和赛季控制（经网关）

网关控制口新增（`internal/game/adminhttp.go` 或新文件 `adminops.go`，同样只绑 loopback）：
- `GET /ops/global?srv=&key=`：在该服务自己的锁下读出全服状态；有 View 的同时给视图。
- `GET /ops/rank?srv=&key=&limit=`：调注册表里的 Rank。
- `GET /ops/season?srv=&key=`：当前赛季或届次、阶段、起止时间、是否已结算、发奖情况。
- `POST /ops/season/settle?srv=&key=`：在服务锁下调用**已有**的 settle 函数，**必须幂等**：已结算的直接返回 `{"settled":false,"reason":"already"}`。

  只给结算函数已有、规则已写好的模块：arenateam、elitematch、crossarena、arenamany、daysrank、guildwar、冠军赛（本届强制推进到发奖或取消）。其他模块 settle 返回 405，并在覆盖清单里写明原因。
- `GET/PUT /ops/worldlev?srv=`：世界等级覆盖值，存在 global_state `"worldlev"`，格式 `{override, until}`。`store.WorldLevel` 优先用没过期的覆盖值；0 表示不覆盖。改完推一次 10905 给在线玩家。

为此给各服务加一个统一接口，例如：

```go
type adminSeasonSvc interface {
    seasonStatus(srv string, now time.Time) (any, error)
    settleNow(srv string, now time.Time) (bool, error)
}
```

在 `RegisterXxx` 里挂到注册表上。

后台接口转发这些，并加上鉴权：
- `GET /api/servers/{id}/modules/{key}`、`/rank`、`/season`：read
- `POST /api/servers/{id}/seasons/{key}/settle`：manage，二次确认，原因必填，记审计
- `GET/PUT /api/servers/{id}/worldlev`：GET 为 read，PUT 为 manage
- `GET /api/servers/{id}/overview`：世界等级（算出值和覆盖值）、开服第 N 天、开服时间、竞技场本季起止

网关不可达时所有全服接口都返回 502。

### 阶段 4：角色专用表补全

对照覆盖清单，把还没在角色详情里出现的表接成只读页签，直接用 store 已有的查询函数：
- 英雄列表、属性、装备、神器
- 阵容
- 公会和职位
- 好友和黑名单
- 任务进度
- 竞技场（分数、排名、日志）
- 角色收到的邮件
- 充值订单

能安全改的，在覆盖清单里写明后再加写接口，同样离线才写。

### 阶段 5：前端

- 角色详情新增页签：
  - **充值状态**：专用视图加每项重置
  - **模块数据**：按注册表列出该角色的所有模块，有视图就显示视图，没有就显示格式化 JSON；重置和编辑只对 superadmin 可见
  - 阶段 4 的各只读页签
- 新页 `/#/game-status`"玩法状态"：选区服后显示
  - 概况：世界等级（superadmin 可以设覆盖值和到期时间）、开服天数、竞技场本季
  - 排名：下拉选择有 Rank 的模块
  - 赛季：列出所有有赛季的模块，显示状态和"立即结算"按钮（superadmin，二次确认，填原因）
  - 全服数据浏览
- 新页 `/#/modules`"模块总表"：注册表加覆盖情况，方便核对"全部接了"。
- 统一交互：
  - 409 提示"玩家在线，请先踢下线"，旁边给踢线按钮
  - 502 提示"网关不可用，未修改"
  - 结算返回 already 时提示"本季已结算"
  - 沿用 `useCommand` 的提交锁、`useCollection`、`management.ts` 权限表、`menu.ts`
- 前端测试新增：权限可见性、item 和原因校验、错误文案映射、JSON 编辑校验。原有 28 项必须全部保留。
- 跑 `make admin-ui-embed`，**更新后的 `internal/admin/ui` 快照一起提交**。

### 阶段 6：文档

- `docs/ROADMAP.md`"后台缺口"：两类都逐项标完成，并链接到 `ADMIN_COVERAGE.md`。
- `web/admin/README.md`：页面表更新。
- `docs/ADMIN_UI_INTEGRATION.md`：补网关 `/ops/*` 接口说明。

## 给 Codex 的约束

- 每个阶段一个 commit（阶段 0 先单独交，我先审清单），不要 push。
- 不改游戏规则。只加注册、导出视图、admin 用的 settle 入口和世界等级覆盖；已有 settle 函数的行为不能变。
- 不碰 `testdata/captures`。不加前端依赖，不重写 lockfile，使用 Node 22 和 corepack pnpm 10.11.0。
- 未确认的规则写进注释，并在 `docs/OPEN_QUESTIONS.md` 加一条。

## 验收（Claude 逐阶段执行）

1. **阶段 0**：抽查清单，拿 grep 出的模块名逐个比对，不能漏；"不提供"的理由要成立。
2. **每阶段**：
   - `go vet ./...`
   - `go test -race ./internal/game ./internal/admin ./internal/store`，注册表完整性测试必须通过
   - `make admin-ui-check`、`make admin-ui-test`、`make build`
   - `git status` 里只有预期文件，快照已更新
3. **读 diff**，重点看：
   - 视图和重置有没有复用模块已有逻辑
   - GET 接口有没有写库
   - 写接口在查在线之前或失败时有没有写库
   - 后台有没有绕过网关直接写 global_state
   - settle 是否幂等，重复调用会不会重复发奖或发邮件
   - JSON 编辑有没有校验未知字段
   - 审计有没有带原因
4. **`make dev` 实测**：
   - 造有月卡、首充、周礼包状态的角色：在线重置返回 409；踢线后重置成功，审计有记录；重新登录后游戏里状态正确，可以重新达成，不重复发奖
   - 随便挑 5 个不同模块打开"模块数据"，原始 JSON 和视图都能显示
   - 世界等级覆盖：登录后 10905 显示覆盖值；过期或清零后恢复按算法计算
   - 对 arenateam 和 daysrank 各执行两次"立即结算"：第二次返回 already，奖励邮件只发一次
   - viewer/operator 看不到写按钮，直接调接口返回 403
   - 网关停掉后，全服页面和写操作都返回 502
