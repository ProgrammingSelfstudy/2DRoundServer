#!/usr/bin/env python3
"""Extract the level-up gifts (lev_gift_data.lua: level, how many the server has, the items) and the time collect
activity (holiday_time_award_data.lua: the item, the hours of the day, the camp and the figures that talk).

usage: extract_levgift.py LUA_DIR OUT_JSON
  LUA_DIR  .../outputs/readable-source/lua/ePoCrKQkWA3vpiOwu2oNfOh02MOeNheW
"""
import json, sys
sys.path.insert(0, __file__.rsplit("/", 1)[0])
from luadump import dump

lua_dir, out_json = sys.argv[1:3]
g = dump(lua_dir, "lev_gift_data.lua", ["Config.LevGiftData.data_level_welfare"])["Config.LevGiftData.data_level_welfare"]
K = "Config.HolidayTimeAwardData."
t = dump(lua_dir, "holiday_time_award_data.lua", [K + "data_const", K + "data_dialog"])
const, dialog = t[K + "data_const"], t[K + "data_dialog"]
camps = {camp: [{"id": r["partner_id"], "pro": r["pro"]} for r in rows] for camp, rows in dialog.items()}
out = {"source": "restored client lev_gift_data.lua + holiday_time_award_data.lua",
       "gifts": [{"id": r["id"], "lev": r["lev"], "num": r["num"], "reward": r["reward"]} for r in sorted(g, key=lambda r: r["id"])],
       "collect": {"award": const["award"]["val"], "from": const["time_quantum"]["val"][0][0], "to": const["time_quantum"]["val"][1][0], "camps": camps}}
json.dump(out, open(out_json, "w", encoding="utf-8"), ensure_ascii=False, separators=(",", ":"))
print("levgift:", len(out["gifts"]), "gifts;", len(camps), "collect camps", out["collect"]["award"])
