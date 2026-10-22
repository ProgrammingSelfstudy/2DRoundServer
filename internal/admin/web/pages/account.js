import { post, session } from "/api.js";
import { h, field } from "/dom.js";
import { roleLabel } from "/lib.js";

export async function render(root, ctx) {
  const old = h("input", { type: "password", autocomplete: "current-password" });
  const n1 = h("input", { type: "password", autocomplete: "new-password" });
  const n2 = h("input", { type: "password", autocomplete: "new-password" });
  const msg = h("p", { class: "form-error", role: "alert" });
  const btn = h("button", { class: "btn primary", type: "submit", text: "修改密码" });
  const form = h("form", { class: "panel narrow" },
    h("p", { text: `当前登录：${ctx.admin.username}（${roleLabel(ctx.admin.role)}）` }),
    field("当前密码", old), field("新密码（8 到 72 位）", n1), field("再输入一次新密码", n2), msg, btn);
  form.addEventListener("submit", async (e) => {
    e.preventDefault();
    msg.textContent = "";
    if (n1.value !== n2.value) { msg.textContent = "两次输入的新密码不一致"; return; }
    btn.disabled = true;
    try {
      await post("/api/me/password", { old: old.value, new: n1.value });
      session.clear();
      sessionStorage.setItem("login.notice", "密码已修改，请用新密码重新登录。");
      location.hash = "#/login";
      location.reload();
    } catch (err) { msg.textContent = err.message; btn.disabled = false; }
  });
  root.append(form, h("p", { class: "hint", text: "修改密码后，所有已登录的会话（包括这一个）都会失效。" }));
}
