#!/usr/bin/env python3
"""Extract the fields of the restored client's item table that decide how an item
is stored and sold: type, overlap (stack limit), expire_type and value (what it
sells for).

Config.ItemDataN.data_unit_table holds one Lua list per item; the order of the
columns comes from the data_unit() function next to it: id, name, icon, type,
sub_type, quality, overlap, gain_type, use_type, then expire_type (only in some
files) and value. value is a list of {id, amount}. Only that flat form is kept;
an empty value (cannot be sold) and the nested form {{{3,15}}}, whose meaning is
not confirmed, both become an empty list.

usage: extract_items.py LUA_DIR OUT_JSON [OUT_NAMES_JSON]
  OUT_NAMES_JSON  optional: {"names": {id: name}} for the admin pages
  LUA_DIR  .../outputs/readable-source/lua/ePoCrKQkWA3vpiOwu2oNfOh02MOeNheW
"""
import glob, json, re, sys
from collections import Counter

# columns 1..9 are the same in every file
ROW = re.compile(r'^\t\[(\d+)\] = "\{(\d+), \\"((?:[^"\\]|\\.)*?)\\", (\d+), (\d+), (\d+), (\d+), (\d+), (\d+), (\d+), (.*)$', re.M)
FLAT = re.compile(r'^\{(?:\{\d+,\d+\},?)*\}$')
PAIR = re.compile(r'\{(\d+),(\d+)\}')


def lua_list(s):
    """The balanced {...} at the start of s, or None."""
    if not s.startswith("{"):
        return None
    depth = 0
    for i, ch in enumerate(s):
        depth += (ch == "{") - (ch == "}")
        if depth == 0:
            return s[:i + 1]
    return None


def parse_value(s):
    lst = lua_list(s.replace(" ", ""))
    assert lst is not None, s[:80]
    return [[int(a), int(b)] for a, b in PAIR.findall(lst)] if FLAT.match(lst) else []


items, names = {}, {}
for path in sorted(glob.glob(sys.argv[1] + "/item_data*.lua")):
    src = open(path, encoding="utf-8").read()
    if "data_unit_table" not in src:
        continue
    has_expire = "expire_type = var_1_1[10]" in src
    declared = int(re.search(r"data_unit_length = (\d+)", src).group(1))
    rows = ROW.findall(src)
    assert len(rows) == declared, (path, len(rows), declared)  # a row the regex missed must not go unnoticed
    for key, id_, name, icon, typ, sub, quality, overlap, gain, use, rest in rows:
        assert key == id_, (path, key, id_)
        assert int(id_) not in items, ("duplicate item", id_)
        expire = 0
        if has_expire:
            m = re.match(r"(\d+), (.*)$", rest)
            expire, rest = int(m.group(1)), m.group(2)
        items[int(id_)] = [int(typ), int(overlap), expire, parse_value(rest)]
        names[int(id_)] = name.replace('\\"', '"')
json.dump({"source": "restored client item_data*.lua data_unit_table",
           "columns": ["type", "overlap", "expire_type", "value"],
           "items": {str(k): v for k, v in sorted(items.items())}}, open(sys.argv[2], "w"), separators=(",", ":"))
if len(sys.argv) > 3:
    json.dump({"source": "restored client item_data*.lua data_unit_table", "names": {str(k): v for k, v in sorted(names.items())}},
              open(sys.argv[3], "w", encoding="utf-8"), ensure_ascii=False, separators=(",", ":"))
print(len(items), "items; types:", dict(sorted(Counter(v[0] for v in items.values()).items())))
print("with a flat sell value:", sum(1 for v in items.values() if v[3]))
