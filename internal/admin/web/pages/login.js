import { post, session, ApiError } from "/api.js";
import { h, field } from "/dom.js";
import { lockText } from "/lib.js";

export function render(root, ctx) {
  const msg = h("p", { class: "form-error", role: "alert", text: ctx.message || "" });
  sessionStorage.removeItem("login.notice");
  const user = h("input", { name: "username", autocomplete: "username", required: true, autofocus: true });
  const pass = h("input", { name: "password", type: "password", autocomplete: "current-password", required: true });
  const btn = h("button", { class: "btn primary", type: "submit", text: "登录" });
  const form = h("form", { class: "login" },
    h("h1", { text: "2DRound 管理后台" }),
    field("用户名", user), field("密码", pass), msg, btn);
  form.addEventListener("submit", async (e) => {
    e.preventDefault();
    btn.disabled = true;
    msg.textContent = "";
    try {
      const r = await post("/api/login", { username: user.value, password: pass.value });
      session.set(r.token, r.admin);
      pass.value = "";
      ctx.navigate("dashboard");
      location.reload(); // start from a clean state with the new session
    } catch (err) {
      msg.textContent = err instanceof ApiError && err.status === 429 ? `登录失败次数过多，${lockText(err.retryAfter)}` : err.message;
      btn.disabled = false;
    }
  });
  root.append(h("div", { class: "login-wrap" }, form));
}
