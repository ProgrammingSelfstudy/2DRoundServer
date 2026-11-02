import { can } from "../auth/permissions.js";
import { int, localUnix } from "./operations.js";
export interface ModuleInfo {
  key: string;
  title: string;
  window: string;
  scope: string;
  has_view: boolean;
  has_rank: boolean;
  editable: boolean;
  resets: { item: string; title: string }[];
}
export interface ModuleState {
  key: string;
  exists: boolean;
  module?: ModuleInfo;
  data: unknown;
  view?: { state?: unknown; details?: unknown; protocols?: unknown; season?: Season };
}
export interface Season {
  supported: boolean;
  key: number;
  start_time: number;
  end_time: number;
  phase: number;
  due: boolean;
  settled: boolean;
  history?: unknown;
}
export interface TableInfo {
  name: string;
  title: string;
  group: string;
  scope: "role" | "guild" | "server";
  owner: string;
  columns: string[];
}
export const settleKeys = [
  "arenateam",
  "elitematch",
  "crossarena",
  "arenamany",
  "daysrank",
  "guildwar",
  "arenachamp",
  "crosschamp"
];
export function reason(value: string) {
  if (!value.trim() || [...value].length > 200 || /[\p{Cc}]/u.test(value)) throw Error("原因须为 1–200 字，不能包含控制字符");
  return value;
}
export function resetPayload(module: ModuleInfo, item: string, why: string) {
  if (!module.resets.some(r => r.item === item)) throw Error("请选择白名单内的重置项");
  return { item, reason: reason(why) };
}
export function canReset(role: unknown, module?: ModuleInfo) {
  return can(role, "manage") && !!module?.resets.length;
}
export function canSettle(role: unknown, key: string) {
  return can(role, "manage") && settleKeys.includes(key);
}
export function worldPayload(override: number, until: string, why: string) {
  return { override: int(override, 0, 200, "世界等级覆盖"), until: localUnix(until), reason: reason(why) };
}
export function settleFeedback(r: { settled: boolean; reason: string }) {
  return r.reason === "already"
    ? "本季已结算"
    : r.reason === "not_due"
      ? "尚未到期，未执行补结算"
      : r.settled
        ? "到期补结算已完成"
        : "未执行结算，请刷新核对";
}
export function gameError(status: number, message: string, reset = false) {
  if (status === 409 && reset)
    return message.includes("online") || message.includes("玩家在线") ? "玩家在线，请先踢下线" : "数据已变化，请刷新后核对";
  if (status === 502 && message === "网关不可用，未修改") return message;
  if (status === 502) return reset ? "网关不可用，未修改" : "网关不可用，未修改（只读请求失败）";
  return message;
}
export function tablePath(t: TableInfo, owner: string | number, page = 1) {
  int(page, 1, Number.MAX_SAFE_INTEGER, "页码");
  if (!String(owner).trim()) throw Error("请填写归属 ID");
  const prefix = { role: "roles", guild: "guilds", server: "servers" }[t.scope];
  return `/${prefix}/${encodeURIComponent(owner)}/tables/${encodeURIComponent(t.name)}?limit=50&offset=${(page - 1) * 50}`;
}
export const pretty = (value: unknown) => JSON.stringify(value, null, 2) ?? "未保存";

export interface ChargePresentation {
  columns: { key: string; label: string }[];
  rows: Record<string, unknown>[];
}
// Render only backend projections/protocol values; no qualification or rollover rules here.
export function chargePresentation(m: ModuleState): ChargePresentation | undefined {
  const p = (m.view?.protocols || {}) as Record<string, any>;
  const state = (m.view?.state || {}) as Record<string, any>;
  const details = (m.view?.details || {}) as Record<string, any>;
  const yes = (v: unknown) => (v === true ? "是" : v === false ? "否" : "—");
  const status = (v: number, empty = "未达成") => ({ 0: empty, 1: "可领取", 2: "已领取" })[v] || `未知（${v}）`;
  const make = (columns: [string, string][], rows: Record<string, unknown>[]) => ({
    columns: columns.map(([key, label]) => ({ key, label })),
    rows
  });
  if (m.key.startsWith("firstcharge_")) {
    const message = Object.values(p).find(v => Array.isArray(v?.first_gift));
    return make(
      [
        ["id", "奖励编号"],
        ["status", "领取状态"],
        ["chosen", "已选英雄ID"]
      ],
      (message?.first_gift || []).map((g: any) => ({
        id: g.id,
        status: status(g.status, "未达成或尚未到领取日"),
        chosen: state.Chosen || "未选择"
      }))
    );
  }
  if (m.key === "daycharge")
    return make(
      [
        ["num", "当日累计（VIP经验）"],
        ["status", "领取状态"]
      ],
      [{ num: p["21010"]?.num, status: status(p["21010"]?.status) }]
    );
  if (m.key === "monthcard") {
    const v = p["16705"] || {};
    return make(
      [
        ["index", "卡片索引"],
        ["active", "已激活"],
        ["remaining", "剩余天数"],
        ["acc", "本轮累计（元）"],
        ["days", "已领天数"],
        ["status", "当日状态"],
        ["base", "实付基数（分）"]
      ],
      (details.cards || []).map((c: any) => ({
        index: c.index,
        active: yes(c.active),
        remaining: c.remaining_days,
        acc: v[`card${c.index + 1}_acc`],
        days: v[`card${c.index + 1}_days`],
        status: status(v[`card${c.index + 1}_is_reward`], "未激活"),
        base: state.Base?.[c.index]
      }))
    );
  }
  if (m.key === "monthfund")
    return make(
      [
        ["index", "基金索引"],
        ["id", "基金ID"],
        ["day", "当前领取日"],
        ["status", "当日状态"],
        ["end", "结束时间（Unix秒）"]
      ],
      (p["24701"] || []).map((v: any, index: number) => ({
        index,
        id: v.id,
        day: v.current_day,
        status: status(v.status, "未购买或已结束"),
        end: v.endtime
      }))
    );
  if (m.key === "limitgift")
    return make(
      [
        ["id", "礼包ID"],
        ["name", "名称"],
        ["charge_id", "商品ID"],
        ["supported", "游戏支持发售"],
        ["offered", "已有报价"],
        ["open", "当前开放"],
        ["expired", "已过期"],
        ["bought", "已购买"],
        ["passed", "已错过"]
      ],
      (details.entries || []).map((v: any) => ({
        ...v,
        supported: yes(v.supported),
        offered: yes(v.offered),
        open: yes(v.open),
        expired: yes(v.expired),
        bought: yes(v.bought),
        passed: yes(v.passed)
      }))
    );
  if (m.key === "cyclegift")
    return make(
      [
        ["cycle", "周期"],
        ["id", "商品ID"],
        ["count", "已购次数"],
        ["refresh", "刷新时间（Unix秒）"]
      ],
      (p["21007"] || []).flatMap((v: any) =>
        (v.first_gift || []).map((g: any) => ({
          cycle: v.type === 1 ? "周礼包" : v.type === 2 ? "月礼包" : String(v.type),
          id: g.id,
          count: g.count,
          refresh: v.ref_time
        }))
      )
    );
  return undefined;
}

// Links from the server and guild pages open the game status page with the server, and for a guild its tables, preselected.
// The page reads them back with statusSelection, which accepts only what it can show: a listed server and a positive GID.
export function statusLocation(srvID: string, guildID?: number | string) {
  const query: Record<string, string> = { srv: srvID };
  if (guildID !== undefined) query.gid = String(guildID);
  return { path: "/game-status", query };
}
export function statusSelection(query: Record<string, unknown>, serverIDs: string[]) {
  const srv = typeof query.srv === "string" && serverIDs.includes(query.srv) ? query.srv : "";
  const gid =
    typeof query.gid === "string" && /^[1-9][0-9]*$/.test(query.gid) && Number.isSafeInteger(Number(query.gid))
      ? query.gid
      : "";
  return { srv, gid };
}
