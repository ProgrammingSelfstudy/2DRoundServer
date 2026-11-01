import type { PushReply, ServerRow } from "@/api/admin";
export function formatTime(value: number | undefined) {
  return value ? new Date(value * 1000).toLocaleString("zh-CN", { hour12: false }) : "—";
}
export function integer(value: unknown, min: number, max: number, label: string): number {
  if (typeof value !== "number" || !Number.isSafeInteger(value) || value < min || value > max) {
    throw new Error(`${label}需要是 ${min} 到 ${max} 的整数`);
  }
  return value;
}
export function validateServer(row: ServerRow) {
  if (!/^[A-Za-z0-9]{1,16}_[0-9]{1,6}$/.test(row.srv_id)) throw new Error("区服 ID 形如 symlf_1");
  if (!row.name.trim() || [...row.name].length > 32) throw new Error("区服名称必填，最多 32 个字");
  if (!row.platform.trim() || row.platform.length > 16) throw new Error("平台必填，最多 16 个字符");
  if (!row.host.trim() || row.host.length > 255) throw new Error("主机地址必填，最多 255 个字符");
  integer(row.port, 1, 65535, "端口"); integer(row.zone_id, 1, 2147483647, "分区号");
  integer(row.group_id, 1, 2147483647, "分组号"); integer(row.status, 0, 1, "状态");
  integer(row.open_time, 0, Number.MAX_SAFE_INTEGER, "开服时间");
}
export function pushFeedback(reply: PushReply, noun = "数据") {
  if (reply.push_error || reply.refreshed == null) return {
    type: "warning" as const, message: `${noun}已保存，但网关未能通知角色；玩家重新登录后可见。${reply.push_error ? `（${reply.push_error}）` : ""}`
  };
  return { type: "success" as const, message: reply.refreshed ? `${noun}已保存，并已通知在线角色` : `${noun}已保存，角色当前离线，重新登录后可见` };
}
export function errorMessage(error: unknown) { return error instanceof Error ? error.message : "请求失败，请稍后重试"; }
