#!/usr/bin/env python3
"""Extract the hero summon (recruit) groups from the restored client:
Config.RecruitData.data_partnersummon_data (costs per group), data_partnersummon_const
and data_summon_data (the pool of each group: the hero card item id and its
displayed chance in percent). Card item ids are turned into hero base ids with
data_partner_base.item_id.

usage: extract_summon.py LUA_DIR OUT_JSON
  LUA_DIR  .../outputs/readable-source/lua/ePoCrKQkWA3vpiOwu2oNfOh02MOeNheW
"""
import json, re, sys

rec = open(sys.argv[1] + "/recruit_data.lua", encoding="utf-8").read()
par = open(sys.argv[1] + "/partner_data.lua", encoding="utf-8").read()

pairs = lambda body: [[int(a), int(b)] for a, b in re.findall(r"\{\s*(\d+),\s*(\d+),?\s*\}", body)]


def section(name, nxt):
    a = rec.index("Config.RecruitData.%s = {" % name)
    return rec[a:rec.index(nxt, a)]


item2bid = {}
bsec = par[par.index("Config.PartnerData.data_partner_base = {"):par.index("Config.PartnerData.data_partner_attr_length")]
for m in re.finditer(r"\t\[(\d+)\] = \{\n\t\tbid = \d+,.*?item_id = (\d+),", bsec, re.S):
    item2bid[int(m.group(2))] = int(m.group(1))

groups = {}
for m in re.finditer(r"\n\t\[(\d+)\] = \{\n(.*?)\n\t\},", section("data_partnersummon_data", "Config.RecruitData.data_partnersummon_const_length"), re.S):
    body = m.group(2)
    g = {"name": json.loads(re.search(r'\tname = ("(?:[^"\\]|\\.)*"),', "\t" + body.replace("\n\t\t", "\n\t")).group(1))}
    for key in ("item_once", "item_five", "ext_item_once", "ext_item_five", "exchange_once", "exchange_once_gain",
                "exchange_five", "exchange_five_gain", "recruit_hero_gain"):
        if re.search(r"\t\t%s = \{\}," % key, body):
            g[key] = []
            continue
        mm = re.search(r"\t\t%s = \{\n(.*?)\n\t\t\}," % key, body, re.S)
        assert mm, key
        g[key] = pairs(mm.group(1))
    dl = re.search(r"\t\tdraw_list = \{(.*?)\n\t\t\},", body, re.S)
    g["draw_list"] = [int(x) for x in re.findall(r"\d+", dl.group(1))]
    groups[int(m.group(1))] = g
assert len(groups) == int(re.search(r"data_partnersummon_data_length = (\d+)", rec).group(1))

const = {k: int(v) for k, v in re.findall(r"\t(\w+) = \{\n\t\tdesc = \"[^\"]*\",\n\t\tkey = \"\w+\",\n\t\tval = (\d+),", section("data_partnersummon_const", "function Config.RecruitData.data_partnersummon_const_fun"))}

for m in re.finditer(r"\n\t\[(\d+)\] = \{\n(.*?)\n\t\},", rec[rec.index("Config.RecruitData.data_summon_data = {"):], re.S):
    rows = re.findall(r'base_id = (\d+),\n\s+id = \d+,\n\s+summon_desc = "([\d.]+)"', m.group(2))
    assert rows and int(m.group(1)) in groups, m.group(1)
    groups[int(m.group(1))]["pool"] = [[item2bid[int(b)], float(p)] for b, p in rows]  # KeyError = a card that is no hero
assert all("pool" in g for g in groups.values())
json.dump({"source": "restored client recruit_data.lua", "const": const, "groups": {str(k): v for k, v in sorted(groups.items())}},
          open(sys.argv[2], "w", encoding="utf-8"), ensure_ascii=False, separators=(",", ":"))
print({k: (g["name"], len(g["pool"]), g["item_once"], g["exchange_once"]) for k, g in groups.items()}, const)
