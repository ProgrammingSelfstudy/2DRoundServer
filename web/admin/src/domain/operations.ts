import { can, type Capability } from "../auth/permissions.js";
export function jsonClone<T>(value: T): T {
  return JSON.parse(JSON.stringify(value));
}
export interface AttachmentInput {
  key: number;
  kind: "asset" | "item";
  asset: string;
  item_id?: number;
  num?: number;
}
export interface AssetChoice {
  name: string;
  label: number;
}
export interface Attachment {
  kind: "asset" | "item";
  asset?: string;
  item_id?: number;
  num: number;
}
export interface MailForm {
  scope: "direct" | "broadcast";
  roles: string;
  srv_id: string;
  subject: string;
  content: string;
  from_name: string;
  type: number;
  expiry: "days" | "date";
  expire_days?: number;
  expire_at: string;
}
export interface MailRow {
  id: number;
  srv_id: string;
  scope: "direct" | "broadcast";
  type: number;
  subject: string;
  content: string;
  from_name: string;
  created_by: string;
  created_at: number;
  expire_at: number;
  attachments: { kind: string; item_id: number; num: number }[] | null;
  recipients: number;
  claimed: number;
}
export interface MailReply {
  mail: MailRow;
  pushed: number | null;
  push_error?: string;
}
export interface Product {
  id: number;
  name: string;
  yuan: number;
}
export interface Order {
  order_no: string;
  rid: number;
  srv_id: string;
  charge_id: number;
  amount_cents: number;
  status: number;
  created_at: number;
  paid_at: number;
  operator: string;
  note: string;
  product?: string;
  role?: string;
}
export interface OrderReply {
  ok: boolean;
  order: Order;
  refreshed?: boolean;
  push_error?: string;
}
export interface Aim {
  id: number;
  str: string;
  event: string;
  target: number;
  rewards: number[][];
  [key: string]: unknown;
}
export interface ActivitySpec {
  title: string;
  panel_type: number;
  aims?: Aim[];
  [key: string]: unknown;
}
export interface Activity {
  id: number;
  srv_id: string;
  camp_id: number;
  bid: number;
  status: number;
  start_at: number;
  end_at: number;
  spec: ActivitySpec;
  created_by?: string;
  created_at?: number;
  updated_at?: number;
}
export const operationCapabilities = { mail: "write", pay: "manage", activities: "write" } as const satisfies Record<
  string,
  Capability
>;
export function canOperate(role: unknown, section: keyof typeof operationCapabilities) {
  return can(role, operationCapabilities[section]);
}
export function int(value: unknown, min: number, max: number, name: string): number {
  if (typeof value !== "number" || !Number.isSafeInteger(value) || value < min || value > max)
    throw Error(`${name}需为 ${min} 到 ${max} 的整数`);
  return value;
}
const size = (text: string) => [...text].length;
export function roleIDs(text: string): number[] {
  const ids: number[] = [];
  for (const word of text.split(/[\s,，;；]+/).filter(Boolean)) {
    if (!/^[1-9]\d*$/.test(word)) throw Error(`角色 ID 不合法：${word}`);
    const id = int(Number(word), 1, Number.MAX_SAFE_INTEGER, "角色 ID");
    if (!ids.includes(id)) ids.push(id);
  }
  if (ids.length > 500) throw Error("一次最多 500 个收件角色");
  return ids;
}
export function attachments(rows: AttachmentInput[], assets: AssetChoice[], max = 8): Attachment[] {
  if (rows.length > max) throw Error(`最多 ${max} 个附件`);
  return rows.map((row, index) => {
    const num = int(row.num, 1, 1000000000, `附件 ${index + 1} 数量`);
    if (row.kind === "asset") {
      if (!assets.some(a => a.name === row.asset)) throw Error(`附件 ${index + 1}：请选择货币`);
      return { kind: "asset", asset: row.asset, num };
    }
    if (row.kind !== "item") throw Error("附件类型无效");
    return { kind: "item", item_id: int(row.item_id, 1, Number.MAX_SAFE_INTEGER, `附件 ${index + 1} 物品 ID`), num };
  });
}
// Strict local datetime parsing: Date's automatic calendar rollover must not be accepted.
export function localUnix(value: string, required = false): number {
  if (!value) {
    if (required) throw Error("请填写时间");
    return 0;
  }
  const match = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2})(?::(\d{2}))?$/.exec(value);
  if (!match) throw Error("时间格式无效");
  const [y, m, d, h, minute, s] = match.slice(1).map(v => Number(v || 0));
  const date = new Date(y, m - 1, d, h, minute, s);
  if (
    date.getFullYear() !== y ||
    date.getMonth() !== m - 1 ||
    date.getDate() !== d ||
    date.getHours() !== h ||
    date.getMinutes() !== minute ||
    date.getSeconds() !== s
  )
    throw Error("时间不存在或超出范围");
  return int(Math.floor(date.getTime() / 1000), 0, Number.MAX_SAFE_INTEGER, "时间戳");
}
export function localInput(seconds: number) {
  if (!seconds) return "";
  const d = new Date(seconds * 1000);
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`;
}
export function mailPayload(form: MailForm, rows: AttachmentInput[], assets: AssetChoice[], now = Date.now() / 1000) {
  const subject = form.subject.trim();
  if (!subject || size(subject) > 60) throw Error("邮件标题必填，最多 60 字");
  if (size(form.content) > 2000) throw Error("邮件正文最多 2000 字");
  const from_name = form.from_name || "GM";
  if (size(from_name) > 20) throw Error("发件人最多 20 字");
  if (![1, 3].includes(form.type)) throw Error("邮件类型只能为 1 或 3");
  const base = {
    scope: form.scope,
    subject,
    content: form.content,
    from_name,
    type: form.type,
    attachments: attachments(rows, assets)
  };
  const expires =
    form.expiry === "date"
      ? { expire_at: localUnix(form.expire_at, true) }
      : { expire_days: int(form.expire_days, 0, 365, "有效天数") };
  if ("expire_at" in expires && expires.expire_at !== undefined && expires.expire_at <= now)
    throw Error("邮件失效时间必须晚于现在");
  if (form.scope === "direct") {
    const role_ids = roleIDs(form.roles);
    if (!role_ids.length) throw Error("请填写至少一个收件角色 ID");
    return { ...base, ...expires, role_ids };
  }
  if (form.scope !== "broadcast") throw Error("发送范围无效");
  // Explicit recipient IDs must never leak from the hidden direct-mail field.
  return { ...base, ...expires, srv_id: form.srv_id };
}
export function mailFeedback(result: MailReply) {
  const saved = `邮件 #${result.mail.id} 已保存。`;
  const broadcast =
    result.mail.scope === "broadcast"
      ? "广播邮件按符合条件的角色查看邮件时逐步投递，当前收件数不代表全服人数。"
      : "定向收件关系已创建。";
  const pushed = !result.push_error && Number.isInteger(result.pushed) && Number(result.pushed) >= 0;
  return {
    type: pushed ? ("success" as const) : ("warning" as const),
    message: `${saved}${pushed ? `已通知 ${result.pushed} 个在线角色。` : "未能确认实时推送，邮件仍保留。"}${broadcast}`
  };
}
export function manualOrder(rid: unknown, charge: unknown, confirm: boolean, note: string, products: Product[]) {
  const role = int(rid, 1, Number.MAX_SAFE_INTEGER, "角色 ID");
  const product = products.find(p => p.id === charge);
  if (!product) throw Error("请选择接口支持发货的商品");
  if (size(note) > 100) throw Error("补单备注最多 100 字");
  return { rid: role, charge_id: product.id, confirm, note };
}
export function paymentFeedback(result: OrderReply) {
  if (result.order.status !== 1)
    return { type: "success" as const, message: `订单 ${result.order.order_no} 已创建，等待人工确认到账，尚未发货` };
  return {
    type: result.push_error || result.refreshed == null ? ("warning" as const) : ("success" as const),
    message: `订单 ${result.order.order_no} 已确认并发货；${result.push_error || result.refreshed == null ? "未能确认通知客户端，请勿重复发货" : result.refreshed ? "已刷新在线角色" : "角色离线，下次登录可见"}`
  };
}
export function validateQr(file: { type: string; size: number }) {
  if (!["image/png", "image/jpeg", "image/webp"].includes(file.type)) throw Error("收款码仅支持 PNG、JPEG、WebP 图片");
  if (file.size <= 0 || file.size > 600 * 1024) throw Error("收款码图片需为 1 字节到 600 KB");
}
export function activityState(row: Pick<Activity, "status" | "start_at" | "end_at">, now = Math.floor(Date.now() / 1000)) {
  if (row.status !== 1) return "关闭";
  if (row.start_at > now) return "未开始";
  if (row.end_at && row.end_at <= now) return "已结束";
  return "进行中";
}
export function aimsText(aims: Aim[] = []) {
  return aims
    .map(a => [a.id, a.str || "", a.event || "", a.target, a.rewards.map(r => r.join(":")).join(",")].join(" | "))
    .join("\n");
}
export function parseAims(text: string, original: Aim[] = []): Aim[] {
  const lines = text
    .split("\n")
    .map(l => l.trim())
    .filter(Boolean);
  if (lines.length > 60) throw Error("目标最多 60 项");
  const seen = new Set<number>();
  return lines.map((line, index) => {
    const parts = line.split("|").map(p => p.trim());
    if (parts.length !== 5) throw Error(`目标第 ${index + 1} 行需为 5 段：编号 | 说明 | 事件 | 目标数 | 奖励`);
    const id = int(Number(parts[0]), 1, Number.MAX_SAFE_INTEGER, "目标编号");
    if (seen.has(id)) throw Error(`目标编号 ${id} 重复`);
    seen.add(id);
    if (size(parts[1]) > 200 || size(parts[2]) > 40) throw Error("目标说明最多 200 字，事件名最多 40 字");
    if (!parts[3]) throw Error("目标数量不能为空");
    const target = int(Number(parts[3]), 0, 1000000000, "目标数");
    const rewards = parts[4]
      .split(",")
      .map(r => r.trim())
      .filter(Boolean)
      .map(r => {
        const m = /^(\d+):(\d+)$/.exec(r);
        if (!m) throw Error(`奖励 ${r} 应为 编号:数量`);
        return [int(Number(m[1]), 1, Number.MAX_SAFE_INTEGER, "奖励编号"), int(Number(m[2]), 1, 1000000000, "奖励数量")];
      });
    if (!rewards.length || rewards.length > 10) throw Error("每个目标需有 1 到 10 个奖励");
    return { ...jsonClone(original.find(a => a.id === id) || {}), id, str: parts[1], event: parts[2], target, rewards };
  });
}
export const specTextFields = [
  "title2",
  "rule_str",
  "time_str",
  "aim_title",
  "cli_type_name",
  "ico",
  "top_banner",
  "bottom_alert",
  "reward_title",
  "channel_ban"
] as const;
export const specFormKeys = ["title", "panel_type", "sort_val", "cli_type", "type_ico", "daily", "aims", ...specTextFields];
export interface ActivityForm {
  srv_id: string;
  camp_id?: number;
  bid?: number;
  status: boolean;
  start: string;
  end: string;
  title: string;
  panel_type?: number;
  sort_val?: number;
  cli_type?: number;
  type_ico?: number;
  daily: boolean;
  aims: string;
  texts: Record<string, string>;
  extra: string;
}
export function activityForm(row?: Activity): ActivityForm {
  const sp = jsonClone(row?.spec || { title: "", panel_type: 16 });
  const extra = Object.fromEntries(Object.entries(sp).filter(([key]) => !specFormKeys.includes(key)));
  return {
    srv_id: row?.srv_id || "",
    camp_id: row?.camp_id,
    bid: row?.bid,
    status: row ? row.status === 1 : true,
    start: localInput(row ? row.start_at : Math.floor(Date.now() / 1000)),
    end: localInput(row?.end_at || 0),
    title: sp.title,
    panel_type: sp.panel_type,
    sort_val: Number(sp.sort_val || 0),
    cli_type: Number(sp.cli_type || 0),
    type_ico: Number(sp.type_ico || 0),
    daily: !!sp.daily,
    aims: aimsText(sp.aims),
    texts: Object.fromEntries(specTextFields.map(k => [k, String(sp[k] || "")])),
    extra: JSON.stringify(extra, null, 2)
  };
}
export function activityPayload(form: ActivityForm, original?: Activity) {
  const title = form.title.trim();
  if (!title || size(title) > 40) throw Error("活动标题必填，最多 40 字");
  let extra: any;
  try {
    extra = JSON.parse(form.extra || "{}");
  } catch {
    throw Error("其他配置 JSON 格式无效");
  }
  if (!extra || typeof extra !== "object" || Array.isArray(extra)) throw Error("其他配置必须是 JSON 对象");
  if (Object.keys(extra).some(k => specFormKeys.includes(k))) throw Error("其他配置中不能重复填写表单已有字段");
  const spec: ActivitySpec = {
    ...extra,
    title,
    panel_type: int(form.panel_type, 1, 145, "面板类型"),
    sort_val: int(form.sort_val, -Number.MAX_SAFE_INTEGER, Number.MAX_SAFE_INTEGER, "排序"),
    cli_type: int(form.cli_type, -Number.MAX_SAFE_INTEGER, Number.MAX_SAFE_INTEGER, "主界面图标"),
    type_ico: int(form.type_ico, -Number.MAX_SAFE_INTEGER, Number.MAX_SAFE_INTEGER, "图标类型"),
    daily: form.daily
  };
  for (const key of specTextFields) {
    const value = form.texts[key] || "";
    if (size(value) > (key === "rule_str" ? 2000 : 200)) throw Error(`${key} 超出字数限制`);
    // Keep whitespace and extension values unchanged unless the operator edits them.
    if (value || Object.hasOwn(original?.spec || {}, key)) spec[key] = value;
  }
  const aims = parseAims(form.aims, original?.spec.aims);
  if (aims.length || Object.hasOwn(original?.spec || {}, "aims")) spec.aims = aims;
  const start_at = original?.start_at === 0 && !form.start ? 0 : localUnix(form.start, true);
  const end_at = localUnix(form.end);
  if (end_at && end_at <= start_at) throw Error("结束时间必须晚于开始时间，或留空表示不结束");
  return {
    srv_id: form.srv_id,
    camp_id: int(form.camp_id, 1, Number.MAX_SAFE_INTEGER, "活动号"),
    bid: int(form.bid, 1, Number.MAX_SAFE_INTEGER, "页签编号"),
    status: form.status ? 1 : 0,
    start_at,
    end_at,
    spec
  };
}
// A lost response to a create/send request may mean the server already committed.
// Never auto-retry; require explicit history review before another submission.
export class SubmissionGate {
  state: "idle" | "pending" | "uncertain" = "idle";
  begin() {
    if (this.state !== "idle") return false;
    this.state = "pending";
    return true;
  }
  finish(ambiguous = false) {
    this.state = ambiguous ? "uncertain" : "idle";
  }
  reviewed() {
    if (this.state !== "pending") this.state = "idle";
  }
}

export function ambiguousStatus(status?: number) {
  return status === undefined || status === 0 || (status >= 200 && status < 300) || status >= 500;
}

// Labels already used by the migrated role-grant form; API values stay unchanged.
export function assetTitle(name: string) {
  const names: Record<string, string> = {
    coin: "金币",
    gold: "钻石",
    exp: "角色经验",
    hero_exp: "英雄经验",
    energy: "情报值",
    vip_exp: "VIP 经验",
    guild: "公会贡献",
    arena_cent: "竞技积分",
    friend_point: "友情点"
  };
  return names[name] || name;
}
