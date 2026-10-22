#!/usr/bin/env python3
"""Extract the skills the battle script needs from the restored client
(Config.SkillData*.data_get_skill_table): per skill id the name, whether it is a
normal attack, the cooldown (cd), the first round it may be cast (fire_cd), which
side it targets, how many targets and how, and the damage or heal percent. The
client keeps the rules only as Chinese text (des), so the numbers are read from it.
The statuses a skill adds (buffs: stun, damage over time, shield, attack down, ...)
come from its buff_des ids and from the buff table Config.SkillData.data_get_buff.

Skills are taken for every hero star in partners.json and for the extra ids given
in EXTRA (the monsters of the wave).

usage: extract_skills.py LUA_DIR PARTNERS_JSON OUT_JSON [EXTRA_ID ...]
  LUA_DIR  .../outputs/lua-restored-paths/ePoCrKQkWA3vpiOwu2oNfOh02MOeNheW
"""
import glob, json, re, sys

lua_dir, partners_json, out_json = sys.argv[1:4]
extra = {int(x) for x in sys.argv[4:]}

wanted = set(extra)
for p in json.load(open(partners_json, encoding="utf-8"))["partners"].values():
    for st in p["stars"].values():
        wanted.update(sk[1] for sk in st.get("skills", []))


def lua_row(text):
    """A row is a Lua table literal like {1, "a", {2, 3}}; turn it into a Python list."""
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
        elif ch in ('"', "'"):  # some rows use single quotes: ['hp_max_per', 200]
            in_str, quote = True, ch
            out.append('"')
        elif ch == "{":
            out.append("[")
        elif ch == "}":
            out.append("]")
        else:
            out.append(ch)
    return json.loads("".join(out).replace(",]", "]"))


# ---- the battle buff table: Config.SkillData.data_get_buff in skill_data.lua ----
buffs = {}
main = open(lua_dir + "/skill_data.lua", encoding="utf-8").read()
i = main.index("Config.SkillData.data_get_buff = {")
blk = main[i:main.index("\nfunction", i)]
for m in re.finditer(r"\n\t\[(\d+)\] = \{(.*?)\n\t\},?", blk, re.S):
    body = m.group(2)
    g = lambda k, d="": (re.search(r"\b" + k + r" = (\"(?:[^\"\\]|\\.)*\"|-?\d+)", body) or [None, d])[1]
    name = g("name").strip('"')
    desc = g("desc").strip('"')
    kind, pct, base = "other", 0, ""
    num = lambda rx: int(re.search(rx, desc).group(1))
    if "眩晕" in name or "无法行动" in desc:
        kind = "stun"
    elif "每回合损失" in desc:
        kind, pct = "dot", int((re.search(r"施法者攻击力\*(\d+)%", desc) or [0, 0])[1])
    elif "免疫1次伤害" in desc:
        kind = "shield"
    elif re.search(r"攻击力减少(\d+)%", desc):
        kind, pct = "atk_down", num(r"攻击力减少(\d+)%")
    elif re.search(r"每回合回复【施法者攻击力\*(\d+)%】", desc):
        kind, pct, base = "regen", num(r"每回合回复【施法者攻击力\*(\d+)%】"), "atk"
    elif re.search(r"每回合回复【(?:施法者|携带者)生命值上限\*(\d+)%】", desc):
        kind, pct, base = "regen", num(r"每回合回复【(?:施法者|携带者)生命值上限\*(\d+)%】"), "maxhp"
    elif "每层" in desc:
        pass  # per-layer statuses need a stack count we do not keep
    elif re.search(r"攻击力?提升(\d+)%", desc):
        kind, pct = "atk_up", num(r"攻击力?提升(\d+)%")
    elif re.search(r"伤害提升(\d+)%", desc) and "暴击" not in desc:
        kind, pct = "dmg_up", num(r"伤害提升(\d+)%")
    elif re.search(r"暴击率提升(\d+)%", desc):
        kind, pct = "crit_up", num(r"暴击率提升(\d+)%")
    elif re.search(r"暴击伤害提升(\d+)%", desc):
        kind, pct = "crit_dmg_up", num(r"暴击伤害提升(\d+)%")
    elif re.search(r"免伤提升(\d+)%", desc):
        kind, pct = "dmg_reduce", num(r"免伤提升(\d+)%")
    elif re.search(r"单位速度提升(\d+)%", desc):
        kind, pct = "speed_up", num(r"单位速度提升(\d+)%")
    buffs[int(m.group(1))] = {"name": name, "kind": kind, "pct": pct, "duration": int(g("duration", "0") or 0),
                              "special": "无法" in desc, "base": base, "keep": bool(re.search(r"不会被驱散|无法被驱散|不可被驱散|不可驱散", desc)),
                              "stack": int(g("join_type", "0") or 0) == 1, "negative": int(g("positive_or_negative", "0") or 0) in (2, 3)}

rows = {}
for f in sorted(glob.glob(lua_dir + "/skill_data[0-9].lua")):
    src = open(f, encoding="utf-8").read()
    i = src.find("data_get_skill_table = {")
    if i < 0:
        continue
    blk = src[i:]
    end = blk.find("\n}\n")
    blk = blk[:end]
    for m in re.finditer(r'\n\t\[(\d+)\.0\] = "((?:[^"\\]|\\.)*)",?', blk):
        bid = int(m.group(1))
        if bid in wanted:
            rows[bid] = lua_row(m.group(2).replace('\\"', '"').replace("\\\\", "\\"))


def pct(rx, text):
    m = re.search(rx, text)
    return int(m.group(1)) if m else 0


out, kinds = {}, {}
for bid, r in sorted(rows.items()):
    name, is_normal, cd, typ, best, side, des, fire_cd = r[1], r[6], r[7], r[8], r[10], r[11], r[16], r[26]
    dmg = pct(r"造成(\d+)%(?:物理|魔法)?(?:伤害|物害|真实伤害)", des)
    heal = 0
    if side == 2:  # an ally skill that heals: "回复等同自身攻击117%的生命值", "回复其等同自身攻击力60%的生命"
        heal = pct(r"(?:回复|恢复)[^，。；]*?(?:等同)?自身攻击(?:力)?(\d+)%", des)
    n, how = 1, "front"
    if side in (2, 5):  # the own side (2) or the caster itself (5)
        m_max = re.search(r"攻击最高的(\d+)个己方", des)
        m_rnd = re.search(r"随机(\d+)个己方", des)
        m_min = re.search(r"生命(?:值)?最低的(\d+)个己方", des)
        if re.search(r"己方全体|全体己方|全体友方|己方所有|所有己方", des):
            n, how = 99, "all"
        elif m_max:
            n, how = int(m_max.group(1)), "max_atk"
        elif m_rnd:
            n, how = int(m_rnd.group(1)), "random"
        elif re.search(r"随机单体", des):
            n, how = 1, "random"
        elif m_min:
            n, how = int(m_min.group(1)), "min_hp"
        elif best == "min_hp_per" or "生命最低" in des:
            how = "min_hp"
        elif side == 5 or ("自身" in des and "己方" not in des):
            how = "self"
    else:
        m = re.search(r"随机(\d+)个", des)
        m2 = re.search(r"(?:攻击最高|生命最低|最前方)的(\d+)个", des)
        if re.search(r"敌方全体|全体敌方|所有敌方", des):
            n, how = 99, "all"
        elif m:
            n, how = int(m.group(1)), "random"
        elif m2:
            n, how = int(m2.group(1)), "max_atk" if best == "max_atk" else ("min_hp" if best == "min_hp_per" else "front")
        elif best == "min_hp_per" or "生命最低" in des:
            how = "min_hp"
        elif best == "max_atk":
            how = "max_atk"
    ids = []
    for ent in (r[23] if len(r) > 23 and isinstance(r[23], list) else []):
        ids.extend(ent if isinstance(ent, list) else [ent])
    # statuses the buff_des list does not carry but the description names: look the
    # status up in the buff table by kind, percent and rounds
    def lookup(kind, want, rounds):
        for b, v in sorted(buffs.items()):
            if v["kind"] == kind and not v["special"] and v["pct"] == want and (not rounds or v["duration"] == rounds):
                return b
        return None

    families = [
        ("atk_down", r"降低(?:目标)?(\d+)%攻击力"),
        ("atk_up", r"提升[^，。；]*?(\d+)%(?:的)?攻击力?"),
        ("dmg_up", r"提升[^，。；]*?(\d+)%(?:的)?伤害"),
        ("crit_up", r"提升[^，。；]*?(\d+)%(?:的)?暴击率"),
        ("crit_dmg_up", r"提升[^，。；]*?(\d+)%(?:的)?暴击伤害"),
        ("dmg_reduce", r"提升[^，。；]*?(\d+)%(?:的)?免伤"),
        ("speed_up", r"提升[^，。；]*?(\d+)%(?:的)?速度"),
    ]
    for kind, rx in families:
        if any(buffs.get(b, {}).get("kind") == kind for b in ids):
            continue
        m = re.search(rx, des)
        if not m:
            continue
        rounds = re.search(r"持续(\d+)回合", des[m.end():])
        b = lookup(kind, int(m.group(1)), int(rounds.group(1)) if rounds else 0)
        if b:
            ids.append(b)
    if "护盾" in des and not any(buffs.get(b, {}).get("kind") == "shield" for b in ids):
        shield = next((b for b, v in sorted(buffs.items()) if v["kind"] == "shield"), None)
        if shield:
            ids.append(shield)
    purge = 0  # how many negative statuses the skill removes from its targets (99 = all)
    m = re.search(r"驱散(?:目标)?(所有|全部|(\d+)个)?(?:的)?负面", des)
    if m:
        purge = 99 if m.group(1) in ("所有", "全部") else int(m.group(2) or 1)
    pc = re.search(r"(\d+)%概率驱散", des)
    purge_chance = int(pc.group(1)) if pc else 100
    sk_buffs = []
    for b in dict.fromkeys(ids):
        v = buffs.get(b)
        if not v:
            continue
        # the description may call the status by another name than the buff table does
        # ("出血" for 灼烧, "晕眩" for 眩晕): by name first, else the first "N%概率"
        m = re.search(r"(\d+)%(?:的)?概率[^，。；]*?【" + re.escape(v["name"]) + "】", des) or re.search(r"(\d+)%(?:的)?概率", des)
        sk_buffs.append({"bid": b, "name": v["name"], "kind": v["kind"], "pct": v["pct"], "rounds": v["duration"],
                         "stack": v["stack"], "negative": v["negative"], "base": v["base"], "keep": v["keep"], "chance": int(m.group(1)) if m else 100})
    out[bid] = {"buffs": sk_buffs, "name": name, "normal": int(is_normal), "type": typ, "cd": cd, "fire_cd": fire_cd,
                "side": side, "how": how, "n": n, "dmg": dmg, "heal": heal, "purge": purge, "purge_chance": purge_chance, "effects": r[13], "des": des}
    kinds[typ] = kinds.get(typ, 0) + 1

missing = sorted(wanted - set(out))
json.dump({"source": "restored client skill_data*.lua Config.SkillData*.data_get_skill_table; dmg / heal / n / how are read from the Chinese description",
           "skills": {str(k): v for k, v in out.items()}},
          open(out_json, "w", encoding="utf-8"), ensure_ascii=False, separators=(",", ":"))
print(len(out), "skills of", len(wanted), "wanted; by type", kinds, "; missing", len(missing), missing[:10])
