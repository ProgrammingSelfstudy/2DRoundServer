ALTER TABLE replays ADD COLUMN combat_map INTEGER NOT NULL DEFAULT 0;
ALTER TABLE replays ADD COLUMN round_count INTEGER NOT NULL DEFAULT 0;
ALTER TABLE replays ADD COLUMN stage INTEGER NOT NULL DEFAULT 0;

UPDATE replays SET round_count=COALESCE(json_array_length(data, '$.rounds'), 0) WHERE json_valid(data);
