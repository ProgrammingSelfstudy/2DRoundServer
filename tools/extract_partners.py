#!/usr/bin/env python3
"""Extract the hero (partner) basics from the restored client's partner_data.lua:
per bid the name, init_star, the level-1 attributes (data_partner_attr) and the
skills of every star (data_partner_star_table column 14, a list of {pos, skill_bid}).

usage: extract_partners.py LUA_DIR OUT_JSON OUT_GROWTH_JSON
  LUA_DIR  .../outputs/readable-source/lua/ePoCrKQkWA3vpiOwu2oNfOh02MOeNheW
"""
import json, re, sys

src = open(sys.argv[1] + "/partner_data.lua", encoding="utf-8").read()


def block(name, nxt):
    a = src.index("Config.PartnerData.%s = {" % name)
    return src[a:src.index(nxt, a)]


def declared(name):
    return int(re.search(r"Config\.PartnerData\.%s_length = (\d+)" % name, src).group(1))


base = {}
for m in re.finditer(r"\t\[(\d+)\] = \{\n(.*?)\n\t\},", block("data_partner_base", "Config.PartnerData.data_partner_attr_length"), re.S):
    f = dict(re.findall(r"\t\t(\w+) = (\"(?:[^\"\\]|\\.)*\"|-?\d+),", m.group(2)))
    base[int(m.group(1))] = {"name": json.loads(f["name"]), "init_star": int(f["init_star"]), "type": int(f["type"]), "break_id": int(f["break_id"]), "camp": int(f["camp_type"])}
assert len(base) == declared("data_partner_base"), len(base)

attr = {}
for m in re.finditer(r"\t\[(\d+)\] = \{\n(.*?)\n\t\},", block("data_partner_attr", "Config.PartnerData.data_partner_lev_length"), re.S):
    attr[int(m.group(1))] = {k: int(v) for k, v in re.findall(r"\t\t(\w+) = (-?\d+),", m.group(2))}
assert len(attr) == declared("data_partner_attr"), len(attr)


def top_level(s):
    """Split the inside of a Lua list into its top-level elements."""
    out, depth, cur, q, i = [], 0, "", False, 0
    while i < len(s):
        ch = s[i]
        if q:
            cur += ch
            if ch == "\\":
                cur += s[i + 1]; i += 1
            elif ch == '"':
                q = False
        elif ch == '"':
            q = True; cur += ch
        elif ch == "{":
            depth += 1; cur += ch
        elif ch == "}":
            depth -= 1; cur += ch
        elif ch == "," and depth == 0:
            out.append(cur.strip()); cur = ""
        else:
            cur += ch
        i += 1
    if cur.strip():
        out.append(cur.strip())
    return out


skills = {}
for m in re.finditer(r'\t\["(\d+)_(\d+)"\] = "\{(.*)\}",\n', block("data_partner_star_table", "Config.PartnerData.data_partner_fuse_star_length")):
    cols = top_level(m.group(3).replace('\\"', '"'))
    assert cols[0] == m.group(1) and cols[1] == m.group(2), cols[:2]
    sk = [[int(a), int(b)] for a, b in re.findall(r"\{(\d+),(\d+)\}", cols[13])]
    # add_hp, add_atk, add_def, add_speed (per mille, 1000 = x1) and the level cap of this star
    triples = lambda c: [[int(a), int(b), int(n)] for a, b, n in re.findall(r"\{(\d+),(\d+),(\d+)\}", c)]
    skills.setdefault(int(m.group(1)), {})[int(m.group(2))] = {
        "skills": sk, "add": [int(c) for c in cols[4:8]], "lev_max": int(cols[14]),
        # flat bonuses of the star, e.g. {"hp_max": 200, "atk": 40}
        "attr": {a: int(b) for a, b in re.findall(r"\{'(\w+)',(\d+)\}", cols[8])},
        # what going up TO this star takes: the hero itself (bid, star, 1), specific
        # heroes (bid, star, n), heroes of a camp (camp, star, n; camp 0 = any) and
        # other costs (id, amount; currencies are assets)
        "self": triples(cols[9]), "fodder": triples(cols[10]), "camp_fodder": triples(cols[11]),
        "cost": [[int(a), int(b)] for a, b in re.findall(r"\{(\d+),(\d+)\}", cols[12])]}

out = {}
for bid, b in sorted(base.items()):
    a = attr.get(bid)
    assert a is not None and b["init_star"] in skills.get(bid, {}), (bid, "missing attr or init-star skills")
    out[str(bid)] = {"name": b["name"], "init_star": b["init_star"], "type": b["type"], "break_id": b["break_id"], "camp": b["camp"], "attr": a,
                     "stars": {str(s): st for s, st in sorted(skills[bid].items())}}
json.dump({"source": "restored client partner_data.lua (data_partner_base, data_partner_attr, data_partner_star_table)",
           "partners": out}, open(sys.argv[2], "w", encoding="utf-8"), ensure_ascii=False, separators=(",", ":"))


# growth tables: what a level costs, and the break (branch) steps with their level caps
lev = []
lb = block("data_partner_lev", "Config.PartnerData.data_partner_brach_length")
for m in re.finditer(r"\t\{\n\t\tlev = (\d+),\n\t\texpend = \{(.*?)\n\t\t\},\n\t\tget_item = \{(.*?)\}\s*,\n\t\},", lb, re.S):
    pairs = lambda t: [[int(a), int(b)] for a, b in re.findall(r"\{\s*(\d+),\s*(\d+),?\s*\}", t)]
    # get_item: what resetting a hero that stands at this level gives back (cumulative)
    lev.append({"lev": int(m.group(1)), "expend": pairs(m.group(2)), "get": pairs(m.group(3))})
assert len(lev) == declared("data_partner_lev"), len(lev)
assert [l["lev"] for l in lev] == list(range(1, len(lev) + 1))
brach = {}
bb = block("data_partner_brach", "Config.PartnerData.data_partner_star_length")
for m in re.finditer(r'\t\["(\d+)_(\d+)_(\d+)"\] = \{\n(.*?)\n\t\},', bb, re.S):
    body = m.group(4)
    f = {k: int(v) for k, v in re.findall(r"\t\t(\w+) = (-?\d+),", body)}
    ex = re.search(r"\t\texpend = \{(.*?)\n\t\t\},", body, re.S)
    gi = re.search(r"\t\tget_item = \{(?:\}|(.*?)\n\t\t\})", body, re.S)
    brach["%s_%s_%s" % m.group(1, 2, 3)] = {
        "get": [[int(a), int(b)] for a, b in re.findall(r"\{\s*(\d+),\s*(\d+),?\s*\}", gi.group(1))] if gi and gi.group(1) else [],
        "lev_max": f["lev_max"], "add": [f["add_hp"], f["add_atk"], f["add_def"], f["add_speed"]],
        "expend": [[int(a), int(b)] for a, b in re.findall(r"\{\s*(\d+),\s*(\d+),?\s*\}", ex.group(1))] if ex else [],
        "limit": "limit = {}" not in body}
assert len(brach) == declared("data_partner_brach"), len(brach)
json.dump({"source": "restored client partner_data.lua (data_partner_lev, data_partner_brach)", "lev": lev, "brach": brach},
          open(sys.argv[3], "w"), separators=(",", ":"))
print(len(out), "heroes;", len(lev), "levels;", len(brach), "break steps; 10101:", out["10101"]["stars"])
