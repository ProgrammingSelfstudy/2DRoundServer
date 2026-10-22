import { get } from "/api.js";
import { h, select } from "/dom.js";
import { itemIdOf, queryString } from "/lib.js";

// attachRows is the list of currency / item rows a mail and a direct grant both use.
// `meta` is GET /api/meta (its asset list fills the currency box). read() returns the rows
// as the form helpers in lib.js take them.
export function attachRows(meta) {
  let seq = 0;
  const box = h("div", { class: "attach-rows" });
  const add = (kind = "asset") => {
    const kindSel = select([["asset", "货币"], ["item", "物品"]], kind);
    const asset = select(meta.assets.map((a) => [a.name, `${a.name}（${a.label}）`]), "gold");
    // an item is picked by name (or id) from the client's item table: type, then choose from the list
    const listID = "items-" + (++seq) + "-" + Math.random().toString(36).slice(2, 7);
    const itemID = h("input", { type: "search", list: listID, placeholder: "物品名称或 ID，如 生命金卡", autocomplete: "off" });
    const itemList = h("datalist", { id: listID });
    let timer = 0;
    itemID.addEventListener("input", () => {
      clearTimeout(timer);
      const q = itemID.value.trim();
      if (!q || /^\d+\s/.test(q)) return; // empty, or already a chosen "id name"
      timer = setTimeout(async () => {
        try {
          const r = await get("/api/items" + queryString({ q, limit: 20 }));
          itemList.replaceChildren(...r.items.map((it) => h("option", { value: `${it.base_id} ${it.name}`, label: it.currency ? "货币，请用货币附件" : (it.in_backpack ? "" : "不在背包显示") })));
        } catch { /* the list is only a convenience */ }
      }, 200);
    });
    const num = h("input", { type: "number", min: 1, placeholder: "数量" });
    const row = h("div", { class: "attach-row" }, kindSel, asset, itemID, itemList, num, h("button", { class: "btn", type: "button", text: "移除", onclick: () => row.remove() }));
    const sync = () => { asset.hidden = kindSel.value !== "asset"; itemID.hidden = kindSel.value !== "item"; };
    kindSel.addEventListener("change", sync);
    sync();
    row.read = () => ({ kind: kindSel.value, asset: asset.value, itemID: itemIdOf(itemID.value), num: num.value });
    box.append(row);
  };
  return { box, add, read: () => [...box.children].map((x) => x.read()), clear: () => box.replaceChildren() };
}
