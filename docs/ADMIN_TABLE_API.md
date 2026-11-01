# 阶段4：专用表只读 API

所有路由均需登录，viewer/operator/superadmin 可读（`levRead`）。没有表写入接口；原有发放、封禁、充值白名单重置等专用接口保持原权限。前端页签留阶段5。

| GET 路由 | 返回 |
| --- | --- |
| `/api/tables` | `{items:[{name,title,group,scope,owner,columns}]}`，82项静态注册 |
| `/api/roles/{id}/tables` | `{items:[{name,total}]}`，所有role范围表的COUNT摘要，不读取明细 |
| `/api/roles/{id}/tables/{name}` | 角色归属分页明细 |
| `/api/guilds/{id}/tables/{name}` | 公会归属分页明细 |
| `/api/servers/{id}/tables/{name}` | 区服归属分页明细；id为真实srv_id字符串 |

明细返回 `{rows:[],total,limit,offset}`；未保存数据返回空数组和total=0，不创建默认记录。`limit`默认50、允许1–200；`offset`默认0、允许非负整数。空值、重复参数、非整数、溢出或越界返回422。超过末页的offset返回空数组，total仍为全部归属行数。每页按注册的完整主键升序，COUNT与明细在同一只读事务快照内执行；多次请求间若游戏并发增删数据，offset分页不承诺跨请求快照。

未登录401；未注册表、范围不匹配、不存在的角色/公会/区服404。调用方只能选择注册表名及limit/offset，不能指定SQL列、条件或排序。所有归属值、limit和offset均参数绑定，SQL标识符与联表表达式仅来自静态白名单。

每行额外包含 `truncated:[]`，列出被截断的字段名。所有文本（包括JSON）最大4096个UTF-8字节，在完整字符处截断；截断后的JSON是预览字符串，不能视为完整JSON文档。数值保留数据库整数类型。原始完整状态仍通过既有模块接口查看。

- 英雄装备和天赋通过 `partners.partner_id` 联接并按 `partners.rid` 筛选，不接受任意英雄ID绕过角色归属。
- `role_mails`联出`mails.subject`，保留读取、领取、软删除状态；只展示实际收件行，不触发广播邮件补投递。
- `guild_members`、`guild_applies`归role范围；成员职位、捐献与公会GID可读。公会主记录归server，公会日志/BOSS/秘境公共表归guild。前端可用注册元数据选择路径。
- 好友申请、赠礼和邀请绑定按双方参与关系筛选，黑名单按持有人筛选。空间留言按空间主人筛选。
- `chat_offline`仅元数据，不查询正文；`message_bytes`为数据库计算的正文UTF-8字节长度，原始`len`是游戏内容类型标志，不当作长度返回。
- `replays`仅元数据（含最大4KB的meta预览），不查询战斗脚本`data`。
- 订单仍使用 `/api/roles/{id}/charge` 与支付页，不重复实现。
- `admins`、`audit_logs`、`login_tokens`、`schema_migrations`、`accounts`、`pay_config`、`pay_orders`、`redeem_codes`、`module_state`、`global_state`不进入此注册表。逐表原因见覆盖清单；账号密码、第三方身份凭据和会话凭据不会借表视图暴露。

这些API直接只读已持久化的专用表，不调用游戏loader、领取或结算逻辑，不推进游戏状态。全服JSON模块及赛季仍只经网关接口。此阶段不改玩法、不新增任意编辑或清空能力。

实现：`internal/store/admin_tables.go`、`admin_tables_registry.go`和`internal/admin/tables.go`。覆盖门禁同时验证92张表的文档、注册/排除决定，迁移schema查询、敏感列、分页、真实联表归属、HTTP权限与全表读取前后不变。
