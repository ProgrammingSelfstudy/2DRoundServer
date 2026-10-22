// Pure helpers: no DOM, no network, so they can be tested with `node --test`.

export const LEVELS = { viewer: 1, operator: 2, superadmin: 3 };
const NEED = { read: 1, write: 2, manage: 3 };

// can(role, "write"): the UI hides what the server would refuse; the server still decides.
export function can(role, need) {
  return (LEVELS[role] || 0) >= (NEED[need] || 99);
}

const pad = (n) => String(n).padStart(2, "0");

// fmtTime renders a unix time (seconds) as local "YYYY-MM-DD HH:mm:ss"; 0 or missing is "-".
export function fmtTime(unix, utc = false) {
  if (!unix) return "-";
  const d = new Date(unix * 1000);
  const g = utc
    ? [d.getUTCFullYear(), d.getUTCMonth() + 1, d.getUTCDate(), d.getUTCHours(), d.getUTCMinutes(), d.getUTCSeconds()]
    : [d.getFullYear(), d.getMonth() + 1, d.getDate(), d.getHours(), d.getMinutes(), d.getSeconds()];
  return `${g[0]}-${pad(g[1])}-${pad(g[2])} ${pad(g[3])}:${pad(g[4])}:${pad(g[5])}`;
}

// A <input type=datetime-local> value ("2026-10-18T22:30", local time) <-> unix seconds.
export function toUnix(local) {
  if (!local) return 0;
  const m = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2})$/.exec(local);
  if (!m) return NaN;
  const [y, mo, d, h, mi] = m.slice(1).map(Number);
  const date = new Date(y, mo - 1, d, h, mi, 0);
  // Date rolls 2026-13-45 over into another day instead of failing; refuse what does not round-trip.
  if (date.getFullYear() !== y || date.getMonth() !== mo - 1 || date.getDate() !== d || date.getHours() !== h || date.getMinutes() !== mi) return NaN;
  return Math.floor(date.getTime() / 1000);
}

export function toLocalInput(unix) {
  if (!unix) return "";
  const d = new Date(unix * 1000);
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

// parseIDs reads "1, 2 3\n4" into unique positive integers; anything else is reported.
export function parseIDs(text) {
  const ids = [];
  const bad = [];
  for (const tok of String(text || "").split(/[\s,，;；]+/).filter(Boolean)) {
    if (/^[1-9]\d{0,15}$/.test(tok)) {
      const n = Number(tok);
      if (!ids.includes(n)) ids.push(n);
    } else bad.push(tok);
  }
  return { ids, bad };
}

const len = (s) => [...String(s ?? "")].length; // characters, not UTF-16 units: 2000 Chinese characters are 2000

// itemIdOf takes what the item box holds ("10034 生命金卡" as the list fills it, or just
// the digits) and returns the id as text, or "" when it does not start with an id.
export function itemIdOf(text) {
  const m = /^\s*(\d+)(?:\s|$)/.exec(String(text || ""));
  return m ? m[1] : "";
}

// fmtBytes writes a size the way people read it: 212992 -> "208 KB".
export function fmtBytes(n) {
  n = Number(n);
  if (!Number.isFinite(n) || n < 0) return "-";
  if (n < 1024) return `${n} B`;
  const units = ["KB", "MB", "GB", "TB"];
  let v = n / 1024, i = 0;
  while (v >= 1024 && i < units.length - 1) { v /= 1024; i++; }
  return `${v >= 100 || Number.isInteger(v) ? Math.round(v) : v.toFixed(1)} ${units[i]}`;
}

export const LIMITS = { maxAttachments: 8, maxGrants: 20, maxHeroes: 20, maxAmount: 1_000_000_000, maxRecipients: 500 };

// buildAttachments turns the rows of an attachment form (mail attachments or grants) into the
// list the API takes, or {error}. A row with nothing in it is skipped.
export function buildAttachments(attachments, max) {
  const rows = (attachments || []).filter((a) => a.num !== "" || a.itemID !== "" || a.kind === "asset" && a.asset);
  if (rows.length > max) return { error: `附件最多 ${max} 个` };
  const list = [];
  for (const [i, a] of rows.entries()) {
    const num = Number(a.num);
    if (!Number.isInteger(num) || num < 1 || num > LIMITS.maxAmount) return { error: `附件 ${i + 1}：数量需要是 1 到 ${LIMITS.maxAmount} 的整数` };
    if (a.kind === "asset") {
      if (!a.asset) return { error: `附件 ${i + 1}：请选择货币` };
      list.push({ kind: "asset", asset: a.asset, num });
    } else {
      const id = Number(a.itemID);
      if (!Number.isInteger(id) || id < 1) return { error: `附件 ${i + 1}：请从列表中选择物品，或填写物品 ID` };
      list.push({ kind: "item", item_id: id, num });
    }
  }
  return { list };
}

// buildGrantPayload builds the body of a direct grant to a role, or {error}.
export function buildGrantPayload(attachments) {
  const att = buildAttachments(attachments, LIMITS.maxGrants);
  if (att.error) return att;
  if (att.list.length === 0) return { error: "请至少添加一项要发放的资源" };
  return { payload: { grants: att.list } };
}

// buildHeroGrantPayload builds the body of a hero grant to a role from rows of
// {bid, star, num}, or {error}. Same hero and star twice are two rows of the request.
export function buildHeroGrantPayload(rows, heroes) {
  const list = [];
  let total = 0;
  for (const r of rows) {
    const bid = Number(r.bid);
    const star = Number(r.star);
    const num = r.num === "" || r.num == null ? 1 : Number(r.num);
    const info = heroes.find((x) => x.bid === bid);
    if (!info) return { error: "请选择英雄" };
    if (!Number.isInteger(star) || star < info.init_star || star > info.max_star) return { error: `「${info.name}」的星级需要是 ${info.init_star} 到 ${info.max_star}` };
    if (!Number.isInteger(num) || num < 1 || num > LIMITS.maxHeroes) return { error: `数量需要是 1 到 ${LIMITS.maxHeroes} 的整数` };
    total += num;
    list.push({ bid, star, num });
  }
  if (list.length === 0) return { error: "请至少添加一名英雄" };
  if (total > LIMITS.maxHeroes) return { error: `一次最多发放 ${LIMITS.maxHeroes} 名英雄` };
  return { payload: { heroes: list } };
}

// buildMailPayload validates the mail form like the server does (the server stays the
// authority) and returns {payload} or {error}.
export function buildMailPayload(f) {
  const subject = String(f.subject || "").trim();
  if (!subject) return { error: "请填写标题" };
  if (len(subject) > 60) return { error: "标题最多 60 个字" };
  if (len(f.content) > 2000) return { error: "正文最多 2000 个字" };
  if (len(f.from) > 20) return { error: "发件人最多 20 个字" };
  const payload = { scope: f.scope, subject, content: f.content || "", from_name: f.from || "GM", type: Number(f.type) || 1 };
  if (f.scope === "direct") {
    const { ids, bad } = parseIDs(f.roleText);
    if (bad.length) return { error: `角色 ID 不合法：${bad.slice(0, 3).join("、")}` };
    if (ids.length === 0) return { error: "请填写至少一个角色 ID" };
    if (ids.length > LIMITS.maxRecipients) return { error: `一次最多 ${LIMITS.maxRecipients} 个角色` };
    payload.role_ids = ids;
  } else if (f.scope === "broadcast") {
    payload.srv_id = f.srvID || "";
  } else return { error: "请选择发送范围" };
  const days = f.expireDays === "" || f.expireDays == null ? 0 : Number(f.expireDays);
  if (!Number.isInteger(days) || days < 0 || days > 365) return { error: "有效天数需要是 0 到 365 的整数（0 为永久）" };
  if (days > 0) payload.expire_days = days;
  const att = buildAttachments(f.attachments, LIMITS.maxAttachments);
  if (att.error) return att;
  payload.attachments = att.list;
  return { payload };
}

export const reSrvID = /^[A-Za-z0-9]{1,16}_[0-9]{1,6}$/;

export function buildServerPayload(f) {
  const srvID = String(f.srvID || "").trim();
  if (!reSrvID.test(srvID)) return { error: "区服 ID 形如 symlf_1" };
  const name = String(f.name || "").trim();
  if (!name || len(name) > 32) return { error: "名称必填，最多 32 个字" };
  if (!String(f.platform || "").trim()) return { error: "请填写平台" };
  if (!String(f.host || "").trim()) return { error: "请填写主机" };
  const port = Number(f.port);
  if (!Number.isInteger(port) || port < 1 || port > 65535) return { error: "端口需要是 1 到 65535" };
  const zone = Number(f.zoneID);
  if (!Number.isInteger(zone) || zone < 1) return { error: "分区号至少为 1" };
  const group = Number(f.groupID);
  if (!Number.isInteger(group) || group < 1) return { error: "分组号至少为 1" };
  const open = toUnix(f.openTime);
  if (Number.isNaN(open)) return { error: "开服时间格式不对" };
  return {
    payload: {
      srv_id: srvID, name, platform: String(f.platform).trim(), zone_id: zone, host: String(f.host).trim(), port,
      ws_url: String(f.wsURL || "").trim(), status: Number(f.status) === 0 ? 0 : 1, is_new: !!f.isNew, is_recommend: !!f.isRecommend,
      open_time: open, main_srv_id: String(f.mainSrvID || "").trim(), group_id: group,
    },
  };
}

export function buildNoticePayload(f) {
  const title = String(f.title || "").trim();
  if (!title) return { error: "请填写标题" };
  if (len(title) > 100) return { error: "标题最多 100 个字" };
  const content = String(f.content || "").trim();
  if (!content) return { error: "请填写内容" };
  if (len(content) > 2000) return { error: "内容最多 2000 个字" };
  const start = toUnix(f.startAt);
  const end = toUnix(f.endAt);
  if (Number.isNaN(start) || Number.isNaN(end)) return { error: "时间格式不对" };
  if (end !== 0 && end <= start) return { error: "结束时间需要晚于开始时间（留空为不结束）" };
  return { payload: { srv_id: f.srvID || "", title, content, start_at: start, end_at: end, status: f.status ? 1 : 0 } };
}

export function pageInfo(total, limit, offset) {
  const from = total === 0 ? 0 : offset + 1;
  const to = Math.min(offset + limit, total);
  return {
    from, to, total,
    hasPrev: offset > 0, hasNext: offset + limit < total,
    prevOffset: Math.max(0, offset - limit), nextOffset: offset + limit,
  };
}

// queryString({a: 1, b: ""}) -> "?a=1": empty values are left out.
export function queryString(obj) {
  const p = new URLSearchParams();
  for (const [k, v] of Object.entries(obj)) if (v !== "" && v != null) p.set(k, String(v));
  const s = p.toString();
  return s ? "?" + s : "";
}

// parseHash("#/roles?q=a&srv_id=x") -> {route: "roles", params: URLSearchParams}
export function parseHash(hash) {
  const raw = String(hash || "").replace(/^#\/?/, "");
  const [route, query = ""] = raw.split("?");
  return { route: route || "dashboard", params: new URLSearchParams(query) };
}

export function truncate(s, n) {
  const a = [...String(s ?? "")];
  return a.length <= n ? a.join("") : a.slice(0, n).join("") + "…";
}

export function lockText(seconds) {
  const s = Number(seconds) || 0;
  return s >= 60 ? `请 ${Math.ceil(s / 60)} 分钟后再试` : "请稍后再试";
}

export const roleLabel = (r) => ({ viewer: "只读", operator: "运营", superadmin: "超级管理员" }[r] || r);

// Chinese text for the API's fixed English error messages. Anything unknown (for
// example a validation message with numbers in it) is shown as the server sent it.
const ERRORS = {
  "login required": "请先登录",
  "invalid or expired token": "登录已失效，请重新登录",
  "invalid token": "登录已失效，请重新登录",
  "your role may not do this": "你的角色没有这个权限",
  "wrong username or password": "用户名或密码错误",
  "too many failed attempts, try again later": "登录失败次数过多，请稍后再试",
  "the current password is wrong": "当前密码不对",
  "the new password must be 8 to 72 characters": "新密码需要 8 到 72 位",
  "the password must be 8 to 72 characters": "密码需要 8 到 72 位",
  "username must be 3 to 32 characters of letters, digits, _ . -": "用户名需要 3 到 32 位字母、数字、_ . -",
  "that username is taken": "这个用户名已被使用",
  "that srv_id already exists": "这个区服 ID 已存在",
  "no such server": "区服不存在", "no such account": "账号不存在", "no such role": "角色不存在",
  "no such notice": "公告不存在", "no such admin": "管理员不存在", "no such endpoint": "接口不存在",
  "the server still has roles; put it into maintenance instead": "这个区服还有角色，不能删除；可以改成维护状态",
  "you cannot lower or disable your own account": "不能降低或禁用你自己的账号",
  "at least one active superadmin must remain": "至少要保留一个可用的超级管理员",
  "request body too large": "提交的内容太大",
  "internal error": "服务器内部错误",
  "bad id": "编号不对",
};

export const zhError = (msg) => ERRORS[msg] || msg;

export const GATEWAY_DOWN = "网关未运行或无法连接";

// grantSummary words the result of a direct grant for a toast: the grant is stored whatever the
// gateway did; "refreshed" says whether an online role was sent its new currencies and backpack.
export function grantSummary(res) {
  if (res && res.push_error) return { kind: "warn", text: `已发放，但没能实时刷新（${GATEWAY_DOWN}）。玩家重新登录后可见。` };
  if (res && res.refreshed) return { kind: "ok", text: "已发放，在线角色已实时刷新" };
  return { kind: "ok", text: "已发放（该角色不在线，登录后可见）" };
}

// heroGrantSummary words the result of a hero grant for a toast.
export function heroGrantSummary(res) {
  const n = res && res.granted ? res.granted.length : 0;
  if (res && res.push_error) return { kind: "warn", text: `已发放 ${n} 名英雄，但没能实时刷新（${GATEWAY_DOWN}）。玩家重新登录后可见。` };
  if (res && res.refreshed) return { kind: "ok", text: `已发放 ${n} 名英雄，在线角色已实时刷新` };
  return { kind: "ok", text: `已发放 ${n} 名英雄（该角色不在线，登录后可见）` };
}

// pushSummary words the result of a notice save / delete for a toast. The notice is
// stored whatever the gateway did, so a failed push is a warning, not an error.
export function pushSummary(res, done = "已保存") {
  if (res && res.push_error) return { kind: "warn", text: `${done}，但没能实时推送（${GATEWAY_DOWN}）。在线玩家下次登录时会看到变化。` };
  const n = res && res.pushed;
  return { kind: "ok", text: n == null ? done : `${done}，已通知 ${n} 个在线角色` };
}
