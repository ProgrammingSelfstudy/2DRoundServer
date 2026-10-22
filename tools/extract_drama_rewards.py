#!/usr/bin/env python3
"""Extract per-dungeon numbers of the main-line campaign from the restored client's
dungeon_data.lua (Config.DungeonData.data_drama_dungeon_info, columns as in the
client's own accessor): the enemy power, the level limit and per_hook_items
(what one hook interval pays: [asset or item id, amount]).

usage: extract_drama_rewards.py CLIENT_ROOT OUT_JSON
"""
import json, re, sys

src = open(sys.argv[1] + "/outputs/lua-restored-paths/ePoCrKQkWA3vpiOwu2oNfOh02MOeNheW/dungeon_data.lua", encoding="utf-8").read()
declared = int(re.search(r"data_drama_dungeon_info_length = (\d+)", src).group(1))
a = src.index("data_drama_dungeon_info_table = {")
b = src.index("Config.DungeonData.data_drama_const_length")


def lua_list(s):
    """A Lua list literal (braces, numbers, quoted strings) as a Python list."""
    out, instr = "", False
    for ch in s.replace('\\"', '"'):
        if ch == '"':
            instr = not instr
        if not instr and ch in "{}":
            ch = "[" if ch == "{" else "]"
        out += ch
    return json.loads(out)


dungeons = {}
for m in re.finditer(r'\[(\d+)\.0\] = "(.*)",?\n', src[a:b]):
    v = lua_list(m.group(2))
    assert v[4] == int(m.group(1)) and len(v) == 20, (m.group(1), len(v))
    dungeons[v[4]] = {"power": v[13], "lev_limit": v[18], "per_hook": v[14]}
assert len(dungeons) == declared, (len(dungeons), declared)
json.dump({"source": "restored client dungeon_data.lua data_drama_dungeon_info", "dungeons": {str(k): d for k, d in sorted(dungeons.items())}},
          open(sys.argv[2], "w"), separators=(",", ":"))
print(len(dungeons), "dungeons; first:", dungeons[10010])
