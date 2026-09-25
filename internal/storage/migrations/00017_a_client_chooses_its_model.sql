-- +goose Up
-- Let a client choose which model answers it.
--
-- The model was fixed in configuration, so everything got the same one. What
-- suits one client does not suit another: a spoken answer has to arrive before
-- the satellite stops waiting for it, while a browser can wait for a slower
-- and better model. Per client rather than per account for that reason, and
-- because the client is already loaded on every request to authenticate it.
--
-- Null means no choice, and the server's configured model answers. That is
-- what every existing client becomes, so nothing changes until something is
-- picked.
ALTER TABLE clients
    ADD COLUMN vendor VARCHAR(50) NULL AFTER channel,
    ADD COLUMN model VARCHAR(100) NULL AFTER vendor;

-- Copied onto the chat when it is accepted, rather than read back from the
-- client when it runs. A chat recovered after a restart then goes to the model
-- it was accepted for, and a listing says which model actually answered rather
-- than which one that client would use today.
ALTER TABLE chats
    ADD COLUMN vendor VARCHAR(50) NULL AFTER channel,
    ADD COLUMN model VARCHAR(100) NULL AFTER vendor;

-- +goose Down
ALTER TABLE chats
    DROP COLUMN model,
    DROP COLUMN vendor;

ALTER TABLE clients
    DROP COLUMN model,
    DROP COLUMN vendor;
