#!/usr/bin/env python3
"""Extract the seven-day ranking (开服冲榜, days_rank_data.lua): the seven rankings (name, view), the rewards by place, the
quests of each ranking and the constants (the days it runs, how many places get a reward, the values the first place needs).

usage: extract_daysrank.py LUA_DIR OUT_JSON
  LUA_DIR  .../outputs/readable-source/lua/ePoCrKQkWA3vpiOwu2oNfOh02MOeNheW
"""
import json, sys
sys.path.insert(0, __file__.rsplit("/", 1)[0])
from luadump import dump

lua_dir, out_json = sys.argv[1:3]
K = "Config.DaysRankData."
d = dump(lua_dir, "days_rank_data.lua", [K + n for n in ("data_rank_data_const", "data_rank_data", "data_rank_list", "data_rank_quest", "data_rank_quest_id")])
d = {k[len(K):]: v for k, v in d.items()}
c = {k: v["val"] for k, v in d["data_rank_data_const"].items()}


def rows(x):
    return list(x.values()) if isinstance(x, dict) else x


ranks = []
for lst in rows(d["data_rank_list"]):
    r = rows(lst)[0]
    ranks.append({"id": r["id"], "name": r["name"], "view": r["rank_type"]})
ranks.sort(key=lambda r: r["id"])
rewards = {}
for lst in rows(d["data_rank_data"]):
    lst = rows(lst)
    rewards[str(lst[0]["id"])] = sorted(({"low": r["low"], "high": r["high"], "rewards": r["rewards"]} for r in lst), key=lambda r: r["low"])
quests = {}
for qid, q in d["data_rank_quest_id"].items():
    p = q["progress"][0]
    quests[str(qid)] = {"desc": q["desc"], "label": p["cli_label"], "param": p["param"], "target": p["target"], "target_val": p["target_val"], "rewards": q["rewards"]}
by_rank = {str(k): [x["id"] for x in rows(v)] for k, v in d["data_rank_quest"].items()}
out = {"source": "restored client days_rank_data.lua",
       "days": c["end_time"][1], "limit_count": c["reward_limit"],
       "limits": {"drama": c["dugeon_limit"], "tower": c["tower_limit"], "partner": c["partner_limit"], "arena": c["arena_limit"], "team": c["team_limit"]},
       "ranks": ranks, "rewards": rewards, "quests": quests, "quests_of": by_rank}
json.dump(out, open(out_json, "w", encoding="utf-8"), ensure_ascii=False, separators=(",", ":"))
print("daysrank:", len(ranks), "ranks;", len(quests), "quests;", c["end_time"], out["limits"])
