#!/usr/bin/env python3
"""Extract the seer palace (先知殿) summon from the restored client:
Config.RecruitHighData.data_seerpalace_data (what a summon costs and the bonus every
summon pays) and data_seerpalace_award (the pool: per group a 5-star and a 4-star list of
[item id, amount, chance in percent]).

usage: extract_seerpalace.py LUA_DIR OUT_JSON
  LUA_DIR  .../outputs/readable-source/lua/ePoCrKQkWA3vpiOwu2oNfOh02MOeNheW
"""
import json, re, sys

src = open(sys.argv[1] + "/recruit_high_data.lua", encoding="utf-8").read()
pairs = lambda body: [[int(a), int(b)] for a, b in re.findall(r"\{\s*(\d+),\s*(\d+),?\s*\}", body)]


def section(name, nxt):
    a = src.index("Config.RecruitHighData.%s = {" % name)
    return src[a:src.index(nxt, a)]


groups = {}
for m in re.finditer(r"\n\t\[(\d+)\] = \{\n(.*?)\n\t\},", section("data_seerpalace_data", "function Config.RecruitHighData.data_seerpalace_data_fun"), re.S):
    gid, body = int(m.group(1)), m.group(2)
    field = lambda n: re.search(n + r" = \{(.*?)\n\t\t\}", body, re.S)
    once = field("item_once")
    gain = field("exchange_once_gain")
    groups[gid] = {
        "name": re.search(r'name = "(.*?)"', body).group(1),
        "item_once": pairs(once.group(1)) if once else [],
        "gain_once": pairs(gain.group(1)) if gain else [],
    }

award = {}
asec = src[src.index("Config.RecruitHighData.data_seerpalace_award = {"):]
for gm in re.finditer(r"\n\t\[(\d+)\] = \{\n(.*?)\n\t\},", asec, re.S):
    gid, body = int(gm.group(1)), gm.group(2)
    lists = {}
    for sm in re.finditer(r"\[(\d+)\] = \{\s*desc = .*?star = (\d+),\s*items = \{(.*?)\n\t\t\t\},", body, re.S):
        items = [[int(a), int(b), float(c)] for a, b, c in re.findall(r"\{\s*(\d+),\s*(\d+),\s*([\d.]+),?\s*\}", sm.group(3))]
        assert items, ("an empty list", gid, sm.group(2))
        lists[sm.group(2)] = items
    award[gid] = lists
assert set(award) == {1000, 2000, 3000, 4000}, award.keys()
for gid in (1000, 2000, 3000, 4000):
    assert gid in groups and groups[gid]["item_once"], gid
    total = sum(i[2] for l in award[gid].values() for i in l)
    assert abs(total - 100) < 2, (gid, total)  # the displayed chances add up to about 100 % (rounded); a draw uses them as weights
# The star-card divination (星象占卜) draws from the same window command with groups
# 10000-50000: it costs star-card points (asset 39) and always gives a 5-star entry of
# the camp it is named after (water, fire, wind, light, dark = camp 1-5). The camp of a
# pool item is the number right after its description in the client's item tables.
import glob
camp = {}
want = {int(i[0]) for g in award.values() for l in g.values() for i in l}
for f in glob.glob(sys.argv[1] + "/item_data*.lua"):
    for m in re.finditer(r'\[(\d+)\] = "\{\d+, .*?\\", (\d+), 0, 2, \{\}, 0, \d+, 0, \{', open(f, encoding="utf-8").read()):
        if int(m.group(1)) in want:
            camp[int(m.group(1))] = int(m.group(2))
assert set(camp) == want, sorted(want - set(camp))
score = {}
for gid, base, cp in ((10000, 1000, 1), (20000, 2000, 2), (30000, 3000, 3), (40000, 4000, 4), (50000, 4000, 5)):
    pool = [i for i in award[base]["5"] if camp[i[0]] == cp]
    assert pool, gid
    assert groups[gid]["item_once"] and groups[gid]["item_once"][0][0] == 39, groups.get(gid)
    score[gid] = {"item_once": groups[gid]["item_once"], "pool": pool}
json.dump({"source": "restored client recruit_high_data.lua data_seerpalace_data / data_seerpalace_award",
           "groups": groups, "award": award, "score": score}, open(sys.argv[2], "w"), ensure_ascii=False, separators=(",", ":"))
print({g: len(v["pool"]) for g, v in score.items()}); print({g: (groups[g]["item_once"], groups[g]["gain_once"], {s: len(l) for s, l in award[g].items()}) for g in award})
