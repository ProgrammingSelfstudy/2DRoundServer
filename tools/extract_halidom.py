#!/usr/bin/env python3
"""Extract the halidoms ("元素信念", 22200~22205) from the restored client: the six camps' halidoms with their unlock cost, the 100 level
costs, exp and attributes, and the 10 steps (conditions, heroes and items to pay, extra percent attributes).

usage: extract_halidom.py LUA_DIR OUT_JSON
  LUA_DIR  .../outputs/readable-source/lua/ePoCrKQkWA3vpiOwu2oNfOh02MOeNheW
"""
import json, sys
sys.path.insert(0, __file__.rsplit("/", 1)[0])
from luadump import dump

lua_dir, out_json = sys.argv[1:3]
H = "Config.HalidomData."
h = dump(lua_dir, "halidom_data.lua", [H + n for n in ("data_const", "data_base", "data_max_lev", "data_max_step", "data_lvup", "data_step")])
a = dump(lua_dir, "attr_data.lua", ["Config.AttrData.data_key_to_id"])
h = {k[len(H):]: v for k, v in h.items()}

def rows(t):  # a list (keys 1..n) or an object (a table with the key 0)
    return t if isinstance(t, list) else [t[k] for k in sorted(t, key=int)]

out = {
    "source": "restored client halidom_data.lua",
    "open_lev": h["data_const"]["halidom_open_lev"]["val"],
    "attr_ids": a["Config.AttrData.data_key_to_id"],
    "base": [{"id": b["id"], "camp": b["camp"], "name": b["name"], "loss": b["loss"]} for b in h["data_base"]],
    "max_lev": h["data_max_lev"],
    "max_step": h["data_max_step"],
    "lvup": [[{"lev": r["lev"], "exp": r["exp"], "total_exp": r["total_exp"], "loss": r["loss"], "exp_attr": r["exp_attr"], "attr": r["attr"]} for r in rows(t)] for t in h["data_lvup"]],
    "step": [[{"step": r["step"], "conds": r["conds"], "loss_fixed": r["loss_fixed"], "loss_rand": r["loss_rand"], "loss_items": r["loss_items"], "dynamic_attr": r["dynamic_attr"], "fixed_attr": r["fixed_attr"]} for r in rows(t)] for t in h["data_step"]],
}
json.dump(out, open(out_json, "w", encoding="utf-8"), ensure_ascii=False, separators=(",", ":"))
print(len(out["base"]), "halidoms;", [len(x) for x in out["lvup"]], "level rows;", [len(x) for x in out["step"]], "step rows")
