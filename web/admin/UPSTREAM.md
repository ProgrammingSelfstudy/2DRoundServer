# 上游来源与本地裁剪

- 来源：https://github.com/HalseySpicy/Geeker-Admin
- 引入日期：2026-11-01
- 分支：master
- 精确提交：`ad08fdd1c08bfd2390a85fbbb359be17e2bdaae9`
- 提交链接：https://github.com/HalseySpicy/Geeker-Admin/commit/ad08fdd1c08bfd2390a85fbbb359be17e2bdaae9
- 许可证：MIT，原始版权及完整许可证见本目录 `LICENSE`，上游作者字段和页脚保留。

这是源码引入，没有嵌套 `.git`；后续可直接修改并随 Go 仓库维护。

保留上游 App、四种布局、主题、国际化、Pinia、菜单/标签页、通用组件（含 ProTable）、样式和必要资源。删除演示业务页、Mock 数据/API、上传/富文本示例、假用户/消息/改密弹窗、PWA 和开发检查器插件；新增公开环境页和健康检查。

第二阶段已接 Go 登录、身份复核及三档权限；路由和菜单按权限声明本地维护，已迁移概览（含广播）、区服与角色管理。第三阶段增加真实邮件、充值和活动管理；第四阶段补齐账号、工单、兑换码、举报、公会、公告、审计、备份、管理员及我的账号，旧导航 16 页全部迁移；提取共享附件/角色搜索/数字输入及提交锁，保留活动 spec 扩展。原上游 HTTP 包装（x-access-token、code/msg/data）、登录/菜单/按钮接口已移除，避免被误当成本项目 API 使用。现有客户端已按 Go HTTP 状态码、error、Bearer 和 sessionStorage 接入；未改 Go 鉴权契约。

构建配置改为本机 Vite `/api` 代理，保留路径前缀，不允许配置远程代理目标。生产请求固定同源 `/api`，不含远程 Mock 地址。锁文件从上游 pnpm v9 格式锁文件裁剪生成，保留仍使用依赖的锁定版本，新增 Node 类型依赖，并用 packageExtensions 补全 SVG 插件漏声明的 fast-glob。Node 和包管理器约束收紧到已验证版本。

未引入 `.husky`、commitlint、lint-staged、commit/release/prepare 脚本；安装不会设置整个 Go 仓库的 Git 钩子，也没有自动 add/commit/push 行为。

上游 ProTable/useTable 仍保留其通用分页接口预期；未来接 Go 的 items/total 等响应时应在页面适配，不能直接沿用演示返回格式。当前业务页使用 Element Plus 表格和 Go 原始分页契约，未使用上游 ProTable 响应包装。已迁移页面均 `isKeepAlive=false`，通用 KeepAlive 上限为 8，退出清缓存。

第五阶段已合入主项目并由 Go 默认提供生产构建：`tools/package-admin-ui.mjs` 复制生成资源，`internal/admin/static.go` 通过 `all:ui/assets` 嵌入全部 chunk（包括 Vite 下划线 helper），无运行时源码/dist 依赖；旧页面不再作为默认入口。仅动态样式放开同源 CSP 的 inline style，脚本仍仅 self、无 eval；移除旧后台链接及配置。构建/真实 HTTP+TCP 证据见 `docs/ADMIN_UI_INTEGRATION.md`。

提交策略：源码、锁文件和 `internal/admin/ui` 的生产快照一起保留，克隆后直接 `go build` 不依赖本机 dist；前端改动后用 `make admin-ui-embed` 更新快照。临时库、凭据、日志、Node 依赖和 Go 二进制均排除。
