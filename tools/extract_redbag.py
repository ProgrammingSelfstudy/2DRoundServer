#!/usr/bin/env python3
"""Extract the guild red bags ("公会红包", guild_data.lua data_guild_red_bag): per kind the amount, the number of shares, the
daily limit, the item that sends one and the sender's reward.

usage: extract_redbag.py LUA_DIR OUT_JSON
  LUA_DIR  .../outputs/readable-source/lua/ePoCrKQkWA3vpiOwu2oNfOh02MOeNheW
"""
import json, sys
sys.path.insert(0, __file__.rsplit("/", 1)[0])
from luadump import dump

lua_dir, out_json = sys.argv[1:3]
S = "Config.GuildData."
m = dump(lua_dir, "guild_data.lua", [S + "data_guild_red_bag"])[S + "data_guild_red_bag"]
rows = m.values() if isinstance(m, dict) else m
bags = [{"id": r["id"], "name": r["name"], "assets": r["assets"], "val": r["val"], "num": r["num"], "limit": r["limit"], "price": r["price"],
         "loss_item": r["loss_item"], "reward": r["reward"], "msg": r["msg"]} for r in sorted(rows, key=lambda r: r["id"])]
json.dump({"source": "restored client guild_data.lua", "bags": bags}, open(out_json, "w"), ensure_ascii=False, separators=(",", ":"))
print("redbag:", [(b["id"], b["assets"], b["val"], b["num"]) for b in bags])
