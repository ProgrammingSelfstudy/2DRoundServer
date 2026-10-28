// Run with: node --test internal/admin/webtest/
import test from "node:test";
import assert from "node:assert/strict";
import {
  can, itemIdOf, fmtBytes, fmtTime, toUnix, toLocalInput, parseIDs, buildMailPayload, buildGrantPayload, grantSummary, buildHeroGrantPayload, heroGrantSummary, buildServerPayload, buildNoticePayload,
  aimsToText, parseAims, buildActivityPayload, pageInfo, queryString, parseHash, truncate, lockText, roleLabel, LIMITS, zhError, GATEWAY_DOWN, pushSummary,
} from "../web/lib.js";

test("can() follows the three levels and refuses unknown roles", () => {
  assert.ok(can("viewer", "read") && !can("viewer", "write") && !can("viewer", "manage"));
  assert.ok(can("operator", "write") && !can("operator", "manage"));
  assert.ok(can("superadmin", "manage") && can("superadmin", "read"));
  assert.ok(!can("root", "read") && !can(undefined, "read") && !can("superadmin", "nonsense"));
});

test("time conversion round-trips in local time and 0 is blank", () => {
  assert.equal(fmtTime(0), "-");
  assert.equal(fmtTime(1_800_000_000, true), "2027-01-15 08:00:00");
  const u = toUnix("2026-10-18T22:30");
  assert.equal(toLocalInput(u), "2026-10-18T22:30");
  assert.equal(toUnix(""), 0);
  assert.ok(Number.isNaN(toUnix("tomorrow")));
  for (const impossible of ["2026-13-45T25:61", "2026-02-30T10:00", "2026-04-31T10:00", "2026-10-18T24:00", "2026-10-18T10:60", "2026-00-10T10:00"]) {
    assert.ok(Number.isNaN(toUnix(impossible)), `${impossible} must be refused`);
  }
  assert.ok(!Number.isNaN(toUnix("2028-02-29T10:00")), "a real leap day is fine");
  assert.ok(Number.isNaN(toUnix("2027-02-29T10:00")), "29 February of a non-leap year is not");
  assert.equal(toLocalInput(0), "");
});

test("parseIDs keeps unique positive integers and reports the rest", () => {
  assert.deepEqual(parseIDs("1, 2 3\n4,,2"), { ids: [1, 2, 3, 4], bad: [] });
  assert.deepEqual(parseIDs("5，6；7"), { ids: [5, 6, 7], bad: [] }); // full-width separators
  const r = parseIDs("1 abc -3 0 1.5 007");
  assert.deepEqual(r.ids, [1]);
  assert.deepEqual(r.bad, ["abc", "-3", "0", "1.5", "007"]);
  assert.deepEqual(parseIDs(""), { ids: [], bad: [] });
  assert.deepEqual(parseIDs(null), { ids: [], bad: [] });
  assert.deepEqual(parseIDs("99999999999999999999").bad, ["99999999999999999999"]); // beyond safe integers
});

const mailForm = (over = {}) => ({ scope: "direct", roleText: "1,2", srvID: "", subject: "标题", content: "正文", from: "客服", type: "1", expireDays: "7", attachments: [], ...over });

test("buildMailPayload builds the request the API expects", () => {
  const r = buildMailPayload(mailForm({ attachments: [{ kind: "asset", asset: "gold", itemID: "", num: "100" }, { kind: "item", asset: "gold", itemID: "40112", num: "2" }] }));
  assert.deepEqual(r.payload, {
    scope: "direct", subject: "标题", content: "正文", from_name: "客服", type: 1, role_ids: [1, 2], expire_days: 7,
    attachments: [{ kind: "asset", asset: "gold", num: 100 }, { kind: "item", item_id: 40112, num: 2 }],
  });
  const b = buildMailPayload(mailForm({ scope: "broadcast", srvID: "symlf_1", expireDays: "" }));
  assert.equal(b.payload.srv_id, "symlf_1");
  assert.equal(b.payload.role_ids, undefined);
  assert.equal(b.payload.expire_days, undefined);
  assert.equal(buildMailPayload(mailForm({ from: "" })).payload.from_name, "GM");
});

test("buildMailPayload rejects what the server would reject", () => {
  const bad = (over, text) => { const r = buildMailPayload(mailForm(over)); assert.ok(r.error && r.error.includes(text), `${JSON.stringify(over)} -> ${r.error}`); };
  bad({ subject: "  " }, "标题");
  bad({ subject: "长".repeat(61) }, "60");
  bad({ content: "长".repeat(2001) }, "2000");
  bad({ from: "长".repeat(21) }, "20");
  bad({ roleText: "" }, "至少一个");
  bad({ roleText: "1 x" }, "不合法");
  bad({ roleText: Array.from({ length: LIMITS.maxRecipients + 1 }, (_, i) => i + 1).join(",") }, "最多");
  bad({ scope: "" }, "范围");
  bad({ expireDays: "400" }, "0 到 365");
  bad({ expireDays: "1.5" }, "0 到 365");
  bad({ expireDays: "-1" }, "0 到 365");
  bad({ attachments: [{ kind: "asset", asset: "gold", itemID: "", num: "0" }] }, "数量");
  bad({ attachments: [{ kind: "asset", asset: "gold", itemID: "", num: String(LIMITS.maxAmount + 1) }] }, "数量");
  bad({ attachments: [{ kind: "item", asset: "", itemID: "", num: "1" }] }, "物品 ID");
  bad({ attachments: Array.from({ length: LIMITS.maxAttachments + 1 }, () => ({ kind: "item", itemID: "1", num: "1" })) }, "最多");
  // 2000 Chinese characters are 2000 characters, not 4000 UTF-16 units; an emoji counts once
  assert.ok(buildMailPayload(mailForm({ content: "长".repeat(2000) })).payload);
  assert.ok(buildMailPayload(mailForm({ subject: "😀".repeat(60) })).payload);
  // an untouched empty attachment row is ignored rather than an error
  assert.deepEqual(buildMailPayload(mailForm({ attachments: [{ kind: "item", asset: "", itemID: "", num: "" }] })).payload.attachments, []);
});

test("buildServerPayload validates like the server", () => {
  const ok = { srvID: "symlf_2", name: "二区", platform: "symlf", host: "10.0.0.2", port: "8800", zoneID: "2", groupID: "1", status: "1", isNew: true, isRecommend: false, openTime: "", mainSrvID: "", wsURL: "" };
  const r = buildServerPayload(ok);
  assert.deepEqual(r.payload, { srv_id: "symlf_2", name: "二区", platform: "symlf", zone_id: 2, host: "10.0.0.2", port: 8800, ws_url: "", status: 1, is_new: true, is_recommend: false, open_time: 0, main_srv_id: "", group_id: 1 });
  for (const [over, text] of [[{ srvID: "no spaces" }, "symlf_1"], [{ name: " " }, "名称"], [{ port: "0" }, "端口"], [{ port: "70000" }, "端口"], [{ port: "80.5" }, "端口"],
    [{ host: "" }, "主机"], [{ platform: "" }, "平台"], [{ zoneID: "0" }, "分区"], [{ groupID: "0" }, "分组"], [{ openTime: "soon" }, "开服时间"]]) {
    const e = buildServerPayload({ ...ok, ...over });
    assert.ok(e.error && e.error.includes(text), `${JSON.stringify(over)} -> ${e.error}`);
  }
  assert.equal(buildServerPayload({ ...ok, status: "0" }).payload.status, 0);
  assert.equal(buildServerPayload({ ...ok, status: "7" }).payload.status, 1);
});

test("buildNoticePayload checks the time window", () => {
  const ok = { title: "t", content: "c", srvID: "", startAt: "", endAt: "", status: true };
  assert.deepEqual(buildNoticePayload(ok).payload, { srv_id: "", title: "t", content: "c", start_at: 0, end_at: 0, status: 1 });
  assert.ok(buildNoticePayload({ ...ok, startAt: "2026-10-18T10:00", endAt: "2026-10-18T09:00" }).error.includes("晚于"));
  assert.ok(buildNoticePayload({ ...ok, startAt: "2026-10-18T10:00", endAt: "2026-10-18T10:00" }).error);
  assert.ok(buildNoticePayload({ ...ok, startAt: "2026-10-18T10:00", endAt: "2026-10-19T10:00" }).payload.end_at > 0);
  assert.ok(buildNoticePayload({ ...ok, title: "" }).error && buildNoticePayload({ ...ok, content: " " }).error);
  assert.equal(buildNoticePayload({ ...ok, status: false }).payload.status, 0);
});

test("paging, query strings, routes and text helpers", () => {
  assert.deepEqual(pageInfo(0, 25, 0), { from: 0, to: 0, total: 0, hasPrev: false, hasNext: false, prevOffset: 0, nextOffset: 25 });
  assert.deepEqual(pageInfo(60, 25, 25), { from: 26, to: 50, total: 60, hasPrev: true, hasNext: true, prevOffset: 0, nextOffset: 50 });
  assert.deepEqual(pageInfo(60, 25, 50), { from: 51, to: 60, total: 60, hasPrev: true, hasNext: false, prevOffset: 25, nextOffset: 75 });
  assert.equal(queryString({ q: "a b", srv_id: "", n: 0, x: null }), "?q=a+b&n=0");
  assert.equal(queryString({}), "");
  assert.equal(parseHash("#/roles?q=a&srv_id=x").route, "roles");
  assert.equal(parseHash("#/roles?q=a&srv_id=x").params.get("srv_id"), "x");
  assert.equal(parseHash("").route, "dashboard");
  assert.equal(parseHash("#/").route, "dashboard");
  assert.equal(truncate("abcdef", 3), "abc…");
  assert.equal(truncate("长长", 5), "长长");
  assert.equal(truncate(null, 5), "");
  assert.ok(lockText(900).includes("15") && lockText(5).includes("稍后"));
  assert.equal(roleLabel("operator"), "运营");
  assert.equal(roleLabel("x"), "x");
});

test("zhError translates the fixed messages and leaves the rest alone", () => {
  assert.equal(zhError("wrong username or password"), "用户名或密码错误");
  assert.equal(zhError("invalid or expired token"), "登录已失效，请重新登录");
  assert.equal(zhError("no such server"), "区服不存在");
  // a message with details in it is not in the table and is shown as the server wrote it
  assert.equal(zhError("attachment 2: num must be 1 to 1000000000"), "attachment 2: num must be 1 to 1000000000");
  assert.equal(zhError("these roles do not exist: [4242]"), "these roles do not exist: [4242]");
  assert.equal(zhError(""), "");
  assert.equal(zhError(undefined), undefined);
  assert.ok(GATEWAY_DOWN.length > 0);
});

test("pushSummary says what the live push did", () => {
  assert.deepEqual(pushSummary({ pushed: 3 }), { kind: "ok", text: "已保存，已通知 3 个在线角色" });
  assert.deepEqual(pushSummary({ pushed: 0 }), { kind: "ok", text: "已保存，已通知 0 个在线角色" });
  assert.deepEqual(pushSummary({ pushed: 2 }, "已删除"), { kind: "ok", text: "已删除，已通知 2 个在线角色" });
  const w = pushSummary({ pushed: null, push_error: "dial tcp" });
  assert.equal(w.kind, "warn");
  assert.ok(w.text.startsWith("已保存") && w.text.includes(GATEWAY_DOWN) && !w.text.includes("dial tcp"), w.text); // no raw error text
  assert.deepEqual(pushSummary({}), { kind: "ok", text: "已保存" });
  assert.deepEqual(pushSummary(null), { kind: "ok", text: "已保存" });
});

test("itemIdOf reads the id out of what the item box holds", () => {
  assert.equal(itemIdOf("10034 生命金卡"), "10034"); // as the list fills it
  assert.equal(itemIdOf("10034"), "10034");
  assert.equal(itemIdOf("  40112  "), "40112");
  assert.equal(itemIdOf("生命金卡"), ""); // a name that was not chosen from the list
  assert.equal(itemIdOf("10034x"), "");   // not an id followed by a space
  assert.equal(itemIdOf(""), "");
  assert.equal(itemIdOf(null), "");
});

test("an item attachment needs an id, which the box yields from a chosen entry", () => {
  const base = { scope: "broadcast", subject: "s", from: "GM", type: 1 };
  const ok = buildMailPayload({ ...base, attachments: [{ kind: "item", asset: "", itemID: itemIdOf("10034 生命金卡"), num: "3" }] });
  assert.deepEqual(ok.payload.attachments, [{ kind: "item", item_id: 10034, num: 3 }]);
  const bad = buildMailPayload({ ...base, attachments: [{ kind: "item", asset: "", itemID: itemIdOf("生命金卡"), num: "3" }] });
  assert.match(bad.error, /从列表中选择物品/);
});

test("fmtBytes writes sizes the way people read them", () => {
  assert.equal(fmtBytes(0), "0 B");
  assert.equal(fmtBytes(1023), "1023 B");
  assert.equal(fmtBytes(1024), "1 KB");
  assert.equal(fmtBytes(212992), "208 KB");
  assert.equal(fmtBytes(1536), "1.5 KB");
  assert.equal(fmtBytes(5 * 1024 * 1024), "5 MB");
  assert.equal(fmtBytes(3.5 * 1024 ** 3), "3.5 GB");
  assert.equal(fmtBytes(-1), "-");
  assert.equal(fmtBytes("x"), "-");
  assert.equal(fmtBytes(undefined), "-");
});

test("buildGrantPayload takes the attachment rows of a direct grant", () => {
  const r = buildGrantPayload([{ kind: "asset", asset: "gold", itemID: "", num: "500" }, { kind: "item", asset: "gold", itemID: "10401", num: "3" }]);
  assert.deepEqual(r.payload, { grants: [{ kind: "asset", asset: "gold", num: 500 }, { kind: "item", item_id: 10401, num: 3 }] });
  const bad = (rows, text) => { const x = buildGrantPayload(rows); assert.ok(x.error && x.error.includes(text), `${JSON.stringify(rows)} -> ${x.error}`); };
  bad([], "至少");
  bad([{ kind: "asset", asset: "gold", itemID: "", num: "0" }], "数量");
  bad([{ kind: "item", asset: "gold", itemID: "", num: "5" }], "物品");
  bad(Array.from({ length: LIMITS.maxGrants + 1 }, () => ({ kind: "asset", asset: "gold", itemID: "", num: "1" })), String(LIMITS.maxGrants));
  // a grant may hold more rows than a mail does
  assert.ok(LIMITS.maxGrants > LIMITS.maxAttachments);
});

test("grantSummary says whether the online role saw it", () => {
  assert.equal(grantSummary({ refreshed: true }).kind, "ok");
  assert.match(grantSummary({ refreshed: true }).text, /实时刷新/);
  assert.match(grantSummary({ refreshed: false }).text, /不在线/);
  assert.equal(grantSummary({ push_error: "down" }).kind, "warn");
});

test("buildHeroGrantPayload checks hero, star and count", () => {
  const heroes = [{ bid: 10501, name: "幽冥公主", camp: 1, init_star: 5, max_star: 10 }, { bid: 10401, name: "修罗", camp: 1, init_star: 4, max_star: 5 }];
  assert.deepEqual(buildHeroGrantPayload([{ bid: "10501", star: "5", num: "" }], heroes).payload, { heroes: [{ bid: 10501, star: 5, num: 1 }] });
  assert.ok(buildHeroGrantPayload([], heroes).error);
  assert.ok(buildHeroGrantPayload([{ bid: "1", star: "5", num: "1" }], heroes).error, "unknown hero");
  assert.ok(buildHeroGrantPayload([{ bid: "10401", star: "6", num: "1" }], heroes).error, "star above the table");
  assert.ok(buildHeroGrantPayload([{ bid: "10401", star: "4", num: "21" }], heroes).error, "too many");
  assert.ok(buildHeroGrantPayload([{ bid: "10401", star: "4", num: "1.5" }], heroes).error, "not whole");
  assert.equal(heroGrantSummary({ granted: [{}, {}], refreshed: true }).kind, "ok");
  assert.equal(heroGrantSummary({ granted: [{}], push_error: "x" }).kind, "warn");
});

test("activity aims are written one to a line and read back", () => {
  const aims = [{ id: 1, str: "每日登录", event: "evt_cli_loaded", target: 1, rewards: [[3, 100], [10403, 2]] }];
  assert.equal(aimsToText(aims), "1 | 每日登录 | evt_cli_loaded | 1 | 3:100,10403:2");
  assert.deepEqual(parseAims(aimsToText(aims)).aims, aims);
  assert.match(parseAims("1 | a | b").error, /5 段/);
  assert.match(parseAims("1 | a | | 1 | 3-100").error, /物品编号:数量/);
  assert.match(parseAims("1 | a | | 1 | 3:1\n1 | b | | 1 | 3:1").error, /重复/);
  assert.match(parseAims("1 | a | | 1 | ").error, /1 到 10/);
});

test("the activity form is checked like the API and keeps the spec keys it does not edit", () => {
  const f = { srvID: "", campID: "9001", bid: "9001", title: "每日福利", panelType: "16", startAt: "2026-10-18T10:00", endAt: "", aims: "1 | 登录 | evt_cli_loaded | 1 | 3:100", status: true, daily: true };
  const r = buildActivityPayload(f, { ico: "x" });
  assert.equal(r.error, undefined);
  assert.deepEqual(r.payload.spec, { ico: "x", title: "每日福利", panel_type: 16, daily: true, aims: [{ id: 1, str: "登录", event: "evt_cli_loaded", target: 1, rewards: [[3, 100]] }] });
  assert.equal(r.payload.end_at, 0);
  assert.equal(r.payload.status, 1);
  assert.match(buildActivityPayload({ ...f, panelType: "200" }).error, /145/);
  assert.match(buildActivityPayload({ ...f, title: "" }).error, /标题/);
  assert.match(buildActivityPayload({ ...f, endAt: "2026-10-17T10:00" }).error, /晚于/);
  assert.match(buildActivityPayload({ ...f, campID: "0" }).error, /活动编号/);
});
