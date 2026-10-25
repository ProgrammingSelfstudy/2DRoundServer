#!/usr/bin/env python3
"""Extract the endless trial ("无尽试炼") from the restored client (endless_data.lua): constants, the buff groups, the
first-clear rewards, the reward of every round and the daily ranking rewards, for the old mode (type 5) and the four
new ones (water, fire, wind, light-dark).

usage: extract_endless.py LUA_DIR OUT_JSON
  LUA_DIR  .../outputs/readable-source/lua/ePoCrKQkWA3vpiOwu2oNfOh02MOeNheW
"""
import json, sys
sys.path.insert(0, __file__.rsplit("/", 1)[0])
from luadump import dump

lua_dir, out_json = sys.argv[1:3]
S = "Config.EndlessData."
m = dump(lua_dir, "endless_data.lua", [S + n for n in ("data_const", "data_buff_data", "data_first_data", "data_floor_data", "data_rank_reward_data", "data_new_type")])
m = {k[len(S):]: v for k, v in m.items()}
out = {
    "source": "restored client endless_data.lua",
    "const": {k: v["val"] for k, v in m["data_const"].items()},
    "buffs": [{k: {"group_id": b["group_id"], "weight": b["weight"], "round": b["round"], "skill_id": b["skill_id"]} for k, b in g.items()} for g in m["data_buff_data"]],
    "first": m["data_first_data"],
    "floors": m["data_floor_data"],
    "rank_rewards": m["data_rank_reward_data"],
    "new_types": m["data_new_type"],
}
json.dump(out, open(out_json, "w", encoding="utf-8"), ensure_ascii=False, separators=(",", ":"))
print(len(out["first"]), "first-reward lists;", [len(f) for f in out["floors"]], "floor rewards;", len(out["buffs"]), "buff groups")
