#!/usr/bin/env python3
"""Extract the heaven dungeons ("天界") from the restored client: 48 chapters of ten customs (the tenth a boss with two teams),
their three star goals each, the star rewards per chapter, the challenge-count prices (dungeon_heaven_data.lua) and the
holy equipment dial (holy_eqm_lottery_data.lua), whose odds are only given in the group's text.

usage: extract_heaven.py LUA_DIR OUT_JSON
  LUA_DIR  .../outputs/readable-source/lua/ePoCrKQkWA3vpiOwu2oNfOh02MOeNheW
"""
import json, re, sys
sys.path.insert(0, __file__.rsplit("/", 1)[0])
from luadump import dump

lua_dir, out_json = sys.argv[1:3]
D = "Config.DungeonHeavenData."
H = "Config.HolyEqmLotteryData."
d = dump(lua_dir, "dungeon_heaven_data.lua", [D + n for n in ("data_const", "data_customs", "data_chapter", "data_star_award", "data_star_cond", "data_count_buy")])
h = dump(lua_dir, "holy_eqm_lottery_data.lua", [H + n for n in ("data_const", "data_group", "data_award", "data_wish_id")])
d = {k[len(D):]: v for k, v in d.items()}
h = {k[len(H):]: v for k, v in h.items()}


def odds(desc):
    """'神装碎片: 45.13%\n空岛原石: 28.50%\n1星凡品: 5.70%...' as [(name, per mille)]."""
    return [[n, round(float(p) * 10)] for n, p in re.findall(r"([^\n:：]+)[:：]\s*([0-9.]+)%", desc)]


customs = []
for ch in d["data_customs"]:
    for c in ch:
        customs.append({"chapter": c["id"], "order": c["order_id"], "dun_id": c["dun_id"], "boss": c["type"], "power": c["power"], "units": c["unit_id"],
                        "cond": c["cond_info"], "first": c["f_award"], "first_drops": c["f_drops"], "auto": c["auto_award"]})
out = {
    "source": "restored client dungeon_heaven_data.lua, holy_eqm_lottery_data.lua",
    "const": {k: v["val"] for k, v in d["data_const"].items() if not isinstance(v["val"], str) or len(v["val"]) < 40},
    "chapters": [{"id": c["id"], "check_open": c["check_open"], "cond_info": c["cond_info"], "limit_id": c["limit_id"]} for c in d["data_chapter"]],
    "customs": customs,
    "star_award": d["data_star_award"],
    "star_cond": d["data_star_cond"],
    "count_buy": d["data_count_buy"],
    "dial": {"const": {k: v["val"] for k, v in h["data_const"].items() if not isinstance(v["val"], str) or len(v["val"]) < 40},
             "groups": [{"group_id": g["group_id"], "award": g["award"], "can_set_num": g["can_set_num"], "can_set_ids": g["can_set_ids"], "day_limit": g["day_limit_count"],
                         "free_times": g["free_times"], "gain_once": g["gain_once"], "gold_once": g["loss_gold_once"], "gold_ten": g["loss_gold_ten"],
                         "item_once": g["loss_item_once"], "item_ten": g["loss_item_ten"], "odds": odds(g["desc"])} for g in h["data_group"]],
             "awards": h["data_award"]},
}
json.dump(out, open(out_json, "w", encoding="utf-8"), ensure_ascii=False, separators=(",", ":"))
print(len(customs), "customs;", len(out["chapters"]), "chapters;", len(out["star_cond"]), "star conditions;", [g["odds"] for g in out["dial"]["groups"]][0])
