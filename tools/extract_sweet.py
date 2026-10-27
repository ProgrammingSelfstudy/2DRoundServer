#!/usr/bin/env python3
"""Extract the sweet activity (甜蜜大作战 / the flag donation, holiday_valentines_data.lua): the constants, the
accumulated score rewards (count is per mille of the maximum) and the rank rewards.

usage: extract_sweet.py LUA_DIR OUT_JSON
  LUA_DIR  .../outputs/readable-source/lua/ePoCrKQkWA3vpiOwu2oNfOh02MOeNheW
"""
import json, sys
sys.path.insert(0, __file__.rsplit("/", 1)[0])
from luadump import dump

lua_dir, out_json = sys.argv[1:3]
S = "Config.HolidayValentinesData."
m = dump(lua_dir, "holiday_valentines_data.lua", [S + n for n in ("data_const", "data_award", "data_rank_award")])
m = {k[len(S):]: v for k, v in m.items()}
const = {k: v["val"] for k, v in m["data_const"].items() if k != "holiday_rule"}
awards = sorted(({"id": r["id"], "count": r["count"], "award": r["award"]} for r in m["data_award"]), key=lambda r: r["id"])
ranks = sorted(({"min": r["rank1"], "max": r["rank2"], "award": r["award"]} for r in m["data_rank_award"]), key=lambda r: r["min"])
assert len(awards) == 7 and len(ranks) == 6 and "base_max_score" in const
json.dump({"source": "restored client holiday_valentines_data.lua", "const": const, "awards": awards, "ranks": ranks}, open(out_json, "w"), ensure_ascii=False, separators=(",", ":"))
print("sweet:", len(const), len(awards), len(ranks))
