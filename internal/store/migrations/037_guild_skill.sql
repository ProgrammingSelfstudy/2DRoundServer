-- Guild skills of a role. They belong to the role, not to the guild: the levels stay when it leaves.
-- pve: per career, the heavens (groups of 6 skills) finished and the skills activated in the current one.
-- pvp: per career, the level of each PvP attribute and of the PvP skill; a row exists once the career broke through.
-- flags: whether the first (cheap) reset of each kind was used.
CREATE TABLE guild_skill_pve (
  rid INTEGER NOT NULL REFERENCES roles(rid) ON DELETE CASCADE,
  career INTEGER NOT NULL,
  done INTEGER NOT NULL DEFAULT 0,
  active TEXT NOT NULL DEFAULT '[]',
  PRIMARY KEY (rid, career)
);
CREATE TABLE guild_skill_pvp (
  rid INTEGER NOT NULL REFERENCES roles(rid) ON DELETE CASCADE,
  career INTEGER NOT NULL,
  attrs TEXT NOT NULL DEFAULT '{}',
  skill_lev INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (rid, career)
);
CREATE TABLE guild_skill_flags (
  rid INTEGER PRIMARY KEY REFERENCES roles(rid) ON DELETE CASCADE,
  pve_reset_used INTEGER NOT NULL DEFAULT 0,
  pvp_reset_used INTEGER NOT NULL DEFAULT 0
);
