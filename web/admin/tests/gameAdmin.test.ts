import test from "node:test";
import assert from "node:assert/strict";
import {
  resetPayload,
  reason,
  canReset,
  canSettle,
  worldPayload,
  settleFeedback,
  gameError,
  tablePath,
  type ModuleInfo
} from "../src/domain/gameAdmin.js";
import { actionAllowed, pageCapability } from "../src/domain/management.js";
import { ApiError } from "../src/api/client.js";
const module: ModuleInfo = {
  key: "monthcard",
  title: "月卡",
  window: "16705",
  scope: "role",
  has_view: true,
  has_rank: false,
  editable: false,
  resets: [
    { item: "0", title: "第一张" },
    { item: "1", title: "第二张" }
  ]
};
test("game administration reads available to all, reset/settlement/world writes only superadmin", () => {
  for (const role of ["viewer", "operator", "superadmin"]) {
    assert.equal(pageCapability("game-status"), "read");
    assert.equal(pageCapability("modules"), "read");
    assert.equal(actionAllowed(role, "game-status"), role === "superadmin");
    assert.equal(actionAllowed(role, "modules"), false);
    assert.equal(canReset(role, module), role === "superadmin");
    assert.equal(canSettle(role, "arenateam"), role === "superadmin");
  }
  assert.equal(canReset("superadmin", { ...module, resets: [] }), false);
  assert.equal(canSettle("superadmin", "activity"), false);
  assert.equal(canSettle("operator", "arenachamp"), false);
});
test("reset exact whitelist, no aliases and no implicit all; reason matches Go Unicode limits", () => {
  assert.deepEqual(resetPayload(module, "0", "人工核查"), { item: "0", reason: "人工核查" });
  for (const item of ["all", "00", "2", "", "constructor"]) assert.throws(() => resetPayload(module, item, "原因"));
  for (const why of ["", "  ", "x\n", "x\u007f", "x\u0085", "中".repeat(201)]) assert.throws(() => reason(why));
  assert.equal(reason("中".repeat(200)).length, 200);
  assert.equal(reason("😀".repeat(200)), "😀".repeat(200));
  assert.equal(module.editable, false);
});
test("world override 0/permanent, local expiration and invalid date/number checks", () => {
  assert.deepEqual(worldPayload(0, "", "停用覆盖"), { override: 0, until: 0, reason: "停用覆盖" });
  assert.equal(worldPayload(200, "2026-11-02T12:30", "调整").until, new Date(2026, 10, 2, 12, 30).getTime() / 1000);
  for (const n of [-1, 201, 1.5, NaN]) assert.throws(() => worldPayload(n, "", "原因"));
  assert.throws(() => worldPayload(1, "2026-02-30T12:00", "原因"));
  assert.throws(() => worldPayload(1, "", ""));
});
test("settlement feedback distinguishes repeated, not due and successful results", () => {
  assert.equal(settleFeedback({ settled: false, reason: "already" }), "本季已结算");
  assert.equal(settleFeedback({ settled: false, reason: "not_due" }), "尚未到期，未执行补结算");
  assert.equal(settleFeedback({ settled: true, reason: "settled" }), "到期补结算已完成");
});
test("online conflict offers offline guidance; concurrent conflict not mistaken for online", () => {
  assert.equal(gameError(409, new ApiError(409, "role is online; kick it offline first").message, true), "玩家在线，请先踢下线");
  assert.equal(gameError(409, "module changed concurrently", true), "数据已变化，请刷新后核对");
  assert.equal(gameError(502, "upstream failed", true), "网关不可用，未修改");
  assert.match(gameError(502, "invalid gateway response"), /只读请求失败/);
  assert.match(new ApiError(502, "gateway unavailable").message, /核对审计/);
});
test("registered table owner scope and paging paths keep identifiers encoded", () => {
  const t = { name: "role_mails", title: "收件", group: "邮件", scope: "role" as const, owner: "rid", columns: ["id"] };
  assert.equal(tablePath(t, 12, 2), "/roles/12/tables/role_mails?limit=50&offset=50");
  assert.equal(tablePath({ ...t, scope: "guild" }, 23), "/guilds/23/tables/role_mails?limit=50&offset=0");
  assert.equal(tablePath({ ...t, scope: "server" }, "a/1"), "/servers/a%2F1/tables/role_mails?limit=50&offset=0");
  assert.throws(() => tablePath(t, "", 1));
  assert.throws(() => tablePath(t, 1, 0));
});

test("charge views display backend projection values without deriving eligibility or rolling state", async () => {
  const { chargePresentation } = await import("../src/domain/gameAdmin.js");
  const source = {
    key: "monthcard",
    exists: true,
    data: null,
    view: {
      protocols: { "16705": { card1_acc: 98, card1_days: 2, card1_is_reward: 1 } },
      state: { Base: [12345] },
      details: { cards: [{ index: 0, active: true, remaining_days: 28 }] }
    }
  };
  const original = JSON.stringify(source),
    view = chargePresentation(source)!;
  assert.equal(view.rows[0].acc, 98);
  assert.equal(view.rows[0].remaining, 28);
  assert.equal(view.rows[0].status, "可领取");
  assert.equal(view.rows[0].base, 12345);
  assert.equal(JSON.stringify(source), original, "rendering does not change projection");
  assert.equal(chargePresentation({ key: "activity", exists: false, data: null }), undefined);
  const limited = chargePresentation({
    key: "limitgift",
    exists: true,
    data: null,
    view: { details: { entries: [{ id: 20, supported: false, offered: false, bought: false }] } }
  })!;
  assert.equal(limited.rows[0].supported, "否");
  const cycle = chargePresentation({
    key: "cyclegift",
    exists: true,
    data: null,
    view: { protocols: { "21007": [{ type: 2, ref_time: 123, first_gift: [{ id: 411, count: 3 }] }] } }
  })!;
  assert.equal(cycle.rows[0].cycle, "月礼包");
  assert.equal(cycle.rows[0].count, 3);
});
