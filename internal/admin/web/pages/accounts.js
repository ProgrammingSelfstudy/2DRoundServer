import { get, post } from "/api.js";
import { h, modal, field, badge, table, pager, toast } from "/dom.js";
import { fmtTime, pageInfo, queryString } from "/lib.js";

export async function render(root, ctx) {
  const limit = 25;
  const q = h("input", { type: "search", placeholder: "按账号名或 ID 搜索", value: ctx.params.get("q") || "" });
  const box = h("div", {});
  let offset = 0;

  async function load() {
    const r = await get("/api/accounts" + queryString({ q: q.value.trim(), limit, offset }));
    const canWrite = ctx.can("write");
    box.replaceChildren(
      table(["ID", "平台", "账号", "角色数", "状态", "注册时间", "最近登录", canWrite ? "操作" : ""].filter(Boolean), r.items.map((a) => h("tr", {},
        h("td", { text: a.id }), h("td", { text: a.platform }), h("td", { text: a.account }), h("td", {}, h("a", { href: "#/roles" + queryString({ q: a.account }), text: a.role_count })),
        h("td", {}, a.status === 1 ? badge("正常", "ok") : badge("已封禁", "err"), a.ban_reason ? h("small", { class: "hint", text: " " + a.ban_reason }) : null),
        h("td", { text: fmtTime(a.created_at) }), h("td", { text: fmtTime(a.last_login_at) }),
        canWrite ? h("td", { class: "actions" }, a.status === 1
          ? h("button", { class: "btn danger", type: "button", text: "封禁", onclick: () => ban(a) })
          : h("button", { class: "btn", type: "button", text: "解封", onclick: () => unban(a) })) : null))),
      pager(pageInfo(r.total, limit, offset), (o) => { offset = o; load().catch((e) => toast(e.message, "err")); }));
  }

  function ban(a) {
    const reason = h("input", { placeholder: "封禁原因（最多 200 字，玩家看不到）", maxlength: 200 });
    const err = h("p", { class: "form-error", role: "alert" });
    modal({
      title: `封禁账号 ${a.account}`, body: h("div", {}, h("p", { text: "封禁会让该账号的登录凭证立即失效，并把它名下在线的角色踢下线。" }), field("原因", reason), err),
      actions: [{ label: "取消", onclick: (c) => c() }, {
        label: "封禁", kind: "danger", onclick: async (close) => {
          try { await post(`/api/accounts/${a.id}/ban`, { reason: reason.value }); close(); toast("已封禁"); await load(); } catch (e) { err.textContent = e.message; }
        },
      }],
    });
  }

  async function unban(a) {
    try { await post(`/api/accounts/${a.id}/unban`); toast("已解封"); await load(); } catch (e) { toast(e.message, "err"); }
  }

  q.addEventListener("keydown", (e) => { if (e.key === "Enter") { offset = 0; load().catch((x) => toast(x.message, "err")); } });
  root.append(h("div", { class: "toolbar" }, q, h("button", { class: "btn", type: "button", text: "搜索", onclick: () => { offset = 0; load().catch((e) => toast(e.message, "err")); } })), box);
  await load();
}
