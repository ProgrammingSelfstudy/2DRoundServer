import { api, get, session, ApiError } from "/api.js";
import { h, clear, toast } from "/dom.js";
import { can, parseHash, roleLabel } from "/lib.js";

// One module per page; each exports render(root, ctx).
const PAGES = {
  dashboard: { title: "概览", need: "read", load: () => import("/pages/dashboard.js") },
  servers: { title: "区服", need: "read", load: () => import("/pages/servers.js") },
  accounts: { title: "账号", need: "read", load: () => import("/pages/accounts.js") },
  roles: { title: "角色", need: "read", load: () => import("/pages/roles.js") },
  mail: { title: "邮件", need: "read", load: () => import("/pages/mail.js") },
  pay: { title: "充值", need: "read", load: () => import("/pages/pay.js") },
  notices: { title: "公告", need: "read", load: () => import("/pages/notices.js") },
  audit: { title: "审计日志", need: "manage", load: () => import("/pages/audit.js") },
  backups: { title: "数据库备份", need: "manage", load: () => import("/pages/backups.js") },
  admins: { title: "管理员", need: "manage", load: () => import("/pages/admins.js") },
  account: { title: "我的账号", need: "read", load: () => import("/pages/account.js") },
};

const app = document.getElementById("app");
let renderToken = 0;

export function navigate(route, query) {
  const q = query ? "?" + new URLSearchParams(query).toString() : "";
  location.hash = `#/${route}${q}`;
}

function showLogin(message) {
  clear(app);
  import("/pages/login.js").then((m) => m.render(app, { navigate, message }));
}

async function route() {
  const { route: name, params } = parseHash(location.hash);
  if (!session.token) return showLogin(sessionStorage.getItem("login.notice") || "");
  const page = PAGES[name];
  const admin = session.admin;
  if (!page) return navigate("dashboard");
  if (!can(admin.role, page.need)) {
    renderShell(name, admin, h("div", { class: "panel err" }, h("h2", { text: "没有权限" }), h("p", { text: "你的角色不能访问这个页面。" })));
    return;
  }
  const my = ++renderToken;
  const main = h("main", { class: "main" }, h("p", { class: "boot", text: "载入中…" }));
  renderShell(name, admin, main);
  try {
    const mod = await page.load();
    if (my !== renderToken) return; // the operator already moved on
    const body = h("div", {});
    await mod.render(body, { admin, params, navigate, can: (n) => can(admin.role, n) });
    if (my !== renderToken) return;
    main.replaceChildren(h("h2", { class: "page-title", text: page.title }), body);
  } catch (e) {
    if (my !== renderToken) return;
    main.replaceChildren(h("div", { class: "panel err" }, h("h2", { text: "出错了" }), h("p", { text: e instanceof ApiError ? e.message : "页面加载失败：" + e.message })));
  }
}

function renderShell(active, admin, main) {
  const nav = Object.entries(PAGES)
    .filter(([k, p]) => k !== "account" && can(admin.role, p.need))
    .map(([k, p]) => h("a", { href: `#/${k}`, class: k === active ? "active" : "", text: p.title }));
  clear(app);
  app.append(
    h("div", { class: "shell" },
      h("header", { class: "top" },
        h("strong", { class: "brand", text: "2DRound 管理后台" }),
        h("span", { class: "spacer" }),
        h("a", { href: "#/account", class: "who", text: `${admin.username}（${roleLabel(admin.role)}）` }),
        h("button", { class: "btn", type: "button", text: "退出", onclick: logout })),
      h("nav", { class: "side" }, nav),
      main));
}

function logout() {
  session.clear();
  sessionStorage.removeItem("login.notice");
  navigate("login");
  route();
}

window.addEventListener("session-expired", () => {
  sessionStorage.setItem("login.notice", "登录已失效，请重新登录。");
  route();
});
window.addEventListener("hashchange", route);

// Verify a stored session before showing anything: the admin may have been disabled.
(async () => {
  if (session.token) {
    try { session.update(await get("/api/me")); } catch { /* a 401 already cleared the session */ }
  }
  route();
})();
