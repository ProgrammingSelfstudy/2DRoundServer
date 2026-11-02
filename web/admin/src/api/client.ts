import type { AdminSession } from "../auth/session.js";

const messages: Record<string, string> = {
  "wrong username or password": "用户名或密码错误，或账号已停用",
  "login required": "请先登录",
  "invalid or expired token": "登录已失效，请重新登录",
  "invalid token": "登录凭证无效，请重新登录",
  "your role may not do this": "当前角色没有执行此操作的权限",
  "too many failed attempts, try again later": "登录失败次数过多，请稍后重试",
  "the server still has roles; put it into maintenance instead": "该区服仍有角色，不能删除；可先设为维护",
  "that srv_id already exists": "区服 ID 已存在",
  "no such server": "区服不存在",
  "no such role": "角色不存在",
  "main_srv_id must be an existing server": "合服目标必须是已存在的区服",
  "no such order": "订单不存在，请刷新订单列表",
  "the order is not waiting for payment any more": "订单已处理或已取消，请刷新核对，不能重复确认或取消",
  "no such product": "商品不存在或不受支持，请重新选择可发货商品",
  "the product of the order is not in the table": "订单商品不在当前商品表中，不能发货",
  "this product cannot be paid for yet": "该商品暂不支持发货，或角色不满足购买条件",
  "the role has too many orders waiting": "该角色待付款订单已达上限，请先核对已有订单",
  "the notice is at most 300 characters": "支付说明最多 300 字",
  "the QR code must be a png, jpeg or webp image of up to 600 KiB": "收款码必须是 PNG、JPEG、WebP 图片，最多 600 KB",
  "srv_id must be an existing server, or empty for all servers": "请选择已存在的区服，留空表示全服",
  "no such activity": "活动不存在，请刷新列表",
  "end_at must be after start_at (or 0 for no end)": "结束时间必须晚于开始时间，或留空表示不结束",
  "subject is required, at most 60 characters": "邮件标题必填，最多 60 字",
  "content is at most 2000 characters": "邮件正文最多 2000 字",
  "from_name is at most 20 characters": "发件人最多 20 字",
  "expire_at is already in the past": "邮件失效时间必须晚于现在",
  "the current password is wrong": "当前密码不正确",
  "the password must be 8 to 72 characters": "密码必须为 8 到 72 字节",
  "the new password must be 8 to 72 characters": "新密码必须为 8 到 72 字节",
  "username must be 3 to 32 characters of letters, digits, _ . -": "用户名须为 3 到 32 位字母、数字、_ . -",
  "that username is taken": "管理员用户名已被使用",
  "role must be viewer, operator or superadmin": "请选择有效的管理员角色",
  "you cannot lower or disable your own account": "不能降低或停用当前管理员账号",
  "at least one active superadmin must remain": "必须保留至少一名启用的超级管理员",
  "no such admin": "管理员不存在，请刷新列表",
  "no such account": "游戏账号不存在",
  "reason is at most 200 characters": "封禁原因最多 200 字",
  "no such ticket": "客服工单不存在",
  "the answer must be 1-1000 characters": "回复须为 1 到 1000 字",
  "code must be 4 to 32 letters or digits": "兑换码须为 4 到 32 位字母或数字",
  "note is at most 200 bytes": "备注最多 200 字节",
  "that code exists": "兑换码已存在（不区分大小写）",
  "no such code": "兑换码不存在，请刷新列表",
  "max_uses must be 0 to 1000000": "使用上限须为 0 到 100 万",
  "days must be 0 (for good) to 3650": "有效天数须为 0 到 3650，0 表示长期",
  "give 1 to 20 rewards": "请提供 1 到 20 项奖励",
  "no such guild": "公会不存在，可能已解散",
  "no such notice": "公告不存在，请刷新列表",
  "title is required, at most 100 characters": "公告标题必填，最多 100 字",
  "content is required, at most 2000 characters": "公告内容必填，最多 2000 字",
  "backups are not configured": "没有配置备份目录（admin.backup_dir），暂不可备份",
  "role is online; kick it offline first": "玩家在线，请先踢下线",
  "gateway unavailable or online status unknown; no data changed": "网关不可用，未修改",
  "gateway unavailable": "网关不可用，请刷新状态并核对审计确认写入结果",
  "invalid gateway response": "网关响应无效，请刷新并核对审计后确认写入结果",
  "module changed concurrently; refresh before retrying": "数据已变化，请刷新后核对",
  "reset item is not in the module whitelist": "重置项目不在白名单内",
  "role has no saved module state": "该角色没有保存此模块状态",
  "internal error": "服务器暂时无法处理请求，请稍后重试"
};
export class ApiError extends Error {
  constructor(
    public status: number,
    message: string,
    public retryAfter = 0
  ) {
    const missing = /^these roles do not exist: (.*)$/.exec(message);
    const unknown = /^spec: json: unknown field "(.*)"$/.exec(message);
    const attachment = /^attachment (\d+): (.*)$/.exec(message);
    const readable = missing
      ? `收件角色不存在：${missing[1]}`
      : unknown
        ? `活动配置包含服务端不支持的字段“${unknown[1]}”；该字段未被自动删除，请核对配置`
        : attachment
          ? `附件 ${attachment[1]} 校验失败：${attachment[2]}`
          : message;
    super(messages[message] || readable);
    this.name = "ApiError";
  }
}
export class StaleSessionError extends Error {
  constructor() {
    super("会话已切换，本次请求已取消");
    this.name = "StaleSessionError";
  }
}
interface Options {
  method?: "GET" | "POST" | "PUT" | "PATCH" | "DELETE";
  body?: unknown;
  public?: boolean;
}

export function createApi(session: AdminSession, fetcher: typeof fetch = fetch) {
  return async function request<T>(path: string, options: Options = {}): Promise<T> {
    if (!path.startsWith("/") || path.startsWith("//") || path.includes("\\")) throw new Error("只允许本项目 API 路径");
    if (!options.public) session.expire();
    const { token, revision } = session.state;
    if (!options.public && !token) throw new ApiError(401, "login required");
    const headers: Record<string, string> = { Accept: "application/json" };
    if (!options.public) headers.Authorization = `Bearer ${token}`;
    if (options.body !== undefined) headers["Content-Type"] = "application/json";
    let response: Response;
    try {
      response = await fetcher(`/api${path}`, {
        method: options.method || "GET",
        headers,
        body: options.body === undefined ? undefined : JSON.stringify(options.body),
        credentials: "omit",
        cache: "no-store",
        redirect: "error",
        signal: AbortSignal.timeout(10000)
      });
    } catch (error) {
      if (!options.public && revision !== session.state.revision) throw new StaleSessionError();
      const timeout = error instanceof Error && ["TimeoutError", "AbortError"].includes(error.name);
      throw new ApiError(0, timeout ? "请求超时，请稍后重试" : "无法连接到服务器，请检查服务是否已启动");
    }
    let data: any;
    try {
      data = await response.json();
    } catch {
      data = null;
    }
    // Never let a late response from a previous login clear or populate a new session.
    if (!options.public && revision !== session.state.revision) throw new StaleSessionError();
    if (!response.ok) {
      if (response.status === 401 && !options.public) session.clear(revision);
      const retry = Number(response.headers.get("Retry-After"));
      throw new ApiError(
        response.status,
        // A 502 from a push (mail, notice, kick) means the push was not delivered. The module, season and world level
        // endpoints say exactly what happened, so their messages in the table above must reach the user as they are.
        response.status === 502 &&
          !(typeof data?.error === "string" && Object.prototype.hasOwnProperty.call(messages, data.error))
          ? "无法连接游戏网关，本次推送未完成；请检查网关后重试"
          : typeof data?.error === "string"
            ? data.error
            : `请求失败（HTTP ${response.status}）`,
        Number.isFinite(retry) && retry > 0 ? retry : 0
      );
    }
    if (data === null) throw new ApiError(response.status, "服务器返回了无法识别的数据");
    return data as T;
  };
}
