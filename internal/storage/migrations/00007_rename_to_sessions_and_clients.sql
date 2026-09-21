-- +goose Up
-- Names, corrected to the model. A conversation is a session: one thread of
-- talk, of which a user has many. A device is a client: one logged-in thing,
-- and a laptop running a browser, Postman and a command line is three of
-- them, not one.
RENAME TABLE conversations TO sessions;
RENAME TABLE devices TO clients;

ALTER TABLE tasks
    CHANGE COLUMN conversation_id session_id CHAR(31) NULL;

ALTER TABLE clients
    CHANGE COLUMN active_conversation_id active_session_id CHAR(31) NULL;

-- Identifiers carry their kind as a prefix, so those change too. The lengths
-- are unchanged, 'conv_' for 'sess_' and 'dev_' for 'cli_', which is why no
-- column has to be widened or narrowed.
--
-- Foreign key checking is suspended for the rewrite: tasks reference sessions
-- by the very value being rewritten, and both sides cannot be changed at once.
-- +goose StatementBegin
SET FOREIGN_KEY_CHECKS = 0;
-- +goose StatementEnd

UPDATE sessions SET id = CONCAT('sess_', SUBSTRING(id, 6)) WHERE id LIKE 'conv\_%';
UPDATE tasks SET session_id = CONCAT('sess_', SUBSTRING(session_id, 6)) WHERE session_id LIKE 'conv\_%';
UPDATE clients SET active_session_id = CONCAT('sess_', SUBSTRING(active_session_id, 6)) WHERE active_session_id LIKE 'conv\_%';
UPDATE clients SET id = CONCAT('cli_', SUBSTRING(id, 5)) WHERE id LIKE 'dev\_%';

-- +goose StatementBegin
SET FOREIGN_KEY_CHECKS = 1;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SET FOREIGN_KEY_CHECKS = 0;
-- +goose StatementEnd

UPDATE clients SET id = CONCAT('dev_', SUBSTRING(id, 5)) WHERE id LIKE 'cli\_%';
UPDATE clients SET active_session_id = CONCAT('conv_', SUBSTRING(active_session_id, 6)) WHERE active_session_id LIKE 'sess\_%';
UPDATE tasks SET session_id = CONCAT('conv_', SUBSTRING(session_id, 6)) WHERE session_id LIKE 'sess\_%';
UPDATE sessions SET id = CONCAT('conv_', SUBSTRING(id, 6)) WHERE id LIKE 'sess\_%';

-- +goose StatementBegin
SET FOREIGN_KEY_CHECKS = 1;
-- +goose StatementEnd

ALTER TABLE clients
    CHANGE COLUMN active_session_id active_conversation_id CHAR(31) NULL;

ALTER TABLE tasks
    CHANGE COLUMN session_id conversation_id CHAR(31) NULL;

RENAME TABLE clients TO devices;
RENAME TABLE sessions TO conversations;
