-- +goose Up
-- Keep what the service actually said, not only what we say about it.
--
-- A failure was stored as one sentence written to be read aloud, which threw
-- away the only thing worth having when something breaks: the words the
-- service used and the status it used them with. The sentence stays where it
-- was, so nothing that reads `error` changes; the exact text goes beside it.
--
-- error_code is what chose the sentence. Storing it means the sentences can
-- be reworded later without the stored rows disagreeing with the live ones,
-- and it can be counted: "how often is this rate limiting" is a question
-- about codes, not about prose.
ALTER TABLE chats
    ADD COLUMN error_code   VARCHAR(40) NOT NULL DEFAULT '' AFTER error,
    ADD COLUMN error_detail TEXT        NULL           AFTER error_code;

-- The same split for the conversation. A failure message shows its sentence
-- and hides the detail behind "more info"; both are given to a model, so that
-- being asked what exactly went wrong is answerable rather than a guess.
ALTER TABLE messages
    ADD COLUMN detail TEXT NULL AFTER content;

-- +goose Down
ALTER TABLE messages
    DROP COLUMN detail;

ALTER TABLE chats
    DROP COLUMN error_detail,
    DROP COLUMN error_code;
