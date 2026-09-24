-- +goose Up
-- A task is a chat. The owner's words: "i mena tere shoudn ot be any task
-- word..if i ask its a chat..for a chat i get response".
--
-- The transient stream a chat produces becomes chat_updates rather than
-- chat_messages: `messages` is now the conversation itself, and two tables a
-- letter apart holding entirely different things is a trap.
--
-- The identifier prefixes are the same length, 'task_' for 'chat_', so no
-- column changes width.
RENAME TABLE tasks TO chats;
RENAME TABLE task_messages TO chat_updates;

ALTER TABLE chat_updates
    CHANGE COLUMN task_id chat_id CHAR(31) NOT NULL;

-- Constraint and index names carried the old word too.
ALTER TABLE chat_updates
    DROP FOREIGN KEY fk_task_messages_task;
ALTER TABLE chats
    DROP FOREIGN KEY fk_tasks_conversation;

ALTER TABLE chats
    RENAME INDEX idx_tasks_conversation TO idx_chats_session,
    RENAME INDEX idx_tasks_status_created TO idx_chats_status_created;

-- Foreign key checking is suspended for the rewrite: chat_updates references
-- chats by the very value being rewritten, and both sides cannot change at
-- once. The constraints are added back afterwards, which re-checks them.
-- +goose StatementBegin
SET FOREIGN_KEY_CHECKS = 0;
-- +goose StatementEnd

UPDATE chats SET id = CONCAT('chat_', SUBSTRING(id, 6)) WHERE id LIKE 'task\_%';
UPDATE chat_updates SET chat_id = CONCAT('chat_', SUBSTRING(chat_id, 6)) WHERE chat_id LIKE 'task\_%';

-- +goose StatementBegin
SET FOREIGN_KEY_CHECKS = 1;
-- +goose StatementEnd

ALTER TABLE chats
    ADD CONSTRAINT fk_chats_session
        FOREIGN KEY (session_id) REFERENCES sessions (id)
        ON DELETE CASCADE;
ALTER TABLE chat_updates
    ADD CONSTRAINT fk_chat_updates_chat
        FOREIGN KEY (chat_id) REFERENCES chats (id)
        ON DELETE CASCADE;

-- +goose Down
ALTER TABLE chat_updates
    DROP FOREIGN KEY fk_chat_updates_chat;
ALTER TABLE chats
    DROP FOREIGN KEY fk_chats_session;

-- +goose StatementBegin
SET FOREIGN_KEY_CHECKS = 0;
-- +goose StatementEnd

UPDATE chat_updates SET chat_id = CONCAT('task_', SUBSTRING(chat_id, 6)) WHERE chat_id LIKE 'chat\_%';
UPDATE chats SET id = CONCAT('task_', SUBSTRING(id, 6)) WHERE id LIKE 'chat\_%';

-- +goose StatementBegin
SET FOREIGN_KEY_CHECKS = 1;
-- +goose StatementEnd

ALTER TABLE chats
    RENAME INDEX idx_chats_session TO idx_tasks_conversation,
    RENAME INDEX idx_chats_status_created TO idx_tasks_status_created;

ALTER TABLE chat_updates
    CHANGE COLUMN chat_id task_id CHAR(31) NOT NULL;

RENAME TABLE chat_updates TO task_messages;
RENAME TABLE chats TO tasks;

ALTER TABLE tasks
    ADD CONSTRAINT fk_tasks_conversation
        FOREIGN KEY (session_id) REFERENCES sessions (id)
        ON DELETE CASCADE;
ALTER TABLE task_messages
    ADD CONSTRAINT fk_task_messages_task
        FOREIGN KEY (task_id) REFERENCES tasks (id)
        ON DELETE CASCADE;
