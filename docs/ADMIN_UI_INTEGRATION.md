# 新后台 Go 接入与验证

新版 Geeker-Admin 已取代 Go 默认管理入口，16 个业务页均使用真实同源 API、Bearer/sessionStorage 和 viewer/operator/superadmin 权限。旧 `internal/admin/web` 仅保留源码参考及回归测试，不再对外提供旧页或循环链接。完整页面/权限矩阵见 [前端说明](../web/admin/README.md)，MIT 和上游版本见 [来源说明](../web/admin/UPSTREAM.md)。

## 构建

克隆仓库后可直接 `go build ./cmd/admin`：所需的 `internal/admin/ui/index.html` 及全部生产资源快照随源码入库，二进制运行时不依赖 Node、Vite、dist 或源码目录。

修改前端后，在仓库根目录使用 Node 22.22.2 / Corepack pnpm 10.11.0：

```sh
nvm use 22.22.2
make build
# 或只更新前端快照：make admin-ui-embed
./bin/admin -config configs/dev.yaml
# 同时启动 loginhttp/gateway/admin：make dev
```

`make build` 顺序为 frozen-lockfile 安装 → 类型检查/生产构建 → 打包 `internal/admin/ui` → 构建全部 Go cmd。前端变动需同步提交更新的生产快照；`web/admin/dist`、node_modules、Go 二进制和运行数据均忽略。资源缺失时 go:embed 明确失败，不会回退旧页。`tools/package-admin-ui.mjs` 只复制生产 HTML/assets，不复制环境文件、source map 或依赖目录。

## 静态契约

- Vite 资源路径为 `/assets/...`，使用 hash 路由：`/#/mail` 等深链刷新仍请求根入口。
- Go 只服务真实嵌入文件；缺失资源、目录及旧 `/app.js` 返回 404，不以 HTML 冒充 JS。未知 `/api/...` 返回 JSON 404，API 鉴权和权限契约保持。
- 明确设置 JS/CSS、字体、图片等 MIME；静态文件 ETag/no-cache 重验证，API no-store，无目录列表。
- CSP 保持 default-src none、script-src/connect-src/font-src self、禁止 eval 和外域脚本。仅为 Vue/Element Plus 动态样式允许 inline style；图片限定同源和 data URL。保留 nosniff、DENY frame 和 no-referrer。
- Vite 会生成下划线开头的 helper，Go 默认递归 embed 会排除它。`all:ui/assets` 覆盖全部 chunk；回归逐个检查 JS 依赖均存在且可访问，防止生产空白页。

## 隔离真实网关联调

```sh
make admin-live-sandbox
# 将脚本打印的隔离目录记作 ADMIN_LIVE_DIR，再运行：
go run ./tools/adminliveprobe -dir "$ADMIN_LIVE_DIR"
# 保持一个协议角色在线，记录浏览器触发的通知：
go run ./tools/adminliveprobe -dir "$ADMIN_LIVE_DIR" -mode watch
```

脚本在忽略的 `data/admin-live.XXXXXX` 中生成独立 SQLite、随机 JWT、三档管理员和权限 0600 的 fixture.json，启动真实 cmd/admin、cmd/loginhttp、cmd/gateway，共用该数据库；仅绑定 loopback，端口占用时拒绝启动，不使用原服或正式玩家库。

| 服务 | 默认隔离地址 |
| --- | --- |
| Go 新后台 | http://127.0.0.1:19080/#/login |
| 登录 HTTP | http://127.0.0.1:19081 |
| TCP 网关 | 127.0.0.1:19800 |
| 网关控制 | http://127.0.0.1:19801 |

凭据、进程 PID、数据库、备份、日志和 evidence.json 只保留在脚本输出的本地隔离目录，均不提交。停止服务时先核对 PID 命令行匹配该配置，再只停止这些进程。脚本停止后保留隔离证据，避免误删仍需复核的测试数据。

协议工具校验标记、目录内数据库、loopback 地址和匹配 API 后才允许写入。游戏账号通过真实 loginhttp 获取 token，以握手、1197 签名、1164/10191/10192 登录；不是 stub 网关，不输出密码/token。

实际验证：

- 定向邮件返回 pushed=1，TCP 收到 10803；10800/10806 可读正确正文与钻石/道具附件。10801 领取准确增加钻石 7、道具 1，详情不再展示已领附件；重复领取被拒，账本不重复增加。
- 公告创建/编辑收到 10950 的正确内容，10952 打开及已读状态更新通过；删除收到 10951，后续列表不含已删公告。
- 角色封禁收到 10310 并实际 EOF，目标角色重登被拒，同账号另一角色可继续请求；解封恢复。主动踢线也收到 10310/EOF，未封禁且可重连。
- 账号封禁断开同账号两个在线角色，HTTP 登录及旧游戏 token 被拒；解封后均能重新登录。
- 浏览器从 Go 页面触发邮件、公告增改删、角色/账号封禁和主动踢线，对应 TCP 帧及断线均被客户端记录。
- 同源上传、保存、显示无收款信息的测试 PNG 后清除，浏览器无 CSP 错误；无真实收款、付款或外域上传。

只暂停自己启动的隔离 gateway 后运行 `-mode offline`：邮件/公告仍保存，pushed=null 且有 push_error，踢线返回 502。重启同一隔离 gateway 并同步本地 PID 后运行 `-mode recover`，通过 TCP 列表/详情确认离线保存的数据可读。工具不会把落库成功等同在线通知成功。

## 验证与边界

已通过前端类型检查、生产构建、新版 28 项和旧版 18 项测试、Go 管理/存储/网关/登录及相关游戏联动 race 测试、静态依赖/MIME/ETag/安全头/API 与资源 404 回归；浏览器核对全部 16 页、深链刷新、权限和会话。同源 Go 二进制从隔离数据目录独立运行，不依赖 Vite。

真实本地网关与 TCP 协议客户端通过，真机界面未验。未操作原服、真实玩家/支付或数据库恢复。构建仍有非阻断的大包和 Browserslist 提示，未扩展无关性能或玩法。

## 5 项游戏回归修复

| 测试 | 根因与修复 |
| --- | --- |
| `TestCrossClosed` | 测试默认真实日期，在周日错误断言冠军赛关闭。固定周一时钟，保持关闭断言；固定周日的 `TestCrossChampion` 继续验证开启、入场名单和状态。 |
| `TestWarOrderAdvanceCard` | 固定十月会话与真实十一月支付 hook 混用，月切换重置了经验和免费领奖记录，手改 Premium 标志无法恢复进度。提取带确认时间的 guard/grant；生产包装仍使用真实确认时间，测试用同一可控时钟，不再手改状态。新增月界、同月进度、重复购买及未开放月份回归。 |
| `TestHaloOfCaptures` | 将布阵预览 condition 排除误用于战斗 wire。按配置精确计数匹配全部战斗光环，升序编码，通用得到五阵营的 212223。61 个旧新 JSONL 的 196 条光环记录吻合。详见 [客户端与抓包依据](COMBAT_HALO_FIX.md)。 |
| `TestArenamanyPartyWithRobotAndRole` | 29000 同时是主动推送和请求回包，测试可能在 handler 保存前误认为请求已完成。改用无主动推送的 29010 在相同 board 锁下同步，保留全部队伍/队长断言；20 次 race 通过，无 sleep 或超时放宽。 |
| `TestSweet` | 不同角色并发执行 LoadGlobal/修改/SaveGlobal，后存快照覆盖先存积分。用服务实例锁保护整个读改写；新增双 TCP 会话各 20 次捐献，检查总分、个人分及库存。用只读编译 overlay 替换旧实现，新增测试连续三次均复现丢分；修复后三轮组合 race 通过。 |

最终从项目根目录执行 `make test`、`make build`、`make admin-ui-check`，均退出 0。包含 **完整 `go test -race ./...`、`go vet ./...`、新版前端 28/28、旧前端 18/18**；游戏包 race 耗时 161.435 秒，无失败或 data race。更新隔离服务后，既有真实 HTTP/TCP 联动工具和 Go 页面冒烟再次通过。

现有后台成果保留，抓包未修改、排除或夹带进入本次任务提交。并发抓包聊天的文档/数据继续保留。光环协议只验证了最多三个 ID，未无依据扩展四 ID 编码；光环属性加成仍是原有未实现范围。
