#!/usr/bin/env python3
"""Extract the home pet (家园宠物旅行): home_pet_data.lua, the constants, the travel events and the interactions.

usage: extract_homepet.py LUA_DIR OUT_JSON
  LUA_DIR  .../outputs/readable-source/lua/ePoCrKQkWA3vpiOwu2oNfOh02MOeNheW
"""
import json, sys
sys.path.insert(0, __file__.rsplit("/", 1)[0])
from luadump import dump

lua_dir, out_json = sys.argv[1:3]
S = "Config.HomePetData."
m = dump(lua_dir, "home_pet_data.lua", [S + n for n in ("data_const", "data_event_info", "data_interaction_info")])
m = {k[len(S):]: v for k, v in m.items()}


def rows(t):
    if isinstance(t, list):
        return t
    return [t[k] for k in sorted(t, key=int)]


c = m["data_const"]
out = {
    "source": "restored client home_pet_data.lua",
    "const": {k: v["val"] for k, v in c.items() if not k.endswith("tips") and k not in ("pet_story",)},
    "events": [{"sid": r["s_id"], "id": r["id"], "type": r["type"], "notice": r["is_notice"], "award": r["award"], "drop": r["drop_ids"]} for r in rows(m["data_event_info"])],
    "talks": [{"id": r["id"], "type": r["type"], "pro": r["pro"], "vigor": r["award"], "expend": r["expend"], "award2": r["award2"]} for r in rows(m["data_interaction_info"])],
}
json.dump(out, open(out_json, "w", encoding="utf-8"), ensure_ascii=False, separators=(",", ":"))
print({k: len(v) for k, v in out.items() if k != "source"})
