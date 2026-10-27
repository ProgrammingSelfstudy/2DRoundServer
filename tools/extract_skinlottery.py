#!/usr/bin/env python3
"""Extract the world recruit draw (世界征兵, holiday_skin_draw_data.lua): per lottery id the token that pays for
the draws, what the n-th draw costs and the ten prizes by position.

usage: extract_skinlottery.py LUA_DIR OUT_JSON
  LUA_DIR  .../outputs/readable-source/lua/ePoCrKQkWA3vpiOwu2oNfOh02MOeNheW
"""
import json, sys
sys.path.insert(0, __file__.rsplit("/", 1)[0])
from luadump import dump

lua_dir, out_json = sys.argv[1:3]
S = "Config.HolidaySkinDrawData."
m = dump(lua_dir, "holiday_skin_draw_data.lua", [S + n for n in ("data_lottery_msg", "data_consum_count", "data_lottery_stock")])
m = {k[len(S):]: v for k, v in m.items()}
camps = {}
for cid, msg in m["data_lottery_msg"].items():
    cost = m["data_consum_count"][cid]
    stock = sorted(m["data_lottery_stock"][cid], key=lambda r: r["sort"])
    assert len({c["loss_id"] for c in cost}) == 1 and [s["sort"] for s in stock] == list(range(1, len(stock) + 1))
    camps[cid] = {"token": msg["lottery_item"], "costs": [c["num"] for c in cost],
                  "prizes": [{"id": s["reward_id"], "num": s["num"]} for s in stock]}
assert camps
json.dump({"source": "restored client holiday_skin_draw_data.lua", "camps": camps}, open(out_json, "w"), ensure_ascii=False, separators=(",", ":"))
print("skin lottery:", {k: (len(v["costs"]), len(v["prizes"])) for k, v in camps.items()})
