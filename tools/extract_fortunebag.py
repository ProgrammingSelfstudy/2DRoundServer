#!/usr/bin/env python3
"""Extract the fortune bag draw (holiday_optional_lottery_data.lua): per camp the 20 rounds of 34 normal prizes and the
optional ultimate prizes, and the constants (the whistle price, the daily diamond draws).

usage: extract_fortunebag.py LUA_DIR OUT_JSON
  LUA_DIR  .../outputs/readable-source/lua/ePoCrKQkWA3vpiOwu2oNfOh02MOeNheW
"""
import json, sys
sys.path.insert(0, __file__.rsplit("/", 1)[0])
from luadump import dump

lua_dir, out_json = sys.argv[1:3]
L = "Config.HolidayOptionalLotteryData."
d = dump(lua_dir, "holiday_optional_lottery_data.lua", [L + n for n in ("data_constant", "data_normal_award", "data_optional_award")])
d = {k[len(L):]: v for k, v in d.items()}
c = d["data_constant"]


def rows(x):
    return list(x.values()) if isinstance(x, dict) else x


camps = {}
for camp, rounds in d["data_normal_award"].items():
    rounds = rows(rounds)
    assert len(rounds) == c["max_round"]["val"]
    camps[camp] = {
        "rounds": [[{"id": r["id"], "type": r["type_id"], "rewards": r["rewards"]} for r in sorted(rows(rd), key=lambda r: r["id"])] for rd in rounds],
        "ultimate": {str(o["type_id"]): o["rewards"] for o in rows(d["data_optional_award"][camp])},
    }
out = {"source": "restored client holiday_optional_lottery_data.lua",
       "const": {"cost": c["draw_expend"]["val"][0], "day_discount": c["day_draw_time"]["val"], "day_original": c["day_original_draw_time"]["val"],
                 "discount_price": c["draw_expend_value"]["val"][0], "original_price": c["draw_original_price"]["val"][0], "max_round": c["max_round"]["val"]},
       "camps": camps}
json.dump(out, open(out_json, "w"), ensure_ascii=False, separators=(",", ":"))
print("fortunebag:", len(camps), "camps", out["const"])
