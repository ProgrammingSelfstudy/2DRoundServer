#!/usr/bin/env python3
"""Extract the mysterious store (holiday_exchange_data.lua): the item it takes and the 26 exchanges with their limits.

usage: extract_stoneshop.py LUA_DIR OUT_JSON
  LUA_DIR  .../outputs/readable-source/lua/ePoCrKQkWA3vpiOwu2oNfOh02MOeNheW
"""
import json, sys
sys.path.insert(0, __file__.rsplit("/", 1)[0])
from luadump import dump

lua_dir, out_json = sys.argv[1:3]
K = "Config.HolidayExchangeData."
d = dump(lua_dir, "holiday_exchange_data.lua", [K + "data_constant", K + "data_get_config_const"])
rows = d[K + "data_get_config_const"]
rows = list(rows.values()) if isinstance(rows, dict) else rows
out = {"source": "restored client holiday_exchange_data.lua", "item": d[K + "data_constant"]["consume_id"]["val"],
       "goods": [{"id": r["id"], "sub_type": r["sub_type"], "day": r["r_limit_day"], "all": r["r_limit_all"], "expend": r["expend"], "award": r["award"]}
                 for r in sorted(rows, key=lambda r: r["id"])]}
json.dump(out, open(out_json, "w", encoding="utf-8"), ensure_ascii=False, separators=(",", ":"))
print("stoneshop:", len(out["goods"]), "goods, item", out["item"])
