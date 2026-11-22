#!/usr/bin/env python3
"""Export item descriptions, hero cards and shard recipes for the backpack.

Usage: extract_backpack_actions.py LUA_DIR OUT_JSON
LUA_DIR is the restored client directory containing item_data*.lua and partner_data.lua.
"""

import json
import re
import sys
from pathlib import Path


ITEM_ROW = re.compile(r'^\t\[(\d+)\] = "(\{.*\})",?$', re.M)
TOKEN = re.compile(r'"(?:[^"\\]|\\.)*"|[{},]')


def item_columns(row):
    # The outer Lua string contains a Lua table. Split its top-level fields
    # without evaluating Lua or splitting commas inside descriptions or tables.
    row = re.sub(r'\\(["\\])', r'\1', row)
    start, depth = 1, 0
    columns = []
    for token in TOKEN.finditer(row):
        match token[0]:
            case "{":
                depth += 1
            case "}":
                depth -= 1
                if depth == 0:
                    columns.append(row[start:token.start()].strip())
            case "," if depth == 1:
                columns.append(row[start:token.start()].strip())
                start = token.end()
    return columns


def extract(source):
    descriptions, cards, shard_items = {}, {}, {}
    for path in sorted(source.glob("item_data*.lua")):
        lua = path.read_text(encoding="utf-8")
        if "data_unit_table" not in lua:
            continue
        offset = 0 if "expire_type = var_1_1[10]" in lua else -1
        for key, row in ITEM_ROW.findall(lua):
            cols = item_columns(row)
            assert key == cols[0], (path, key)
            id_, typ = int(key), int(cols[3])
            desc = cols[14 + offset]
            if typ != 7 and desc.startswith('"'):
                desc = re.sub(r'<br\s*/?\s*>', "\n", desc[1:-1], flags=re.I)
                desc = re.sub(r'<[^>]*>', "", desc).replace(r'\n', "\n").replace(r'\"', '"').strip()
                if desc:
                    descriptions[id_] = desc
            effect = cols[11 + offset]
            card = re.fullmatch(r'\{\{effect_type=27,val=\{(\d+),(\d+),1\}\}\}', effect)
            if card:
                cards[id_] = dict(zip(("bid", "star"), map(int, card.groups())))
            if typ == 102:
                hero = re.fullmatch(r'\{\{effect_type=5,val=(\d+)\}\}', effect)
                shard_items[id_] = (int(hero[1]) if hero else 0, int(cols[15 + offset]), int(cols[20 + offset]))

    partners = (source / "partner_data.lua").read_text(encoding="utf-8")
    block = partners.split("Config.PartnerData.data_get_compound_info = {", 1)[1].split("\n}", 1)[0]
    rows = re.findall(r'\[(\d+)\] = \{\s*is_random = (\d+),\s*num = (\d+),\s*rand_id = \d+,\s*\}', block)
    declared = int(re.search(r'data_get_compound_info_length = (\d+)', partners)[1])
    assert len(rows) == declared, (len(rows), declared)
    shards = {}
    for id_, fixed, need in (map(int, row) for row in rows):
        bid, camp, star = shard_items[id_]
        assert star > 0 and need > 0 and (bool(bid) == bool(fixed)), (id_, bid, camp, star)
        shards[id_] = {"bid": bid, "camp": camp, "star": star, "need": need}
    return {"descriptions": descriptions, "hero_cards": cards, "hero_shards": shards}


if __name__ == "__main__":
    result = extract(Path(sys.argv[1]))
    with Path(sys.argv[2]).open("w", encoding="utf-8") as out:
        out.write("{\n")
        for i, (section, rows) in enumerate(result.items()):
            if i:
                out.write(",\n")
            out.write(f'  "{section}": {{\n')
            out.write(",\n".join(f'    "{k}": {json.dumps(v, ensure_ascii=False, separators=(",", ":"))}'
                                 for k, v in sorted(rows.items())))
            out.write("\n  }")
        out.write("\n}\n")
    print({key: len(rows) for key, rows in result.items()})
