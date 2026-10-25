#!/usr/bin/env python3
"""Extract the devil fruit collection ("恶魔果实图鉴") from the restored client (emoguoshitujian_data.lua): the 31 fruits with
their level, star and awakening tables, the shop, the lottery boxes and the fruit gift (battle pass).

usage: extract_emoguoshi.py LUA_DIR OUT_JSON
  LUA_DIR  .../outputs/readable-source/lua/ePoCrKQkWA3vpiOwu2oNfOh02MOeNheW
"""
import json, sys
sys.path.insert(0, __file__.rsplit("/", 1)[0])
from luadump import dump

lua_dir, out_json = sys.argv[1:3]
S = "Config.EmoguoshitujianData."
names = ["data_const", "data_get_emoguoshitujian_level", "data_get_emoguoshitujian_star", "data_get_emoguoshitujian_juexing", "data_get_random_data",
         "data_get_random_do_data", "data_get_random_group_data", "data_get_shop_data", "data_get_emoguoshitujian_data", "data_get_zhanling_data"]
m = dump(lua_dir, "emoguoshitujian_data.lua", [S + n for n in names])
m = {k[len(S):]: v for k, v in m.items()}
out = {
    "source": "restored client emoguoshitujian_data.lua",
    "const": {k: v["val"] for k, v in m["data_const"].items() if not isinstance(v["val"], str) or len(v["val"]) < 40},
    "fruits": {k: v["name"] for k, v in m["data_get_emoguoshitujian_data"].items()},
    "level": {k: {"need_items": v["need_items"], "need_star": v["need_star"]} for k, v in m["data_get_emoguoshitujian_level"].items()},
    "star": {k: {"need_items": v["need_items"], "need_partner": v["need_partner"], "max_level": v["max_level"], "effect": v["effect"],
                 "effect_cond": v["effect_cond"], "effect_param": v["effect_param"]} for k, v in m["data_get_emoguoshitujian_star"].items()},
    "juexing": m["data_get_emoguoshitujian_juexing"],
    "boxes": m["data_get_random_data"], "do": m["data_get_random_do_data"], "groups": m["data_get_random_group_data"],
    "shop": m["data_get_shop_data"], "gift": m["data_get_zhanling_data"],
}
json.dump(out, open(out_json, "w", encoding="utf-8"), ensure_ascii=False, separators=(",", ":"))
print(len(out["fruits"]), "fruits;", len(out["level"]), "level rows;", len(out["star"]), "star rows;", len(out["shop"]), "goods;", len(out["gift"]), "gift layers")
