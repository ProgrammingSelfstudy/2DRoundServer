-- Campaign milestone rewards a role already took (the id is the client's data_drama_reward id).
CREATE TABLE drama_milestones (
  rid INTEGER NOT NULL REFERENCES roles(rid) ON DELETE CASCADE,
  id  INTEGER NOT NULL,
  PRIMARY KEY (rid, id)
);
