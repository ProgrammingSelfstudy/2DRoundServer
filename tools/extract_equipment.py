#!/usr/bin/env python3
"""Extract the hero equipment from the restored client's item tables: items of
type 1-4 (weapon, armor, ... : the item type is the slot), with their quality and
main attributes ({'main_attr', {{'atk', 43}}} in the item row).

usage: extract_equipment.py LUA_DIR OUT_JSON
  LUA_DIR  .../outputs/readable-source/lua/ePoCrKQkWA3vpiOwu2oNfOh02MOeNheW
"""
import glob, json, re, sys

ROW = re.compile(r'^\t\[(\d+)\] = "\{(\d+), \\"((?:[^"\\]|\\.)*?)\\", (\d+), (\d+), (\d+), (\d+), (\d+), (\d+), (.*)$', re.M)
out = {}
for path in sorted(glob.glob(sys.argv[1] + "/item_data*.lua")):
    src = open(path, encoding="utf-8").read()
    if "data_unit_table" not in src:
        continue
    for key, id_, name, icon, typ, sub, quality, overlap, gain, rest in ROW.findall(src):
        if int(typ) not in (1, 2, 3, 4):
            continue
        m = re.search(r"'main_attr',(.*?)\}\}\}", rest)
        attrs = {k: int(v) for k, v in re.findall(r"\{'(\w+)',(\d+)\}", m.group(1) + "}")} if m else {}
        assert attrs, ("equipment without main_attr", id_)
        out[int(id_)] = {"name": name, "slot": int(typ), "quality": int(quality), "attr": attrs}
json.dump({"source": "restored client item_data*.lua, items of type 1-4", "equipment": {str(k): v for k, v in sorted(out.items())}},
          open(sys.argv[2], "w", encoding="utf-8"), ensure_ascii=False, separators=(",", ":"))
print(len(out), "equipment items;", sorted({tuple(v["attr"]) for v in out.values()}))
