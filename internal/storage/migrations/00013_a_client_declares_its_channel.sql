-- +goose Up
-- Move the channel onto the client, where it is actually known.
--
-- It was inferred from the endpoint: a prompt arriving at /api/chat was
-- recorded as voice. That is wrong, and wrong in the direction that grants
-- more. /api/chat is a wire format -- Ollama's -- and the format says nothing
-- about how the words were produced. Anything able to speak it can call it,
-- and one day something typed will.
--
-- What does know is the client. A token is issued to one thing, and that thing
-- either has a microphone and no way to confirm before acting, or it does not.
-- So the channel is declared when the client registers and copied onto every
-- chat it submits.
--
-- Existing clients default to direct, which grants less. The voice satellite's
-- row is corrected separately, because guessing it from a name a person can
-- edit is the same mistake in a different place.
ALTER TABLE clients
    ADD COLUMN channel VARCHAR(16) NOT NULL DEFAULT 'direct' AFTER name;

-- +goose Down
ALTER TABLE clients
    DROP COLUMN channel;
