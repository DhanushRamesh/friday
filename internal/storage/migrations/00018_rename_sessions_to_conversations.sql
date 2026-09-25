-- +goose Up
-- A session is a conversation. Migration 00007 went the other way, and the
-- word has since been taken: everywhere else in this field a session is one
-- exchange with a service, a single call and its reply. What this holds is a
-- thread of talk that outlives any number of those, which is a conversation.
--
-- 'sess_' and 'conv_' are both five characters, so no column changes width
-- and the identifiers stay CHAR(31).
RENAME TABLE sessions TO conversations;

-- The constraints are dropped first: a foreign key holds the name of the
-- column it is built on, and renaming that column underneath one leaves a
-- constraint pointing at a name nothing has.
ALTER TABLE chats
    DROP FOREIGN KEY fk_chats_session;

ALTER TABLE messages
    DROP FOREIGN KEY fk_messages_session;

ALTER TABLE chats
    CHANGE COLUMN session_id conversation_id CHAR(31) NULL;

ALTER TABLE messages
    CHANGE COLUMN session_id conversation_id CHAR(31) NOT NULL;

ALTER TABLE clients
    CHANGE COLUMN active_session_id active_conversation_id CHAR(31) NULL;

-- Foreign key checking is suspended for the rewrite: chats and messages
-- reference a conversation by the very value being rewritten, and both sides
-- cannot change at once.
-- +goose StatementBegin
SET FOREIGN_KEY_CHECKS = 0;
-- +goose StatementEnd

UPDATE conversations SET id = CONCAT('conv_', SUBSTRING(id, 6)) WHERE id LIKE 'sess\_%';
UPDATE chats SET conversation_id = CONCAT('conv_', SUBSTRING(conversation_id, 6)) WHERE conversation_id LIKE 'sess\_%';
UPDATE messages SET conversation_id = CONCAT('conv_', SUBSTRING(conversation_id, 6)) WHERE conversation_id LIKE 'sess\_%';
UPDATE clients SET active_conversation_id = CONCAT('conv_', SUBSTRING(active_conversation_id, 6)) WHERE active_conversation_id LIKE 'sess\_%';

-- +goose StatementBegin
SET FOREIGN_KEY_CHECKS = 1;
-- +goose StatementEnd

ALTER TABLE chats
    ADD CONSTRAINT fk_chats_conversation
        FOREIGN KEY (conversation_id) REFERENCES conversations (id) ON DELETE CASCADE;

ALTER TABLE messages
    ADD CONSTRAINT fk_messages_conversation
        FOREIGN KEY (conversation_id) REFERENCES conversations (id) ON DELETE CASCADE;

-- An index carries the old word in its name as well as its columns.
ALTER TABLE chats
    DROP INDEX idx_chats_session,
    ADD INDEX idx_chats_conversation (conversation_id);

ALTER TABLE messages
    DROP INDEX idx_messages_session_seq,
    ADD UNIQUE INDEX idx_messages_conversation_seq (conversation_id, seq);

ALTER TABLE conversations
    DROP INDEX idx_sessions_user_archived,
    ADD INDEX idx_conversations_user_archived (user_id, archived_at, updated_at);

-- +goose Down
ALTER TABLE conversations
    DROP INDEX idx_conversations_user_archived,
    ADD INDEX idx_sessions_user_archived (user_id, archived_at, updated_at);

ALTER TABLE messages
    DROP INDEX idx_messages_conversation_seq,
    ADD UNIQUE INDEX idx_messages_session_seq (conversation_id, seq);

ALTER TABLE chats
    DROP INDEX idx_chats_conversation,
    ADD INDEX idx_chats_session (conversation_id);

ALTER TABLE chats
    DROP FOREIGN KEY fk_chats_conversation;

ALTER TABLE messages
    DROP FOREIGN KEY fk_messages_conversation;

-- +goose StatementBegin
SET FOREIGN_KEY_CHECKS = 0;
-- +goose StatementEnd

UPDATE clients SET active_conversation_id = CONCAT('sess_', SUBSTRING(active_conversation_id, 6)) WHERE active_conversation_id LIKE 'conv\_%';
UPDATE messages SET conversation_id = CONCAT('sess_', SUBSTRING(conversation_id, 6)) WHERE conversation_id LIKE 'conv\_%';
UPDATE chats SET conversation_id = CONCAT('sess_', SUBSTRING(conversation_id, 6)) WHERE conversation_id LIKE 'conv\_%';
UPDATE conversations SET id = CONCAT('sess_', SUBSTRING(id, 6)) WHERE id LIKE 'conv\_%';

-- +goose StatementBegin
SET FOREIGN_KEY_CHECKS = 1;
-- +goose StatementEnd

ALTER TABLE clients
    CHANGE COLUMN active_conversation_id active_session_id CHAR(31) NULL;

ALTER TABLE messages
    CHANGE COLUMN conversation_id session_id CHAR(31) NOT NULL;

ALTER TABLE chats
    CHANGE COLUMN conversation_id session_id CHAR(31) NULL;

RENAME TABLE conversations TO sessions;

ALTER TABLE chats
    ADD CONSTRAINT fk_chats_session
        FOREIGN KEY (session_id) REFERENCES sessions (id) ON DELETE CASCADE;

ALTER TABLE messages
    ADD CONSTRAINT fk_messages_session
        FOREIGN KEY (session_id) REFERENCES sessions (id) ON DELETE CASCADE;
