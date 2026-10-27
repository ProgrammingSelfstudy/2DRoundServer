"""Reading the client's unit tables (unit_data*.lua): the monster groups of a dungeon and their monsters.
Shared by the extract_*.py tools that need the monsters of a unit group."""
import glob, json, re


def lua_row(text):
    out, in_str, esc, quote = [], False, False, ""
    for ch in text:
        if in_str:
            if esc:
                esc = False
                out.append(ch)
            elif ch == "\\":
                esc = True
                out.append(ch)
            elif ch == quote:
                in_str = False
                out.append('"')
            else:
                out.append(ch)
        elif ch in ('"', "'"):
            in_str, quote = True, ch
            out.append('"')
        elif ch == "{":
            out.append("[")
        elif ch == "}":
            out.append("]")
        else:
            out.append(ch)
    text = re.sub(r",\s*\]", "]", "".join(out))
    text = re.sub(r"\[\s*,", "[null,", text)
    text = re.sub(r",\s*,", ",null,", text)
    return json.loads(text)


def table_rows(path, table):
    src = open(path, encoding="utf-8").read()
    i = src.find(table + " = {")
    if i < 0:
        return {}
    blk = src[i:]
    blk = blk[:blk.find("\n}\n")]
    rows = {}
    for m in re.finditer(r'\n\t\[(\d+)(?:\.0)?\] = "((?:[^"\\]|\\.)*)",?', blk):
        rows[int(m.group(1))] = lua_row(m.group(2).replace('\\"', '"').replace("\\\\", "\\"))
    return rows


def load_units(unit_dir):
    """Returns (groups, monsters): unit group id -> row, monster id -> row. A group's monsters are the ids at [11:16]; a monster row is
    [id, name, career, camp, ?, lev, ..., head at 12]."""
    groups, monsters = {}, {}
    for f in sorted(glob.glob(unit_dir + "/unit_data[0-9]*.lua")):
        src = open(f, encoding="utf-8").read()
        for name, dest in (("data_unit1_table", groups), ("data_unit2_table", monsters)):
            m = re.search(r"(Config\.UnitData\d*\." + name + r")", src)
            if m:
                dest.update(table_rows(f, m.group(1)))
    return groups, monsters


def monsters_of(groups, monsters, unit_id):
    """[[bid, name, career, camp, lev, head]] of a unit group."""
    out = []
    for mid in (groups.get(unit_id) or [])[11:16]:
        m = monsters.get(mid) if mid else None
        if m:
            out.append([mid, m[1], m[2], m[3], m[5], str(m[12])])
    return out
