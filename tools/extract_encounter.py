#!/usr/bin/env python3
"""Extract the hero stories (英雄物语, encounter): encounter_data.lua, the constants and the story list.

usage: extract_encounter.py LUA_DIR OUT_JSON
  LUA_DIR  .../outputs/readable-source/lua/ePoCrKQkWA3vpiOwu2oNfOh02MOeNheW
"""
import json, sys
sys.path.insert(0, __file__.rsplit("/", 1)[0])
from luadump import dump

lua_dir, out_json = sys.argv[1:3]
E = "Config.EncounterData."
m = dump(lua_dir, "encounter_data.lua", [E + "data_encounter_const", E + "data_encounter_info"])
m = {k[len(E):]: v for k, v in m.items()}
info = m["data_encounter_info"]
info = info if isinstance(info, list) else [info[k] for k in sorted(info, key=int)]
const = m["data_encounter_const"]
out = {
    "source": "restored client encounter_data.lua",
    "level": const["encounter_lev"]["val"],
    "windows": const["encounter_update_time"]["val"],
    "stories": [{"id": r["id"], "bid": r["partner_bid"], "star": r["star"], "weight": r["weight"],
                 "lev": r["first_lev"], "power": r["first_power"], "pages": len(r["dialogue"]),
                 "choices": len(r["answer"]) if r["answer"] != [0] else 0,
                 "reward": {str(c): ids for c, ids in r["reward"]}} for r in info],
}
json.dump(out, open(out_json, "w", encoding="utf-8"), ensure_ascii=False, separators=(",", ":"))
print(len(out["stories"]), out["level"], out["windows"])
