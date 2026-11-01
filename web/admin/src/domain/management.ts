import { attachments, int, localUnix, type AssetChoice, type AttachmentInput } from "./operations.js";
import { can, type Capability, type Role } from "../auth/permissions.js";
export const managementPages = [
  ["accounts", "游戏账号", "read", "write"],
  ["tickets", "客服工单", "read", "write"],
  ["redeem", "兑换码", "read", "write"],
  ["reports", "举报记录", "read", null],
  ["guilds", "公会管理", "read", "write"],
  ["notices", "公告管理", "read", "write"],
  ["audit", "审计日志", "manage", null],
  ["backups", "数据库备份", "manage", "manage"],
  ["admins", "管理员", "manage", "manage"],
  ["account", "我的账号", "read", "read"]
] as const;
export function pageCapability(page: string): Capability | undefined {
  return managementPages.find(p => p[0] === page)?.[2];
}
export function actionAllowed(role: unknown, page: string) {
  const p = managementPages.find(p => p[0] === page);
  return !!p?.[3] && can(role, p[3]);
}
export const bytes = (s: string) => new TextEncoder().encode(s).length;
export function password(value: string) {
  if (bytes(value) < 8 || bytes(value) > 72) throw Error("密码必须为 8 到 72 字节（中文占多个字节）");
  return value;
}
export function ownPassword(old: string, value: string, repeat: string) {
  if (!old) throw Error("请输入当前密码");
  if (value !== repeat) throw Error("两次输入的新密码不一致");
  return { old, new: password(value) };
}
export function adminPayload(username: string, value: string, role: Role) {
  if (!/^[A-Za-z0-9_.-]{3,32}$/.test(username)) throw Error("用户名须为 3 到 32 位字母、数字、_ . -");
  if (!["viewer", "operator", "superadmin"].includes(role)) throw Error("管理员角色无效");
  return { username, password: password(value), role };
}
export function banReason(value: string) {
  if ([...value].length > 200) throw Error("封禁原因最多 200 字");
  return { reason: value };
}
export function ticketAnswer(content: string) {
  if (!content.trim() || [...content].length > 1000) throw Error("回复内容须为 1 到 1000 字");
  return { content };
}
export interface Notice {
  id: number;
  srv_id: string;
  title: string;
  content: string;
  start_at: number;
  end_at: number;
  status: number;
  created_by: string;
  created_at: number;
}
export function noticePayload(form: {
  title: string;
  content: string;
  srv_id: string;
  start: string;
  end: string;
  shown: boolean;
}) {
  if (!form.title.trim() || [...form.title].length > 100) throw Error("标题必填，最多 100 字");
  if (!form.content.trim() || [...form.content].length > 2000) throw Error("内容必填，最多 2000 字");
  const start_at = localUnix(form.start),
    end_at = localUnix(form.end);
  if (end_at && end_at <= start_at) throw Error("结束时间必须晚于开始时间");
  return { title: form.title, content: form.content, srv_id: form.srv_id, start_at, end_at, status: form.shown ? 1 : 0 };
}
export function noticeFeedback(reply: { pushed?: number | null; push_error?: string }, removed = false) {
  const verb = removed ? "已删除" : "已保存";
  if (reply.push_error || reply.pushed == null)
    return {
      type: "warning" as const,
      message: `公告${verb}，在线通知未完成；玩家重新登录后读取最新公告。${reply.push_error || ""}`
    };
  return {
    type: "success" as const,
    message: `公告${verb}，网关已${removed ? "移除" : "通知"} ${reply.pushed} 个在线角色（未验证真机接收）`
  };
}
export function redeemPayload(
  form: { code: string; note: string; max_uses: number; days: number },
  rows: AttachmentInput[],
  assets: AssetChoice[]
) {
  const code = form.code.trim(),
    note = form.note.trim();
  if (!/^[A-Za-z0-9]{4,32}$/.test(code)) throw Error("兑换码须为 4 到 32 位字母或数字");
  if (bytes(note) > 200) throw Error("备注最多 200 字节");
  const grants = attachments(rows, assets, 20);
  if (!grants.length) throw Error("至少添加一项奖励");
  return {
    code,
    note,
    max_uses: int(form.max_uses, 0, 1000000, "总使用上限"),
    days: int(form.days, 0, 3650, "有效天数"),
    grants
  };
}
export function formatBytes(value: number) {
  if (value < 1024) return `${value} B`;
  if (value < 1048576) return `${(value / 1024).toFixed(1)} KB`;
  return `${(value / 1048576).toFixed(1)} MB`;
}
export const ticketStates: Record<number, string> = { 1: "待回复", 2: "已回复", 3: "已解决", 4: "已评分" };
export interface Account {
  id: number;
  platform: string;
  account: string;
  role_count: number;
  status: number;
  ban_reason: string;
  created_at: number;
  last_login_at: number;
}
export interface Ticket {
  id: number;
  rid: number;
  srv_id: string;
  issue_type: number;
  title: string;
  state: number;
  score: number;
  created_at: number;
  answered_at: number;
  phone: string;
  email: string;
  phone_info: string;
  questions: string[];
  answers: string[];
}
export interface Guild {
  gid: number;
  srv_id: string;
  name: string;
  lev: number;
  members: number;
  leader: string;
  leader_rid: number;
  created_at: number;
}
export interface Admin {
  id: number;
  username: string;
  role: Role;
  status: number;
  created_at: number;
  last_login_at: number;
}
export interface Report {
  id: number;
  rid: number;
  target_rid: number;
  target_srv_id: string;
  type: number;
  msg: string;
  created_at: number;
}
export interface Redeem {
  code: string;
  note: string;
  attachments: { kind: string; item_id: number; num: number }[];
  used: number;
  max_uses: number;
  expires_at: number;
  created_at: number;
}
export interface Audit {
  id: number;
  created_at: number;
  admin_name: string;
  action: string;
  target: string;
  ip: string;
  detail: string;
}
export interface Backup {
  name: string;
  size: number;
  mod_time: number;
}
