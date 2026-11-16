CREATE TABLE ratings (
    request_id TEXT PRIMARY KEY,
    score INTEGER NOT NULL CHECK (score BETWEEN 1 AND 5)
);
