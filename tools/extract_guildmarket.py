#!/usr/bin/env python3
"""Extract the guild treasury ("公会宝库", 26900~26906) from the restored client: the constants and the 56 goods (price, what a put pays,
goods per piece, tab, whether players may put it, storage cap, days kept, daily buy limit).

usage: extract_guildmarket.py LUA_DIR OUT_JSON
  LUA_DIR  .../outputs/readable-source/lua/ePoCrKQkWA3vpiOwu2oNfOh02MOeNheW
"""
import json, sys
sys.path.insert(0, __file__.rsplit("/", 1)[0])
from luadump import dump

lua_dir, out_json = sys.argv[1:3]
G = "Config.GuildMarketplaceData."
g = dump(lua_dir, "guild_marketplace_data.lua", [G + n for n in ("data_const", "data_item_info_table")])
g = {k[len(G):]: v for k, v in g.items()}
names = ("id", "name", "price", "sell", "radio", "type", "is_sell", "is_pile", "max_pile", "capacity", "end_time", "is_board", "day_buy")
items = {k: dict(zip(names, v)) for k, v in g["data_item_info_table"].items()}
out = {
    "source": "restored client guild_marketplace_data.lua",
    "const": {k: v["val"] for k, v in g["data_const"].items()},
    "items": items,
}
json.dump(out, open(out_json, "w", encoding="utf-8"), ensure_ascii=False, separators=(",", ":"))
print(len(items), "goods;", out["const"])
