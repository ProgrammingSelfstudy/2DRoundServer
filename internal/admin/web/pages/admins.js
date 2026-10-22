import { get, post, patch } from "/api.js";
import { h, modal, field, select, badge, table, toast } from "/dom.js";
import { fmtTime, roleLabel } from "/lib.js";

export async function render(root, ctx) {
  const box = h("div", {});
  const roles = [["viewer", "只读"], ["operator", "运营"], ["superadmin", "超级管理员"]];

  async function load() {
    const { items } = await get("/api/admins");
    box.replaceChildren(table(["ID", "用户名", "角色", "状态", "最近登录", "创建时间", "操作"], items.map((a) => {
      const me = a.id === ctx.admin.id;
      const roleSel = select(roles, a.role, { disabled: me });
      return h("tr", {},
        h("td", { text: a.id }), h("td", { text: a.username + (me ? "（我）" : "") }), h("td", {}, roleSel),
        h("td", {}, a.status === 1 ? badge("启用", "ok") : badge("已禁用", "err")),
        h("td", { text: fmtTime(a.last_login_at) }), h("td", { text: fmtTime(a.created_at) }),
        h("td", { class: "actions" },
          h("button", { class: "btn", type: "button", text: "保存角色", disabled: me, onclick: () => update(a, { role: roleSel.value }, "角色已更新") }),
          h("button", { class: "btn", type: "button", text: a.status === 1 ? "禁用" : "启用", disabled: me, onclick: () => update(a, { status: a.status === 1 ? 0 : 1 }, "状态已更新") }),
          h("button", { class: "btn", type: "button", text: "重置密码", onclick: () => reset(a) })));
    })));
  }

  async function update(a, body, ok) {
    try { await patch(`/api/admins/${a.id}`, body); toast(ok); await load(); } catch (e) { toast(e.message, "err"); }
  }

  function reset(a) {
    const pw = h("input", { type: "password", autocomplete: "new-password" });
    const err = h("p", { class: "form-error", role: "alert" });
    modal({
      title: `重置 ${a.username} 的密码`, body: h("div", {}, h("p", { text: "重置后该管理员所有已登录的会话都会失效。" }), field("新密码（8 到 72 位）", pw), err),
      actions: [{ label: "取消", onclick: (c) => c() }, {
        label: "重置", kind: "primary", onclick: async (close) => {
          try { await patch(`/api/admins/${a.id}`, { password: pw.value }); close(); toast("密码已重置"); await load(); } catch (e) { err.textContent = e.message; }
        },
      }],
    });
  }

  function create() {
    const f = { username: h("input", { autocomplete: "off" }), password: h("input", { type: "password", autocomplete: "new-password" }), role: select(roles, "operator") };
    const err = h("p", { class: "form-error", role: "alert" });
    modal({
      title: "新建管理员", body: h("div", {}, field("用户名", f.username, "3 到 32 位字母、数字、_ . -"), field("密码", f.password, "8 到 72 位"), field("角色", f.role), err),
      actions: [{ label: "取消", onclick: (c) => c() }, {
        label: "创建", kind: "primary", onclick: async (close) => {
          try { await post("/api/admins", { username: f.username.value, password: f.password.value, role: f.role.value }); close(); toast("已创建"); await load(); } catch (e) { err.textContent = e.message; }
        },
      }],
    });
  }

  root.append(h("div", { class: "toolbar" }, h("button", { class: "btn primary", type: "button", text: "新建管理员", onclick: create })), box);
  await load();
}
