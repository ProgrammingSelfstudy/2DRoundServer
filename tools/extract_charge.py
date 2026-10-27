#!/usr/bin/env python3
"""Extract the top-up products (charge_data.lua data_charge_data): price in yuan, diamonds, the extra for the first and the later
purchases, VIP experience, type (0 diamonds, 1 weekly gifts, 2 gifts and funds that the activities deliver).

usage: extract_charge.py LUA_DIR OUT_JSON
  LUA_DIR  .../outputs/readable-source/lua/ePoCrKQkWA3vpiOwu2oNfOh02MOeNheW
"""
import json, sys
sys.path.insert(0, __file__.rsplit("/", 1)[0])
from luadump import dump

lua_dir, out_json = sys.argv[1:3]
K = "Config.ChargeData."
d = dump(lua_dir, "charge_data.lua", [K + "data_charge_data"])[K + "data_charge_data"]
rows = list(d.values()) if isinstance(d, dict) else d
out = {"source": "restored client charge_data.lua",
       "products": [{"id": r["id"], "name": r["name"], "val": r["val"], "gold": r["gold"], "add": r["add"], "first_add": r["first_add"], "items": r["items"],
                     "vip_exp": r["vip_exp"], "type": r["type"], "sort": r["sort"], "pic": r["pic"]} for r in sorted(rows, key=lambda r: r["id"])]}
json.dump(out, open(out_json, "w", encoding="utf-8"), ensure_ascii=False, separators=(",", ":"))
print("charge:", len(out["products"]), "products")
