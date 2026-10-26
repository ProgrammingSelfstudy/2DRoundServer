#!/usr/bin/env python3
"""Extract the dial ("转盘", holiday_dial_data.lua): the constants, the 12 slots and the score awards.

usage: extract_dial.py LUA_DIR OUT_JSON
  LUA_DIR  .../outputs/readable-source/lua/ePoCrKQkWA3vpiOwu2oNfOh02MOeNheW
"""
import json, sys
sys.path.insert(0, __file__.rsplit("/", 1)[0])
from luadump import dump

lua_dir, out_json = sys.argv[1:3]
S = "Config.HolidayDialData."
m = dump(lua_dir, "holiday_dial_data.lua", [S + n for n in ("data_dial_const", "data_dial_item", "data_score_award")])
m = {k[len(S):]: v for k, v in m.items()}


def vals(x):
    return list(x.values()) if isinstance(x, dict) else x


const = {k: v["val"] for k, v in m["data_dial_const"].items() if k != "holiday_rule"}
slots = sorted((r for rows in vals(m["data_dial_item"]) for r in vals(rows)), key=lambda r: r["order"])
slots = [{"order": r["order"], "id": r["id"], "num": r["num"], "type": r["other_type"]} for r in slots]
awards = sorted((r for rows in vals(m["data_score_award"]) for r in vals(rows)), key=lambda r: r["id"])
awards = [{"id": r["id"], "score": r["num"], "award": r["award_list"]} for r in awards]
assert len(slots) == 12, len(slots)
json.dump({"source": "restored client holiday_dial_data.lua", "const": const, "slots": slots, "awards": awards}, open(out_json, "w"), ensure_ascii=False, separators=(",", ":"))
print("dial:", const, len(slots), len(awards))
