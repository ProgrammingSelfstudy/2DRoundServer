#!/usr/bin/env python3
"""Extract the hero conversion activity (holiday_convert_data.lua): the conversion rows by star and camp,
the item of the cost, and the hero groups a source hero or a target hero must be in.

usage: extract_holidayconvert.py LUA_DIR OUT_JSON
  LUA_DIR  .../outputs/readable-source/lua/ePoCrKQkWA3vpiOwu2oNfOh02MOeNheW
"""
import json, sys
sys.path.insert(0, __file__.rsplit("/", 1)[0])
from luadump import dump

lua_dir, out_json = sys.argv[1:3]
S = "Config.HolidayConvertData."
m = dump(lua_dir, "holiday_convert_data.lua", [S + n for n in ("data_const", "data_convert_info", "data_hero_list", "data_target_hero_list")])
m = {k[len(S):]: v for k, v in m.items()}


def vals(x):
    return list(x.values()) if isinstance(x, dict) else x


rows = []
for star, byCamp in m["data_convert_info"].items():
    for r in vals(byCamp):
        rows.append({"star": r["star"], "camp": r["camp_type"], "expend": r["expend"], "src_group": r["src_group"],
                     "target_group": r["target_group"], "loss": r["loss"]})
rows.sort(key=lambda r: (r["star"], r["camp"]))
groups = lambda t: {str(g): sorted(int(b) for b in bids) for g, bids in t.items()}
out = {"source": "restored client holiday_convert_data.lua", "item_id": m["data_const"]["item_id"]["val"], "rows": rows,
       "src": groups(m["data_hero_list"]), "target": groups(m["data_target_hero_list"])}
assert len(m["data_convert_info"]) == 19 and rows, (len(m["data_convert_info"]), len(rows))
json.dump(out, open(out_json, "w"), ensure_ascii=False, separators=(",", ":"))
print("convert rows:", len(rows), "src groups", len(out["src"]), "target groups", len(out["target"]))
