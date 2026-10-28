#!/usr/bin/env python3
"""Extract the artifact (战技) tables of the restored client (partner_artifact_data.lua):
the tiers (10451~10457: attribute and skill counts, recast cost), the attribute ceiling per tier, the compound
rates, the resolve rewards, the skill pools by level and the constants the server uses.

usage: extract_artifact.py LUA_DIR OUT_JSON
"""
import json, sys, os
sys.path.insert(0, os.path.dirname(__file__))
from luadump import dump

P = "Config.PartnerArtifactData."
m = dump(sys.argv[1], "partner_artifact_data.lua", [P + n for n in (
    "data_artifact_const", "data_artifact_data", "data_artifact_attr", "data_artifact_compound",
    "data_artifact_resolve", "data_artifact_skill")])
g = lambda n: m[P + n]
const = g("data_artifact_const")
keep = ("change_condition", "change_gift", "recast_tag", "lucky_item_id", "lucky_artifact_ids", "lucky_artifact_skillids")
out = {
    "source": "restored client partner_artifact_data.lua",
    "const": {k: const[k]["val"] for k in keep},
    "tiers": {k: {"attr_num": v["attr_num"], "skill_num": v["skill_num"], "ref_expend": v["ref_expend"], "limit_lv": v["limit_lv"]}
              for k, v in g("data_artifact_data").items()},
    "attr": g("data_artifact_attr"),
    "compound": g("data_artifact_compound"),
    "resolve": g("data_artifact_resolve"),
    "skills": g("data_artifact_skill"),
}
assert len(out["tiers"]) == 7 and len(out["skills"]) == 4
json.dump(out, open(sys.argv[2], "w"), separators=(",", ":"), ensure_ascii=False)
print({k: len(v) for k, v in out.items() if hasattr(v, "__len__")})
