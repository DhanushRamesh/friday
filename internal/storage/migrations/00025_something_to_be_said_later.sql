-- +goose Up
-- Things to be said at a time, rather than because somebody asked.
--
-- A timer and a schedule are one row. They differ only in how the time was
-- written -- "in twenty minutes" against "every weekday at seven" -- and
-- keeping them apart would mean two firing loops and two sets of bugs.
--
-- A reminder only ever says something. It carries words and speaks them; it
-- does not run instructions unattended, because a task firing with nobody
-- watching has nobody to catch it.
--
-- scope decides where it lands. 'client' fires at the client that set it,
-- which is what a timer wants. 'user' fires wherever the person is told
-- things, which is what a reminder wants.
--
-- due_at is UTC like everything else. The person's own hour is worked out
-- from [assistant] timezone when the reminder is made, never stored.
--
-- repeats is null for a one-shot. It is a word rather than a cron
-- expression: cron is a language, and nobody says it aloud.
CREATE TABLE reminders (
    id            CHAR(30)     NOT NULL,
    user_id       CHAR(30)     NOT NULL,
    client_id     CHAR(30)     NULL,
    scope         VARCHAR(16)  NOT NULL,
    title         VARCHAR(160) NOT NULL,
    body          TEXT         NOT NULL,
    due_at        DATETIME(3)  NOT NULL,
    repeats       VARCHAR(16)  NULL,
    status        VARCHAR(16)  NOT NULL,
    created_at    DATETIME(3)  NOT NULL,
    updated_at    DATETIME(3)  NOT NULL,
    last_fired_at DATETIME(3)  NULL,
    fires         INT UNSIGNED NOT NULL DEFAULT 0,
    PRIMARY KEY (id),
    -- What the firing loop asks for, several times a minute, for ever.
    KEY idx_reminders_due (status, due_at),
    KEY idx_reminders_owner (user_id, status, due_at),
    CONSTRAINT fk_reminders_user FOREIGN KEY (user_id)
        REFERENCES users (id) ON DELETE CASCADE
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci;

-- +goose Down
DROP TABLE reminders;
