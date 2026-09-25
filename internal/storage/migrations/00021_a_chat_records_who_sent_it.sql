-- +goose Up
-- Which client submitted a chat.
--
-- The channel was recorded but not the client, which was enough while a chat
-- only ever produced words. A tool acts on somebody's behalf and some of them
-- act on the client itself -- switching which conversation it talks in -- so
-- the chat has to say which one asked.
--
-- Null for every chat that came before this, and for anything submitted
-- without a client. A tool that needs one refuses rather than guessing.
ALTER TABLE chats
    ADD COLUMN client_id CHAR(30) NULL AFTER conversation_id;

-- +goose Down
ALTER TABLE chats
    DROP COLUMN client_id;
