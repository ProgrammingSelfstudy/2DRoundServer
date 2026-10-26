#!/usr/bin/env python3
"""Extract the fireworks party ("花火大会", holiday_petard_data.lua): the constants, the heat awards and the red bag wishes.

usage: extract_petard.py LUA_DIR OUT_JSON
  LUA_DIR  .../outputs/readable-source/lua/ePoCrKQkWA3vpiOwu2oNfOh02MOeNheW
"""
import json, sys
sys.path.insert(0, __file__.rsplit("/", 1)[0])
from luadump import dump

lua_dir, out_json = sys.argv[1:3]
S = "Config.HolidayPetardData."
m = dump(lua_dir, "holiday_petard_data.lua", [S + n for n in ("data_const", "data_award", "data_wish")])
m = {k[len(S):]: v for k, v in m.items()}


def vals(x):
    return list(x.values()) if isinstance(x, dict) else x


const = {k: v["val"] for k, v in m["data_const"].items() if isinstance(v["val"], int) and k != "holiday_time"}
back = {"meteor": m["data_const"]["meteor_item_reward"]["val"], "firework": m["data_const"]["firework_item_reward"]["val"]}
awards = [{"id": r["id"], "count": r["count"], "award": r["award"]} for r in sorted(vals(m["data_award"]), key=lambda r: r["id"])]
wishes = [{"id": r["id"], "pro": r["pro"]} for r in sorted(vals(m["data_wish"]), key=lambda r: r["id"])]
json.dump({"source": "restored client holiday_petard_data.lua", "const": const, "back": back, "awards": awards, "wishes": wishes}, open(out_json, "w"), ensure_ascii=False, separators=(",", ":"))
print("petard:", len(const), len(awards), len(wishes))
