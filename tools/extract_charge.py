#!/usr/bin/env python3
"""Extract the top-up products (charge_data.lua data_charge_data): price in yuan, diamonds, the extra for the first and the later
purchases, VIP experience, the first top-up gifts and the rewards of the accumulated top-up (by the VIP experience paid in), type (0 diamonds, 1 weekly gifts, 2 gifts and funds that the activities deliver).

usage: extract_charge.py LUA_DIR OUT_JSON
  LUA_DIR  .../outputs/readable-source/lua/ePoCrKQkWA3vpiOwu2oNfOh02MOeNheW
"""
import json, sys
sys.path.insert(0, __file__.rsplit("/", 1)[0])
from luadump import dump

lua_dir, out_json = sys.argv[1:3]
K = "Config.ChargeData."
m = dump(lua_dir, "charge_data.lua", [K + "data_charge_data", K + "data_charge_reward_data", K + "data_first_charge_data"])
d = m[K + "data_charge_data"]
rows = list(d.values()) if isinstance(d, dict) else d
out = {"source": "restored client charge_data.lua",
       "products": [{"id": r["id"], "name": r["name"], "val": r["val"], "gold": r["gold"], "add": r["add"], "first_add": r["first_add"], "items": r["items"],
                     "vip_exp": r["vip_exp"], "type": r["type"], "sort": r["sort"], "pic": r["pic"]} for r in sorted(rows, key=lambda r: r["id"])]}
out["acc"] = [{"id": r["id"], "sum": r["charge_sum"], "items": r["item_list"]} for r in sorted(m[K + "data_charge_reward_data"], key=lambda r: r["id"])]
# the first top-up gifts (21000/21001): fid 1 and 2 are the two tiers, each with three days (id 1~3 and 4~6)
fd = m[K + "data_first_charge_data"]
out["first"] = [{"id": r["id"], "fid": r["fid"], "day": r["day"], "need_exp": r["need_exp"], "items": r["item_list"]}
                for r in sorted(fd.values() if isinstance(fd, dict) else fd, key=lambda r: r["id"])]
json.dump(out, open(out_json, "w", encoding="utf-8"), ensure_ascii=False, separators=(",", ":"))
print("charge:", len(out["products"]), "products")
