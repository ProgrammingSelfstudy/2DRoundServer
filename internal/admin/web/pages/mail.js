import { get, post } from "/api.js";
import { h, field, select, badge, table, pager, toast } from "/dom.js";
import { buildMailPayload, fmtTime, pageInfo, truncate, LIMITS, GATEWAY_DOWN } from "/lib.js";
import { attachRows } from "/attach.js";

export async function render(root, ctx) {
  const [{ items: servers }, meta] = await Promise.all([get("/api/servers"), get("/api/meta")]);
  const canWrite = ctx.can("write");
  const sent = h("div", {});
  const limit = 15;
  let offset = 0;

  async function loadSent() {
    const r = await get(`/api/mails?limit=${limit}&offset=${offset}`);
    sent.replaceChildren(
      table(["ID", "范围", "标题", "附件", "收件 / 已领", "有效期", "发送人", "时间"], r.items.map((m) => h("tr", {},
        h("td", { text: m.id }), h("td", {}, m.scope === "broadcast" ? badge(m.srv_id ? `广播 ${m.srv_id}` : "广播 全服") : badge("定向")),
        h("td", { text: truncate(m.subject, 24) }),
        h("td", { text: (m.attachments || []).map((a) => `${a.kind === "asset" ? assetName(a.item_id) : ((r.item_names || {})[a.item_id] || "物品" + a.item_id)}×${a.num}`).join("，") || "-" }),
        h("td", { text: `${m.recipients} / ${m.claimed}` }), h("td", { text: m.expire_at ? fmtTime(m.expire_at) : "永久" }),
        h("td", { text: `${m.from_name}（${m.created_by}）` }), h("td", { text: fmtTime(m.created_at) })))),
      pager(pageInfo(r.total, limit, offset), (o) => { offset = o; loadSent().catch((e) => toast(e.message, "err")); }));
  }

  const names = new Map(meta.assets.map((a) => [a.label, a.name]));
  const assetName = (label) => names.get(label) || `货币${label}`;

  const attach = attachRows(meta);
  const rows = attach.box;
  const addRow = attach.add;

  const scope = select([["direct", "指定角色"], ["broadcast", "广播"]], ctx.params.get("to") ? "direct" : "direct");
  const roleText = h("textarea", { rows: 2, placeholder: "角色 ID，用逗号或空格分隔（可在「角色」页查找）", value: ctx.params.get("to") || "" });
  const srv = select([["", "全服"], ...servers.map((s) => [s.srv_id, s.name + "（" + s.srv_id + "）"])], "");
  const subject = h("input", { maxlength: 60, placeholder: "标题（最多 60 字）" });
  const content = h("textarea", { rows: 5, maxlength: 2000, placeholder: "正文（最多 2000 字）" });
  const from = h("input", { value: "GM", maxlength: 20 });
  const type = select([[1, "类型 1"], [3, "类型 3"]], 1);
  const expire = h("input", { type: "number", min: 0, max: 365, value: 7 });
  const result = h("div", { class: "result", role: "status" });
  const err = h("p", { class: "form-error", role: "alert" });
  const send = h("button", { class: "btn primary", type: "button", text: "发送" });

  const syncScope = () => { roleText.parentElement.hidden = scope.value !== "direct"; srv.parentElement.hidden = scope.value !== "broadcast"; };
  scope.addEventListener("change", syncScope);

  send.addEventListener("click", async () => {
    err.textContent = ""; result.replaceChildren();
    const r = buildMailPayload({ scope: scope.value, roleText: roleText.value, srvID: srv.value, subject: subject.value, content: content.value,
      from: from.value, type: type.value, expireDays: expire.value, attachments: attach.read() });
    if (r.error) { err.textContent = r.error; return; }
    send.disabled = true;
    try {
      const out = await post("/api/mails", r.payload);
      const lines = [`邮件 #${out.mail.id} 已发送（${out.mail.scope === "broadcast" ? "广播" : "定向"}）。`];
      lines.push(out.push_error ? `没能实时推送（${GATEWAY_DOWN}）。邮件已保存，玩家下次登录时能收到。` : `已实时推送给 ${out.pushed} 个在线角色。`);
      result.replaceChildren(...lines.map((t, i) => h("p", { text: t, class: out.push_error && i ? "warn" : "ok", title: i && out.push_error ? out.push_error : "" })));
      subject.value = ""; content.value = ""; attach.clear();
      offset = 0; await loadSent();
    } catch (e) { err.textContent = e.message; } finally { send.disabled = false; }
  });

  const compose = h("div", { class: "panel" },
    h("h3", { text: "写邮件" }),
    h("div", { class: "form-grid" },
      field("发送范围", scope), field("角色 ID", roleText), field("广播区服", srv),
      field("标题", subject), field("发件人", from), field("类型", type), field("有效天数", expire, "0 = 永久，最多 365"),
      field("正文", content)),
    h("h4", { text: "附件" }), h("p", { class: "hint", text: `最多 ${LIMITS.maxAttachments} 个。货币直接进入玩家资产；物品进入玩家背包（只显示在「道具」页的物品，装备类暂不显示）。` }),
    rows, h("button", { class: "btn", type: "button", text: "添加附件", onclick: () => addRow() }),
    err, result, h("div", { class: "toolbar" }, send));
  root.append(canWrite ? compose : h("p", { class: "hint", text: "你的角色没有发送邮件的权限。" }), h("h3", { text: "已发送" }), sent);
  syncScope();
  await loadSent();
}
