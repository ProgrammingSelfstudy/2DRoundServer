import { get, post } from "/api.js";
import { h, modal, field, badge, table, toast } from "/dom.js";
import { fmtTime, truncate } from "/lib.js";

const STATES = { 1: ["待回复", "warn"], 2: ["已回复", ""], 3: ["已解决", "ok"], 4: ["已评分", "ok"] };

export async function render(root, ctx) {
  const box = h("div", {});
  const canWrite = ctx.can("write");
  let onlyOpen = false;

  async function load() {
    const r = await get("/api/tickets?limit=100");
    const items = onlyOpen ? r.items.filter((t) => t.state === 1) : r.items;
    box.replaceChildren(table(["ID", "角色", "区服", "类型", "标题", "状态", "评分", "提交", "回复", canWrite ? "操作" : ""].filter(Boolean), items.map((t) => {
      const [text, kind] = STATES[t.state] || ["未知", ""];
      return h("tr", {},
        h("td", { text: t.id }), h("td", { text: t.rid }), h("td", { text: t.srv_id }), h("td", { text: t.issue_type }), h("td", { text: truncate(t.title, 30) }),
        h("td", {}, badge(text, kind)), h("td", { text: t.score || "-" }), h("td", { text: fmtTime(t.created_at) }), h("td", { text: t.answered_at ? fmtTime(t.answered_at) : "-" }),
        h("td", { class: "actions" }, h("button", { class: "btn", type: "button", text: canWrite ? "查看 / 回复" : "查看", onclick: () => open(t) })));
    })));
  }

  function open(t) {
    const thread = h("div", { class: "thread" },
      ...t.questions.flatMap((q, i) => [h("p", {}, h("strong", { text: `玩家 ${i + 1}：` }), q), t.answers[i] ? h("p", {}, h("strong", { text: "客服：" }), t.answers[i]) : null]),
      ...t.answers.slice(t.questions.length).map((a) => h("p", {}, h("strong", { text: "客服：" }), a)));
    const info = h("p", { class: "hint", text: `角色 ${t.rid}（${t.srv_id}）  手机 ${t.phone || "-"}  邮箱 ${t.email || "-"}  机型 ${t.phone_info || "-"}` });
    const text = h("textarea", { rows: 5, maxlength: 1000, placeholder: "回复内容，玩家下次打开工单列表时看到" });
    const err = h("p", { class: "form-error", role: "alert" });
    modal({
      title: `工单 #${t.id}：${t.title}`, wide: true, body: h("div", {}, info, thread, canWrite ? field("回复", text) : null, err),
      actions: [{ label: "关闭", onclick: (c) => c() }, canWrite ? {
        label: "发送回复", kind: "primary", onclick: async (close) => {
          if (!text.value.trim()) { err.textContent = "回复不能为空"; return; }
          try { await post(`/api/tickets/${t.id}/reply`, { content: text.value }); close(); toast("已回复"); await load(); } catch (e) { err.textContent = e.message; }
        },
      } : null].filter(Boolean),
    });
  }

  const only = h("input", { type: "checkbox" });
  only.addEventListener("change", () => { onlyOpen = only.checked; load().catch((e) => toast(e.message, "err")); });
  root.append(h("div", { class: "toolbar" }, h("label", { class: "field check" }, only, " 只看待回复")),
    h("p", { class: "hint", text: "玩家在游戏设置里提交的客服工单，最近 100 条。回复后玩家再打开工单列表就能看到；状态含义（待回复 / 已回复 / 已解决 / 已评分）照客户端文字。" }), box);
  await load();
}
