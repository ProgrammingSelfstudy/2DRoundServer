-- Channel broadcasts look up the listeners who blocked the speaker.
CREATE INDEX idx_friend_blacklist_target ON friend_blacklist(black_rid, rid);
