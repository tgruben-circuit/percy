CREATE TABLE IF NOT EXISTS cells (
    cell_id     TEXT PRIMARY KEY,
    topic_id    TEXT,
    source_type TEXT NOT NULL CHECK(source_type IN ('conversation', 'file')),
    source_id   TEXT NOT NULL,
    source_name TEXT,
    cell_type   TEXT NOT NULL CHECK(cell_type IN ('fact', 'decision', 'preference', 'task', 'risk', 'code_ref')),
    salience    REAL NOT NULL DEFAULT 0.5,
    content     TEXT NOT NULL,
    embedding   BLOB,
    created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
    superseded  BOOLEAN DEFAULT FALSE
);
CREATE INDEX IF NOT EXISTS idx_cells_source ON cells(source_type, source_id);
CREATE INDEX IF NOT EXISTS idx_cells_topic ON cells(topic_id);

CREATE VIRTUAL TABLE IF NOT EXISTS cells_fts USING fts5(
    content,
    content='cells',
    content_rowid='rowid'
);

CREATE TRIGGER IF NOT EXISTS cells_ai AFTER INSERT ON cells BEGIN
    INSERT INTO cells_fts(rowid, content) VALUES (new.rowid, new.content);
END;
CREATE TRIGGER IF NOT EXISTS cells_ad AFTER DELETE ON cells BEGIN
    INSERT INTO cells_fts(cells_fts, rowid, content) VALUES('delete', old.rowid, old.content);
END;
CREATE TRIGGER IF NOT EXISTS cells_au AFTER UPDATE ON cells BEGIN
    INSERT INTO cells_fts(cells_fts, rowid, content) VALUES('delete', old.rowid, old.content);
    INSERT INTO cells_fts(rowid, content) VALUES (new.rowid, new.content);
END;

CREATE TABLE IF NOT EXISTS topics (
    topic_id    TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    summary     TEXT,
    embedding   BLOB,
    cell_count  INTEGER DEFAULT 0,
    updated_at  DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE VIRTUAL TABLE IF NOT EXISTS topics_fts USING fts5(
    summary,
    content='topics',
    content_rowid='rowid'
);

CREATE TRIGGER IF NOT EXISTS topics_au AFTER UPDATE OF summary ON topics BEGIN
    INSERT INTO topics_fts(topics_fts, rowid, summary) VALUES('delete', old.rowid, COALESCE(old.summary, ''));
    INSERT INTO topics_fts(rowid, summary) VALUES (new.rowid, COALESCE(new.summary, ''));
END;
CREATE TRIGGER IF NOT EXISTS topics_ai AFTER INSERT ON topics BEGIN
    INSERT INTO topics_fts(rowid, summary) VALUES (new.rowid, COALESCE(new.summary, ''));
END;
CREATE TRIGGER IF NOT EXISTS topics_ad AFTER DELETE ON topics BEGIN
    INSERT INTO topics_fts(topics_fts, rowid, summary) VALUES('delete', old.rowid, COALESCE(old.summary, ''));
END;

CREATE TABLE IF NOT EXISTS index_state (
    source_type TEXT NOT NULL,
    source_id   TEXT NOT NULL,
    indexed_at  DATETIME NOT NULL,
    hash        TEXT,
    PRIMARY KEY (source_type, source_id)
);
