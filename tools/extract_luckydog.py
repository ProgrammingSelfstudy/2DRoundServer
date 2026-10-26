#!/usr/bin/env python3
"""Extract the lucky dog action (holiday_lucky_dog_data.lua): the 7 periods and their quests, the quests (event, target) and
the four prizes of each period with the ending digits that win them.

usage: extract_luckydog.py LUA_DIR OUT_JSON
  LUA_DIR  .../outputs/readable-source/lua/ePoCrKQkWA3vpiOwu2oNfOh02MOeNheW
"""
import json, sys
sys.path.insert(0, __file__.rsplit("/", 1)[0])
from luadump import dump

lua_dir, out_json = sys.argv[1:3]
L = "Config.HolidayLuckyDogData."
d = dump(lua_dir, "holiday_lucky_dog_data.lua", [L + n for n in ("data_period", "data_quest", "data_quest_award")])
d = {k[len(L):]: v for k, v in d.items()}


def rows(x):
    return list(x.values()) if isinstance(x, dict) else x


periods = [{"period": p["period"], "open": p["open_time"], "quests": p["quest_ids"]} for p in sorted(rows(d["data_period"]), key=lambda p: p["period"])]
quests = {str(q["quest_id"]): {"desc": q["desc"], "event": q["condition"][0], "target": q["condition"][2]} for q in rows(d["data_quest"])}
awards = [[{"digits": a["limit_number"], "award": a["award"]} for a in p] for p in rows(d["data_quest_award"])]
assert len(awards) == len(periods) and all(len(a) == 4 for a in awards)
json.dump({"source": "restored client holiday_lucky_dog_data.lua", "periods": periods, "quests": quests, "awards": awards},
          open(out_json, "w"), ensure_ascii=False, separators=(",", ":"))
print("luckydog:", len(periods), "periods,", len(quests), "quests")
