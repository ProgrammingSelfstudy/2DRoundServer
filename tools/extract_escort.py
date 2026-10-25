#!/usr/bin/env python3
"""Extract the escort rewards (escort_data.lua data_rewards_table) into internal/game/data/escort.json."""
import json, re, sys

SRC = sys.argv[1]
OUT = sys.argv[2] if len(sys.argv) > 2 else "internal/game/data/escort.json"
text = open(SRC, encoding="utf-8").read()
rewards = {}
for key, body in re.findall(r'\["(\d+_\d+)"\] = "(\{.*?\})"\s*,', text):
    nums = json.loads(body.replace("{", "[").replace("}", "]"))
    rewards[key] = {"award": nums[2], "plunder": nums[3]}
if len(rewards) != 500:
    sys.exit("expected 500 rewards, got %d" % len(rewards))
json.dump({"rewards": rewards}, open(OUT, "w"), ensure_ascii=False, separators=(",", ":"), sort_keys=True)
print(len(rewards), "rewards ->", OUT)
