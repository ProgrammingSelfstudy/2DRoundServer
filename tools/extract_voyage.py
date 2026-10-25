#!/usr/bin/env python3
"""Extract the voyage (远航) orders: shipping_data.lua, the constants, orders, hero conditions, quick finish prices and refresh limits.

usage: extract_voyage.py LUA_DIR OUT_JSON
  LUA_DIR  .../outputs/readable-source/lua/ePoCrKQkWA3vpiOwu2oNfOh02MOeNheW
"""
import json, sys
sys.path.insert(0, __file__.rsplit("/", 1)[0])
from luadump import dump

lua_dir, out_json = sys.argv[1:3]
S = "Config.ShippingData."
names = ("data_const", "data_order", "data_condition", "data_quick_cost", "data_refresh")
m = dump(lua_dir, "shipping_data.lua", [S + n for n in names])
m = {k[len(S):]: v for k, v in m.items()}


def rows(t):
    if isinstance(t, list):
        return t
    return [t[k] for k in sorted(t, key=int)]


out = {
    "source": "restored client shipping_data.lua",
    "const": {k: v["val"] for k, v in m["data_const"].items()},
    "orders": [{"id": r["id"], "name": r["name"], "limit": r["limit_num"], "time": r["need_time"], "quality": r["quality"],
                "award": r["award"], "cond": r["condition"], "expend": r["expend"]} for r in rows(m["data_order"])],
    "conds": {str(r["id"]): r["conition"][0] for r in rows(m["data_condition"])},
    "quick": [{"min": r["min"], "max": r["max"], "gold": r["gold"]} for r in rows(m["data_quick_cost"])],
    "refresh": [{"vip": r["vip_lev"], "all": r["all_times"], "free": r["free_times"], "expend": r["expend"]} for r in rows(m["data_refresh"])],
}
json.dump(out, open(out_json, "w", encoding="utf-8"), ensure_ascii=False, separators=(",", ":"))
print({k: len(v) for k, v in out.items() if k != "source"})
