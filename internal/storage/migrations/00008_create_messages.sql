-- +goose Up
-- The conversation itself, as a log of what was said rather than something
-- reconstructed from the work that produced it.
--
-- Until now a session's history was read back out of the tasks table, one
-- turn from the prompt column and one from the response. That works only
-- while every exchange is exactly one question and one answer, and only while
-- both are plain text. A message log has somewhere to put a failure the user
-- was shown, and somewhere to grow.
CREATE TABLE messages (
    -- The session this was said in.
    session_id CHAR(31)    NOT NULL,
    -- Position within the session, starting at 1. Assigned on insert, so the
    -- order is the order things were said and needs no timestamp comparison.
    seq        INT         NOT NULL,
    -- 'chat' or 'error'. An error is shown to the user but never sent to a
    -- model: it is FRIDAY reporting that it could not answer, which read back
    -- as conversation would have the model explaining its own outage.
    kind       VARCHAR(16) NOT NULL,
    -- 'user' or 'assistant'.
    role       VARCHAR(16) NOT NULL,
    content    MEDIUMTEXT  NOT NULL,
    created_at DATETIME(3) NOT NULL,
    PRIMARY KEY (session_id, seq),
    CONSTRAINT fk_messages_session
        FOREIGN KEY (session_id) REFERENCES sessions (id)
        ON DELETE CASCADE
) ENGINE = InnoDB
  DEFAULT CHARSET = utf8mb4
  COLLATE = utf8mb4_0900_ai_ci;

-- Carry across what has already been said, so upgrading does not lose the
-- history of a running FRIDAY.
--
-- Each finished piece of work contributes the question, then the answer or
-- the failure it ended in. Ordering is by identifier, which is a ULID and so
-- orders by time, with the question before what followed it.
INSERT INTO messages (session_id, seq, kind, role, content, created_at)
SELECT session_id,
       ROW_NUMBER() OVER (PARTITION BY session_id ORDER BY ordered_by, said_second),
       kind,
       role,
       content,
       said_at
FROM (
    SELECT t.session_id,
           'chat'      AS kind,
           'user'      AS role,
           t.prompt    AS content,
           t.created_at AS said_at,
           t.id        AS ordered_by,
           0           AS said_second
    FROM tasks t
    JOIN sessions s ON s.id = t.session_id
    WHERE t.prompt <> ''

    UNION ALL

    SELECT t.session_id,
           'chat',
           'assistant',
           t.response,
           COALESCE(t.finished_at, t.updated_at, t.created_at),
           t.id,
           1
    FROM tasks t
    JOIN sessions s ON s.id = t.session_id
    WHERE t.response IS NOT NULL AND t.response <> ''

    UNION ALL

    SELECT t.session_id,
           'error',
           'assistant',
           t.error,
           COALESCE(t.finished_at, t.updated_at, t.created_at),
           t.id,
           1
    FROM tasks t
    JOIN sessions s ON s.id = t.session_id
    WHERE t.error IS NOT NULL AND t.error <> ''
) AS said;

-- +goose Down
DROP TABLE messages;
