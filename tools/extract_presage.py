#!/usr/bin/env python3
"""Extract the presage summon ("预知召唤", the pine cone treasure, holiday_predict_data.lua): constants, the prices of
buying the shown treasure and the prize positions of the two kinds (lucky pine 101-137, luxury pine 138-208).

usage: extract_presage.py LUA_DIR OUT_JSON
  LUA_DIR  .../outputs/readable-source/lua/ePoCrKQkWA3vpiOwu2oNfOh02MOeNheW
"""
import json, sys
sys.path.insert(0, __file__.rsplit("/", 1)[0])
from luadump import dump

lua_dir, out_json = sys.argv[1:3]
S = "Config.HolidayPredictData."
m = dump(lua_dir, "holiday_predict_data.lua", [S + n for n in ("data_constant", "data_prize_pool", "data_reward_pos", "data_magnificat_list")])
m = {k[len(S):]: v for k, v in m.items()}

const = {k: v["val"] for k, v in m["data_constant"].items() if not k.startswith("game_rule")}
prize = {str(p["type"]): {"item": p["expend_item"][0], "gold": p["expend_gold"][0]} for p in (m["data_prize_pool"].values() if isinstance(m["data_prize_pool"], dict) else m["data_prize_pool"])}
pools = {"1": [], "2": []}
for k in sorted(m["data_reward_pos"], key=int):
    r = m["data_reward_pos"][k]
    pools["1" if int(k) < 138 else "2"].append({"id": int(k), "goal": r["is_goal"], "item": r["item_id"], "num": r["item_num"]})
assert len(pools["1"]) == 37 and len(pools["2"]) == 70, (len(pools["1"]), len(pools["2"]))
json.dump({"source": "restored client holiday_predict_data.lua", "const": const, "prize": prize, "pools": pools}, open(out_json, "w"), ensure_ascii=False, separators=(",", ":"))
print("presage:", {k: len(v) for k, v in pools.items()}, "goals", {k: sum(p["goal"] for p in v) for k, v in pools.items()})
