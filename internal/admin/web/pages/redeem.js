import { get, post, del } from "/api.js";
import { h, modal, field, badge, table, toast, confirmBox } from "/dom.js";
import { buildAttachments, fmtTime, LIMITS } from "/lib.js";
import { attachRows } from "/attach.js";

export async function render(root, ctx) {
  const meta = await get("/api/meta");
  const names = new Map(meta.assets.map((a) => [a.label, a.name]));
  const box = h("div", {});
  const canWrite = ctx.can("write");

  async function load() {
    const r = await get("/api/redeem-codes");
    box.replaceChildren(table(["兑换码", "备注", "奖励", "已用 / 上限", "有效期", "创建", canWrite ? "操作" : ""].filter(Boolean), r.items.map((c) => h("tr", {},
      h("td", {}, h("code", { text: c.code })), h("td", { text: c.note || "-" }),
      h("td", { text: (c.attachments || []).map((a) => `${a.kind === "asset" ? (names.get(a.item_id) || "货币" + a.item_id) : "物品" + a.item_id}×${a.num}`).join("，") }),
      h("td", { text: c.used + " / " + (c.max_uses || "不限") }),
      h("td", {}, c.expires_at ? (c.expires_at * 1000 < Date.now() ? badge("已过期") : badge(fmtTime(c.expires_at))) : badge("长期", "ok")),
      h("td", { text: fmtTime(c.created_at) }),
      canWrite ? h("td", { class: "actions" }, h("button", { class: "btn danger", type: "button", text: "删除", onclick: () => remove(c) })) : null))));
  }

  async function remove(c) {
    if (!(await confirmBox(`删除兑换码 ${c.code}？`, { danger: true, confirm: "删除" }))) return;
    try { await del(`/api/redeem-codes/${encodeURIComponent(c.code)}`); toast("已删除"); await load(); } catch (e) { toast(e.message, "err"); }
  }

  function create() {
    const attach = attachRows(meta);
    attach.add();
    const f = { code: h("input", { maxlength: 32, placeholder: "4~32 位字母或数字" }), note: h("input", { maxlength: 100 }),
      max: h("input", { type: "number", min: 0, value: 0 }), days: h("input", { type: "number", min: 0, value: 0 }) };
    const err = h("p", { class: "form-error", role: "alert" });
    modal({
      title: "新建兑换码", wide: true,
      body: h("div", { class: "form-grid" }, field("兑换码", f.code, "不分大小写，每个角色只能用一次"), field("备注", f.note),
        field("总次数上限", f.max, "0 = 不限"), field("有效天数", f.days, "0 = 长期有效"),
        field("奖励", h("div", {}, attach.box, h("button", { class: "btn", type: "button", text: "添加一项", onclick: () => attach.add() }))), err),
      actions: [{ label: "取消", onclick: (c) => c() }, {
        label: "创建", kind: "primary", onclick: async (close) => {
          const g = buildAttachments(attach.read(), LIMITS.maxGrants);
          if (g.error) { err.textContent = g.error; return; }
          if (!g.list.length) { err.textContent = "请至少添加一项奖励"; return; }
          try { await post("/api/redeem-codes", { code: f.code.value.trim(), note: f.note.value.trim(), grants: g.list, max_uses: Number(f.max.value) || 0, days: Number(f.days.value) || 0 }); close(); toast("已创建"); await load(); } catch (e) { err.textContent = e.message; }
        },
      }],
    });
  }

  root.append(canWrite ? h("div", { class: "toolbar" }, h("button", { class: "btn primary", type: "button", text: "新建兑换码", onclick: create })) : null,
    h("p", { class: "hint", text: "玩家在游戏 设置 → 兑换码 里输入。奖励直接发到背包。" }), box);
  await load();
}
