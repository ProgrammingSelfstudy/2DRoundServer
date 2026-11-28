#!/usr/bin/env python3
"""Extract initiative rules and bonuses from the restored client. Usage: extract_combat_speed.py LUA_DIR OUT_JSON."""
import json
import re
import sys
from pathlib import Path
from decimal import Decimal

from luadump import dump

root, target = sys.argv[1:3]


def table(file, name):
    return dump(root, file + ".lua", [name])[name]


def attributes(pairs):
    result = {}
    for p in pairs:
        if len(p) >= 2 and p[0] in ("speed", "speed_per"):
            key = "flat" if p[0] == "speed" else "per"
            result[key] = result.get(key, 0) + p[1]
        elif len(p) == 4 and p[:2] == ["lev", "speed"]:
            result["level"] = p[2]
    return {k: v for k, v in result.items() if v}


skills = {}
refs = {}
families = {2305, 94310, 50410, 50430, 50940, 51740, 52720, 53710, 54020, 54140, 54340, 55320, 55350, 55410, 55840, 76340, 77010, 77030, 77040, 77440, 77640, 77840, 78240}
first = {}
for file in sorted(Path(root).glob("skill_data[0-9].lua")):
    part = file.stem[-1]
    rows = table(file.stem, "Config.SkillData" + part + ".data_get_skill_table")
    for k, v in rows.items():
        if v[3] in families:
            refs[k] = [bid for group in v[23] for bid in group]
        attrs = attributes(v[14])
        if attrs:
            skills[k] = attrs
        if "必定先手" in v[16]:
            first[k] = "无视控制" in v[16]

# The restored battle units leave sex zero even for named heroes. Resolve
# recognisable roster identities for Chaton's male-ally effect, including skins
# and named variants. An anonymous monster remains unclassified.
male_names = set("""
修罗 卡巴奇 小八 杰斯 克罗马利蒙 艾斯 德雷克 贝基 基德 大悟 贝拉米 小丑巴基 艾尼路 罗 萨博 霍金斯 阿普 冰山
天空骑士 巴里 索隆 弗兰奇 乌索普 布鲁克 韦伯 乔巴 山治 路飞 甚平 Mr.3 阿布萨罗姆 Mr.1 鳄鱼 暴君熊 霍古巴克
月光莫利亚 龙马 鹰眼 多弗朗明哥 黑胡子 布鲁诺 猫头鹰 加布拉 路奇 战桃丸 黄猿 卡普 斯摩格 青雉 战国 麦哲伦
赤犬 藤虎 泽法 茶豚 克洛 阿龙 克利克 瓦尔波 白胡子 红发 香克斯 凯多 佛之战国 金狮子 凯撒 圆月猫蝮蛇 猫蝮蛇
萨坦圣 佩罗斯佩罗 绿牛 烬 文斯莫克·伊治 伊治 尼治 贝克曼 熊 库赞 奥兹 巴基 海尔丁 巴托洛米奥 卡塔库栗
""".split())
roster = table("partner_data", "Config.PartnerData.data_partner_base")
male = {k: True for k, v in roster.items()
        if re.split(r"[（(]", v["name"], maxsplit=1)[0] in male_names}

growth = table("partner_data", "Config.PartnerData.data_partner_brach")
slots = {k: v["skill_num"] for k, v in growth.items()}
skins = table("partner_skin_data", "Config.PartnerSkinData.data_skin_info")
titles = table("honor_data", "Config.HonorData.data_title")
equipment = table("partner_eqm_data", "Config.PartnerEqmData.data_eqm_suit")
holy = table("partner_holy_eqm_data", "Config.PartnerHolyEqmData.data_suit_info")
sets = {}
for k, rows in equipment.items():
    thresholds = {r["num"]: attributes(r["attr"]) for r in rows if attributes(r["attr"])}
    if thresholds:
        sets[k] = thresholds
for k, rows in holy.items():
    thresholds = {}
    for row in rows:
        attrs = attributes([p for p in row["skill_id"] if isinstance(p, list)])
        if attrs:
            thresholds[row["num"]] = attrs
    if thresholds:
        sets[k] = thresholds
item_sets = {}
for part in (2, 3):
    rows = table("item_data" + str(part), f"Config.ItemData{part}.data_unit_table")
    for k, v in rows.items():
        if str(v[14]) in sets:
            item_sets[k] = v[14]

fruits = {}
for tier in ("level", "star", "juexing"):
    rows = table("emoguoshitujian_data", "Config.EmoguoshitujianData.data_get_emoguoshitujian_" + tier)
    fruits[tier] = {k: a for k, r in rows.items() if (a := attributes(r.get("atts", [])))}

buffs = {}
raw_buffs = table("skill_data", "Config.SkillData.data_get_buff")
# The round-start exclusive choices have sibling effects with no speed stat.
extra_groups = {7820, 7824, 6304, 7772, 7774, 2710, 2610, 5434, 114, 162, 1985, 5065, 6200, 7751, 7752, 7754, 7710, 7823}
for k, b in raw_buffs.items():
    desc = b.get("desc", "")
    stats = {}
    pct = re.search(r"速度(?:提升|提高|增加)(\d+)%|增加(\d+)%速度", desc)
    flat = re.search(r"(?:伤害)?速度(?:提升|提高|增加)?(\d+)(?:点|[，,；。])", desc)
    slow = re.search(r"速度减少(\d+)%", desc)
    caster = re.search(r"速度提升施法者速度的(\d+)%", desc)
    marine = re.search(r"己方海军提高([\d.]+)%速度", desc)
    wind = re.search(r"提升\d+攻击和(\d+)速度", desc)
    if wind:
        stats["flat"] = int(wind[1])
    if slow:
        stats["per"] = -int(slow[1]) * 10
    elif caster:
        stats["caster"] = int(caster[1]) * 10
    elif pct:
        stats["per"] = int(pct[1] or pct[2]) * 10
    elif flat:
        stats["flat"] = int(flat[1])
    # 槛槛果实's text says 伤害速度10, with a plain number and no percent.
    if not stats and not marine and int(k) // 10 not in extra_groups and b.get("group") not in (114, 162, 1985, 5065, 6200, 775, 771, 782):
        continue
    cap = re.search(r"最多(?:可)?叠加(?:到|至)?(\d+)层", desc)
    buffs[k] = {"stats": stats, "group": b["group"], "type": b["join_type"], "rank": b["group_pro"],
                "rounds": b["duration"], "keep": bool(re.search("不可.*?驱散|无法被驱散|不会被驱散", desc)),
                "negative": b["positive_or_negative"] in (2, 3)}
    for key, stat in (("atk", "攻击(?:力)?"), ("crit", "暴击(?:几率|率)"), ("mitigation", "免伤"), ("damage", "伤害")):
        val = re.search(stat + r"(?:提升|增加|提高)(\d+)%|增加(\d+)%" + stat, desc)
        if val:
            buffs[k][key] = int(val[1] or val[2])
    if marine:
        buffs[k]["marine"] = int(Decimal(marine[1]) * 10)
    if cap:
        buffs[k]["cap"] = int(cap[1])
    if b["name"] == "速度提升" and not stats:
        del buffs[k]

result = {"male": male, "skills": skills, "refs": refs, "first": first, "slots": slots, "sets": sets, "item_sets": item_sets,
          "skins": {k: a for k, r in skins.items() if (a := attributes(r.get("skin_attr", [])))},
          "titles": {k: a for k, r in titles.items() if (a := attributes(r.get("attr", [])))},
          "fruits": fruits, "buffs": buffs}
with open(target, "w", encoding="utf-8") as output:
    json.dump(result, output, sort_keys=True, ensure_ascii=False, separators=(",", ":"))
