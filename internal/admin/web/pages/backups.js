import { get, post } from "/api.js";
import { h, table, toast, confirmBox } from "/dom.js";
import { fmtBytes, fmtTime } from "/lib.js";

// The database backups the server keeps (see README: Backups). Making one is safe while the
// server runs; restoring is done by hand with the services stopped, on purpose.
export async function render(root) {
  const box = h("div", {});
  const make = h("button", { class: "btn primary", type: "button", text: "立即备份" });

  async function load() {
    let r;
    try { r = await get("/api/backups"); } catch (e) {
      box.replaceChildren(h("p", { class: "hint", text: e.status === 409 ? "没有配置备份目录（admin.backup_dir）。" : e.message }));
      make.disabled = true;
      return;
    }
    box.replaceChildren(
      h("p", { class: "hint", text: `共 ${r.items.length} 份，最多保留 ${r.keep} 份，旧的会被自动删除。备份文件含有全部账号数据，请妥善保管。` }),
      table(["文件", "大小", "时间"], r.items.map((b) => h("tr", {}, h("td", {}, h("code", { text: b.name })),
        h("td", { text: fmtBytes(b.size) }), h("td", { text: fmtTime(b.mod_time) })))));
  }

  make.addEventListener("click", async () => {
    if (!(await confirmBox("现在做一份数据库备份？（服务不会中断）", { confirm: "备份" }))) return;
    make.disabled = true;
    try {
      const res = await post("/api/backups");
      toast(`已备份：${res.name}（${fmtBytes(res.size)}）` + (res.pruned ? `，删除了 ${res.pruned} 份旧备份` : ""));
      await load();
    } catch (e) { toast(e.message, "err"); } finally { make.disabled = false; }
  });

  root.append(h("div", { class: "toolbar" }, make), box,
    h("p", { class: "hint", text: "恢复：停止所有服务，用备份文件覆盖数据库文件（并删除同名的 -wal / -shm 文件），再启动。需要时按 README 的步骤手动操作。" }));
  await load();
}
