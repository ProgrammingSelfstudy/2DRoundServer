import test from "node:test";
import assert from "node:assert/strict";
import {
  activityForm,
  activityPayload,
  activityState,
  ambiguousStatus,
  attachments,
  canOperate,
  localInput,
  localUnix,
  mailFeedback,
  mailPayload,
  manualOrder,
  parseAims,
  paymentFeedback,
  roleIDs,
  SubmissionGate,
  validateQr,
  type Activity,
  type MailForm,
  type MailReply,
  type OrderReply
} from "../src/domain/operations.js";
const assets = [{ name: "gold", label: 3 }];
const mail = (): MailForm => ({
  scope: "direct",
  roles: "1, 2 1",
  srv_id: "uitest_1",
  subject: "测试邮件",
  content: "正文",
  from_name: "GM",
  type: 1,
  expiry: "days",
  expire_days: 7,
  expire_at: ""
});
test("operation capability mapping matches Go: payment requires manage, mail/activity require write", () => {
  assert.deepEqual(
    ["mail", "pay", "activities"].map(s => canOperate("viewer", s as "mail" | "pay" | "activities")),
    [false, false, false]
  );
  assert.deepEqual(
    ["mail", "pay", "activities"].map(s => canOperate("operator", s as "mail" | "pay" | "activities")),
    [true, false, true]
  );
  assert.deepEqual(
    ["mail", "pay", "activities"].map(s => canOperate("superadmin", s as "mail" | "pay" | "activities")),
    [true, true, true]
  );
  assert.equal(canOperate("constructor", "pay"), false);
});
test("mail direct/broadcast mapping never mixes recipients, server scope or expiry modes", () => {
  const direct = mailPayload(mail(), [], assets);
  assert.deepEqual("role_ids" in direct && direct.role_ids, [1, 2]);
  assert.equal(Object.hasOwn(direct, "srv_id"), false);
  assert.equal(Object.hasOwn(direct, "expire_at"), false);
  const broadcast = mailPayload({ ...mail(), scope: "broadcast", roles: "invalid-hidden-field" }, [], assets);
  assert.equal("srv_id" in broadcast && broadcast.srv_id, "uitest_1");
  assert.equal(Object.hasOwn(broadcast, "role_ids"), false);
  const all = mailPayload({ ...mail(), scope: "broadcast", srv_id: "", expire_days: 0 }, [], assets);
  assert.equal("srv_id" in all && all.srv_id, "");
  assert.equal(all.expire_days, 0);
  const at = localInput(Math.floor(Date.now() / 1000) + 3600);
  const timed = mailPayload({ ...mail(), expiry: "date", expire_at: at }, [], assets);
  assert.ok("expire_at" in timed && timed.expire_at);
  assert.equal(Object.hasOwn(timed, "expire_days"), false);
  assert.throws(() => mailPayload({ ...mail(), expiry: "date", expire_at: "2000-01-01T00:00" }, [], assets), /晚于现在/);
});
test("mail validates role IDs, byte-safe integers, limits and Unicode lengths", () => {
  assert.deepEqual(roleIDs("1，2；3\n1"), [1, 2, 3]);
  for (const value of ["0", "-1", "1.5", "9007199254740993", "hello"]) assert.throws(() => roleIDs(value));
  assert.throws(() => roleIDs(Array.from({ length: 501 }, (_, i) => i + 1).join(",")), /500/);
  assert.throws(() => mailPayload({ ...mail(), roles: "" }, [], assets));
  assert.throws(() => mailPayload({ ...mail(), subject: "字".repeat(61) }, [], assets));
  assert.doesNotThrow(() => mailPayload({ ...mail(), subject: "😀".repeat(60) }, [], assets));
  assert.throws(() => mailPayload({ ...mail(), expire_days: 366 }, [], assets));
  assert.throws(() => mailPayload({ ...mail(), type: 2 }, [], assets));
});
test("currency/item attachments have the exact Go shape and enforce all quantity boundaries", () => {
  const rows = [
    { key: 1, kind: "asset" as const, asset: "gold", num: 100 },
    { key: 2, kind: "item" as const, asset: "", item_id: 10034, num: 2 }
  ];
  assert.deepEqual(attachments(rows, assets), [
    { kind: "asset", asset: "gold", num: 100 },
    { kind: "item", item_id: 10034, num: 2 }
  ]);
  assert.throws(() => attachments([...rows, ...rows, ...rows, ...rows, ...rows], assets), /最多/);
  for (const num of [undefined, 0, -1, 1.2, 1000000001]) assert.throws(() => attachments([{ ...rows[0], num }], assets));
  assert.throws(() => attachments([{ ...rows[0], asset: "unknown" }], assets));
});
test("mail saved/push feedback never equates broadcast creation with all recipients receiving it", () => {
  const result = { mail: { id: 1, scope: "broadcast" }, pushed: null, push_error: "gateway unavailable" } as unknown as MailReply;
  assert.equal(mailFeedback(result).type, "warning");
  assert.match(mailFeedback(result).message, /逐步投递/);
  assert.match(mailFeedback(result).message, /未能确认/);
  assert.equal(mailFeedback({ ...result, pushed: 0, push_error: undefined }).type, "success");
  assert.match(mailFeedback({ ...result, pushed: 0, push_error: undefined }).message, /0 个/);
});
test("manual orders only accept supported products and distinguish pending/delivered/unpushed", () => {
  const products = [{ id: 4, name: "普通充值30", yuan: 30 }];
  assert.deepEqual(manualOrder(1, 4, false, "测试", products), { rid: 1, charge_id: 4, confirm: false, note: "测试" });
  assert.throws(() => manualOrder(1, 101, true, "", products), /支持/);
  assert.throws(() => manualOrder(0, 4, true, "", products));
  const pending = { order: { order_no: "test", status: 0 } } as OrderReply;
  assert.match(paymentFeedback(pending).message, /尚未发货/);
  const delivered = { order: { order_no: "test", status: 1 }, push_error: "unavailable" } as OrderReply;
  assert.equal(paymentFeedback(delivered).type, "warning");
  assert.match(paymentFeedback(delivered).message, /请勿重复/);
  assert.match(paymentFeedback({ ...delivered, push_error: undefined, refreshed: false }).message, /离线/);
});
test("QR codes enforce MIME/size before reading; no external URL upload is used", () => {
  assert.doesNotThrow(() => validateQr({ type: "image/png", size: 600 * 1024 }));
  for (const file of [
    { type: "image/svg+xml", size: 50 },
    { type: "image/png", size: 600 * 1024 + 1 },
    { type: "image/webp", size: 0 }
  ])
    assert.throws(() => validateQr(file));
});
const activity = (): Activity => ({
  id: 7,
  srv_id: "uitest_1",
  camp_id: 900,
  bid: 901,
  status: 1,
  start_at: Math.floor(Date.now() / 1000) - 60,
  end_at: 0,
  spec: {
    title: "旧标题",
    panel_type: 16,
    ico: "资源图标",
    top_banner: "原始横幅",
    reward_title: "奖励标题",
    channel_ban: "保留渠道",
    aims: [{ id: 1, str: "登录", event: "evt_cli_loaded", target: 1, rewards: [[3, 100]], future_aim: { preserve: true } }],
    future_extension: { array: [1, "原值"], nested: { enabled: true } }
  }
});
test("activity edit/copy preserves unexposed and future fields without mutating source or posting IDs", () => {
  const original = activity();
  const before = JSON.stringify(original);
  const form = activityForm(original);
  form.title = "新标题";
  const result = activityPayload(form, original);
  assert.equal(result.spec.title, "新标题");
  assert.deepEqual(result.spec.future_extension, original.spec.future_extension);
  assert.equal(result.spec.ico, original.spec.ico);
  assert.equal(result.spec.top_banner, original.spec.top_banner);
  assert.equal(result.spec.reward_title, original.spec.reward_title);
  assert.deepEqual(result.spec.aims?.[0].future_aim, { preserve: true });
  assert.equal(JSON.stringify(original), before);
  assert.equal(Object.hasOwn(result, "id"), false);
  assert.equal(result.camp_id, original.camp_id);
  assert.equal(result.bid, original.bid);
  assert.equal(result.start_at, original.start_at);
  (result.spec.future_extension as { nested: { enabled: boolean } }).nested.enabled = false;
  assert.equal((original.spec.future_extension as { nested: { enabled: boolean } }).nested.enabled, true);
});
test("activity target syntax and date validation reject silent coercion and accidental reward loss", () => {
  assert.deepEqual(parseAims("1 | 直接领取 | | 0 | 3:100,10034:2")[0].rewards, [
    [3, 100],
    [10034, 2]
  ]);
  for (const line of [
    "1 | a | e | 1 | 3:0",
    "1 | a | e | 1 | 3:-1",
    "1 | a | e | | 3:1",
    "1 | a | e | 1 | 3:1\n1 | b | e | 1 | 3:2"
  ])
    assert.throws(() => parseAims(line));
  assert.throws(() => localUnix("2026-02-30T12:00"), /不存在/);
  assert.throws(() => localUnix("invalid"));
  const timestamp = Math.floor(Date.now() / 1000);
  assert.equal(localUnix(localInput(timestamp)), timestamp);
  const original = activity();
  const form = activityForm(original);
  form.end = localInput(original.start_at - 1);
  assert.throws(() => activityPayload(form, original), /晚于/);
  form.end = "";
  form.extra = "[]";
  assert.throws(() => activityPayload(form, original), /对象/);
  form.extra = '{"title":"override"}';
  assert.throws(() => activityPayload(form, original), /重复/);
});
test("activity state boundaries and closed precedence match the old UI", () => {
  const a = { status: 1, start_at: 100, end_at: 200 };
  assert.equal(activityState(a, 99), "未开始");
  assert.equal(activityState(a, 100), "进行中");
  assert.equal(activityState(a, 200), "已结束");
  assert.equal(activityState({ ...a, status: 0 }, 150), "关闭");
  assert.equal(activityState({ ...a, end_at: 0 }, 100000), "进行中");
});
test("submission gate blocks double clicks, pending confirmation and ambiguous retry until explicit review", () => {
  const gate = new SubmissionGate();
  assert.equal(gate.begin(), true);
  assert.equal(gate.begin(), false);
  gate.reviewed();
  assert.equal(gate.begin(), false);
  gate.finish(true);
  assert.equal(gate.begin(), false);
  gate.reviewed();
  assert.equal(gate.begin(), true);
  gate.finish();
  assert.equal(gate.begin(), true);
  for (const status of [undefined, 0, 200, 201, 500, 503]) assert.equal(ambiguousStatus(status), true);
  for (const status of [400, 401, 403, 404, 409, 422, 429]) assert.equal(ambiguousStatus(status), false);
});

test("editing a zero-start schedule preserves its unlimited start and signed optional integers", () => {
  const original = activity();
  original.start_at = 0;
  original.spec.sort_val = -2;
  original.spec.type_ico = -1;
  const form = activityForm(original);
  assert.equal(form.start, "");
  const result = activityPayload(form, original);
  assert.equal(result.start_at, 0);
  assert.equal(result.spec.sort_val, -2);
  assert.equal(result.spec.type_ico, -1);
});
