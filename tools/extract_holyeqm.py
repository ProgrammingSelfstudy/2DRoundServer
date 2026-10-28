#!/usr/bin/env python3
"""Extract the holy equipment (神装) tables of the restored client (partner_holy_eqm_data.lua): per base item the number of
random attributes, the wash price and the attribute ranges (parsed from the client's own tips text, percent values in tenths
of a percent like the client's attribute numbers), and the suit bonuses.

usage: extract_holyeqm.py LUA_DIR OUT_JSON
"""
import json, re, sys, os
sys.path.insert(0, os.path.dirname(__file__))
from luadump import dump

P = "Config.PartnerHolyEqmData."
m = dump(sys.argv[1], "partner_holy_eqm_data.lua", [P + n for n in ("data_base_info_table", "data_suit_info", "data_const", "data_holy_suit_manage")])
NAMES = {"攻击": "atk", "生命": "hp_max", "速度": "speed", "攻击比": "atk_per", "防御比": "def_per", "生命比": "hp_max_per",
         "治疗": "cure", "暴伤": "crit_ratio", "暴击": "crit_rate", "命中": "hit_rate", "控制": "hit_magic", "物伤": "dam_p",
         "法伤": "dam_s", "抗暴": "tenacity", "闪避": "dodge_rate", "抗控": "dodge_magic", "物免": "res_p", "法免": "res_s"}
line = re.compile(r"^(\S+?):\s*([\d.]+)(%?)\s*-\s*([\d.]+)(%?)$")
bases = {}
for k, v in m[P + "data_base_info_table"].items():
    bid, attr_num, group, limit, price, _unlock, _show, _sort, tips, name, score = v
    rng = {}
    for ln in tips.split("\n")[1:]:
        r = line.match(ln.strip())
        assert r, (k, ln)
        key = NAMES[r.group(1)]
        f = 10 if r.group(3) else 1
        rng[key] = [round(float(r.group(2)) * f), round(float(r.group(4)) * f)]
    bases[k] = {"n": attr_num, "group": group, "price": price, "name": name, "score": score, "range": rng}
suits = {}
for k, rows in m[P + "data_suit_info"].items():
    suits[k] = {str(r["num"]): r["skill_id"] for r in rows}
const = m[P + "data_const"]
out = {"source": "restored client partner_holy_eqm_data.lua", "bases": bases, "suits": suits,
       "clear_return": const["clear_return"]["val"],
       "plans": [{"id": r["id"], "open": r["is_open"], "cost": r["open_cost"]} for r in m[P + "data_holy_suit_manage"]]}
assert len(bases) == 1680 and len(suits) == 140
json.dump(out, open(sys.argv[2], "w"), separators=(",", ":"), ensure_ascii=False)
print(len(bases), len(suits), out["clear_return"])
