#!/usr/bin/env python3
"""Extract the main-line campaign ("drama") dungeon chain from the restored
client's dungeon_data.lua (Config.DungeonData.data_drama_dungeon_info).
Column order comes from the client's own accessor: name, mode, chapter_id,
land_id, id, next_id, ...

usage: extract_drama_dungeons.py CLIENT_ROOT OUT_JSON
"""
import json, re, sys

src = open(sys.argv[1] + "/outputs/lua-restored-paths/ePoCrKQkWA3vpiOwu2oNfOh02MOeNheW/dungeon_data.lua", encoding="utf-8").read()
declared = int(re.search(r"data_drama_dungeon_info_length = (\d+)", src).group(1))
a = src.index("data_drama_dungeon_info_table = {")
b = src.index("Config.DungeonData.data_drama_const_length")
rows = {}
for m in re.finditer(r'\[(\d+)\.0\] = "\{\\"([^"\\]*)\\", (\d+), (\d+), (\d+), (\d+), (\d+),', src[a:b]):
    did, name, mode, chap, land, ident, nxt = m.groups()
    assert did == ident
    rows[int(did)] = {"id": int(did), "name": name, "mode": int(mode), "chapter": int(chap), "land": int(land), "next": int(nxt)}
assert len(rows) == declared, (len(rows), declared)
nexts = {r["next"] for r in rows.values()}
heads = sorted(i for i in rows if i not in nexts)
assert len(heads) == 1, heads
chain, seen = [heads[0]], {heads[0]}
while rows[chain[-1]]["next"] in rows:
    n = rows[chain[-1]]["next"]
    assert n not in seen
    chain.append(n); seen.add(n)
assert len(chain) == len(rows), "dungeons are not one single chain"
json.dump({"source": "restored client dungeon_data.lua Config.DungeonData.data_drama_dungeon_info",
           "first": heads[0], "dungeons": [rows[i] for i in chain]},
          open(sys.argv[2], "w"), ensure_ascii=False)
print(len(chain), "dungeons in one chain; first", heads[0], rows[heads[0]]["name"], "; last", chain[-1], rows[chain[-1]]["name"])
