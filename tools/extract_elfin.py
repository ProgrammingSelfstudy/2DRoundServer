#!/usr/bin/env python3
"""Extract the sprite ("elfin", the client's 幽灵) system from the restored client: the hatching grounds, the eggs and
speed-up items, the 141 sprites with their altar / help attributes, the compose recipes, the ancient tree (levels, steps,
slots) and the permanent sprite summon (holiday_sprite_lottery_data.lua, camp 0, group 110).

usage: extract_elfin.py LUA_DIR OUT_JSON
  LUA_DIR  .../outputs/readable-source/lua/ePoCrKQkWA3vpiOwu2oNfOh02MOeNheW
"""
import json, sys
sys.path.insert(0, __file__.rsplit("/", 1)[0])
from luadump import dump

lua_dir, out_json = sys.argv[1:3]
S = "Config.SpriteData."
H = "Config.HolidaySpriteLotteryData."
s = dump(lua_dir, "sprite_data.lua", [S + n for n in (
    "data_const", "data_hatch_data", "data_hatch_lev", "data_hatch_egg", "data_smash_item", "data_elfin_data_table", "data_elfin_book",
    "data_tree_attr", "data_tree_up_lv_table", "data_tree_step", "data_tree_limit", "data_elfin_com")])
h = dump(lua_dir, "holiday_sprite_lottery_data.lua", [H + n for n in ("data_const", "data_action", "data_summon", "data_award", "data_probability")])
s = {k[len(S):]: v for k, v in s.items()}
h = {k[len(H):]: v for k, v in h.items()}

names = ["id", "skill", "step", "effect_id", "sprite_type", "icon", "scale_val", "offset_y", "power", "altar_attribute", "help_attribute", "evolution_lev"]
sprites = {}
for k, v in s["data_elfin_data_table"].items():
    r = dict(zip(names, v))
    sprites[k] = {"skill": r["skill"], "step": r["step"], "type": r["sprite_type"], "power": r["power"],
                  "altar": {a: n for a, n in r["altar_attribute"]}, "help": {a: n for a, n in r["help_attribute"]}}
const = {k: v["val"] for k, v in s["data_const"].items() if not isinstance(v["val"], str)}
tree_up = [{"lev": r[0], "expend": r[1]} for r in s["data_tree_up_lv_table"].values()] if isinstance(s["data_tree_up_lv_table"], dict) else [{"lev": r[0], "expend": r[1]} for r in s["data_tree_up_lv_table"]]
out = {
    "source": "restored client sprite_data.lua, holiday_sprite_lottery_data.lua",
    "const": const,
    "hatch": s["data_hatch_data"], "hatch_lev": s["data_hatch_lev"], "egg": s["data_hatch_egg"], "smash": s["data_smash_item"],
    "sprites": sprites, "book": s["data_elfin_book"], "tree_attr": s["data_tree_attr"][0], "tree_up": tree_up,
    "tree_step": s["data_tree_step"], "tree_limit": s["data_tree_limit"], "compose": s["data_elfin_com"],
    "lottery": {"const": {k: v["val"] for k, v in h["data_const"].items() if not isinstance(v["val"], str)},
                "summon": h["data_summon"]["110"], "awards": h["data_award"]["0"], "odds": h["data_probability"]["110"]},
}
json.dump(out, open(out_json, "w", encoding="utf-8"), ensure_ascii=False, separators=(",", ":"))
print(len(sprites), "sprites;", len(tree_up), "tree levels;", len(out["tree_step"]), "steps;", len(out["compose"]), "recipes")
