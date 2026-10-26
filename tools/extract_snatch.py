#!/usr/bin/env python3
"""Extract the holiday snatch (节日夺宝, holiday_snatch_data.lua): the constants (the two daily time windows, the ticket and the
score item) and the goods (group f_id, the prize, the price and the slots).

usage: extract_snatch.py LUA_DIR OUT_JSON
  LUA_DIR  .../outputs/readable-source/lua/ePoCrKQkWA3vpiOwu2oNfOh02MOeNheW
"""
import json, sys
sys.path.insert(0, __file__.rsplit("/", 1)[0])
from luadump import dump

lua_dir, out_json = sys.argv[1:3]
S = "Config.HolidaySnatchData."
m = dump(lua_dir, "holiday_snatch_data.lua", [S + "data_const", S + "data_join_goods_list"])
const = {k: v["val"] for k, v in m[S + "data_const"].items() if k != "holiday_rule"}
goods = [{"id": g["id"], "group": g["f_id"], "name": g["name"], "award": g["award"], "expend": g["expend"], "limit": g["limit_max"],
          "limit_role": g["limit_role_max"]} for g in sorted(m[S + "data_join_goods_list"].values(), key=lambda g: g["id"])]
json.dump({"source": "restored client holiday_snatch_data.lua", "const": const, "goods": goods}, open(out_json, "w"), ensure_ascii=False, separators=(",", ":"))
print("snatch:", len(goods), "goods", const)
