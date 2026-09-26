-- +goose Up
-- Which chat produced a message.
--
-- A message knows its conversation but not the turn that wrote it, so the
-- tool calls and results belonging to one answer cannot be told from those
-- belonging to the next. That is the whole of what an answer's timeline
-- needs, and there was no way to ask for it.
--
-- Null for every message written before this, whose timeline is therefore
-- unavailable rather than wrong.
--
-- CHAR(31), matching chats.id: the "chat_" prefix is five characters, not
-- the four that "mem_" and "usr_" have.
ALTER TABLE messages
    ADD COLUMN chat_id CHAR(31) NULL AFTER conversation_id,
    ADD KEY idx_messages_chat (chat_id);

-- What was recalled for a chat, and what it scored.
--
-- Kept on the chat rather than in the transcript. It is not something
-- anybody said: it is how the answer came to be what it is, written once
-- before the model is asked and read only when somebody wants to know why.
--
-- JSON because its shape belongs to the timeline and not to the schema, and
-- because nothing queries inside it.
ALTER TABLE chats
    ADD COLUMN recalled JSON NULL AFTER error_detail;

-- +goose Down
ALTER TABLE chats
    DROP COLUMN recalled;

ALTER TABLE messages
    DROP KEY idx_messages_chat,
    DROP COLUMN chat_id;
