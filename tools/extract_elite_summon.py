#!/usr/bin/env python3
"""Extract the elite summon (精英召唤, group 50051) table of the restored client:
Config.RecruitHolidayLuckyData: the cost and the points per draw (data_summon), the
cumulative-draw rewards (data_award), the chances (data_probability) and the heroes one may
pick as the up hero (data_wish). The wish ids are hero items (29400...); each is matched to
the hero of the same name in partners.json (heroes without a match are left out).

usage: extract_elite_summon.py LUA_DIR PARTNERS_JSON ITEM_NAMES_JSON OUT_JSON
  LUA_DIR  .../outputs/readable-source/lua/ePoCrKQkWA3vpiOwu2oNfOh02MOeNheW
"""
import json, re, sys

src = open(sys.argv[1] + "/recruit_holiday_lucky_data.lua", encoding="utf-8").read()
partners = json.load(open(sys.argv[2]))["partners"]
names = json.load(open(sys.argv[3]))["names"]


def block(name):
    a = src.index("Config.RecruitHolidayLuckyData.%s = {" % name)
    m = re.search(r"\n(?:function|Config\.RecruitHolidayLuckyData\.\w+ =)", src[a + 10:])
    return src[a:a + 10 + m.start()] if m else src[a:]


def pairs(body):
    return [[int(a), int(b)] for a, b in re.findall(r"\{\s*(\d+),\s*(\d+),\s*\}", body)]


const = {m.group(1): int(m.group(2)) for m in re.finditer(r"\n\t\tkey = \"(\w+)\",\n\t\tval = (\d+),", block("data_const"))}
summon = block("data_summon")
def field(key):
    m = re.search(r"\n\t\t%s = \{\n(.*?)\n\t\t\}," % key, summon, re.S)
    return pairs(m.group(1))
cost = {k: field(k) for k in ("gain_once", "gain_ten", "loss_item_once", "loss_item_ten")}

awards = []
# the award block lists camp_id, id, times, reward, self_reward in this order
for m in re.finditer(r"camp_id = (\d+),\n\t\t\tid = (\d+),\n\t\t\ttimes = (\d+),\n\t\t\treward = \{\n(.*?)\n\t\t\t\},", block("data_award"), re.S):
    awards.append({"id": int(m.group(2)), "times": int(m.group(3)), "reward": pairs(m.group(4))})
assert awards, "no awards"

prob = []
for m in re.finditer(r"is_up = (\d+),\n\t\t\tname = \"([^\"]*)\",\n\t\t\tnum = \d+,\n\t\t\tprobability = \"([\d.]+)\",\n\t\t\tstar = \"(\d)星\"", block("data_probability")):
    prob.append({"up": int(m.group(1)), "name": m.group(2), "chance": float(m.group(3)), "star": int(m.group(4))})
assert len(prob) == 6, prob

by_name = {}
for bid, p in partners.items():
    by_name.setdefault(p["name"], []).append(int(bid))
wish, missing = [], []
for m in re.finditer(r"\n\t\t\[(\d+)\] = \{", block("data_wish")):
    item = int(m.group(1))
    hits = [b for b in by_name.get(names.get(str(item)), []) if partners[str(b)]["init_star"] == 5]
    (wish if hits else missing).append({"item": item, "bid": hits[0]} if hits else {"item": item, "name": names.get(str(item))})
assert wish, "no wish heroes"

json.dump({"source": "restored client recruit_holiday_lucky_data.lua (group 50051)", "group": 50051,
           "scroll": const["common_s"], "gain_once": cost["gain_once"], "gain_ten": cost["gain_ten"],
           "item_once": cost["loss_item_once"], "item_ten": cost["loss_item_ten"],
           "awards": awards, "chances": prob, "wish": wish, "no_hero": missing},
          open(sys.argv[4], "w"), ensure_ascii=False, separators=(",", ":"))
print(len(awards), "awards;", len(wish), "wish heroes;", len(missing), "without a hero:", [m["name"] for m in missing])
