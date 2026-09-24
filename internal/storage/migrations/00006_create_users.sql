-- +goose Up
-- A user is the person the server belongs to. Devices are the things they talk
-- through, and conversations belong to the person rather than to any one of
-- them, so an exchange begun on a phone can be continued at a desk.
CREATE TABLE users (
    -- 'usr_' followed by a 26 character ULID.
    id            CHAR(30)     NOT NULL,
    username      VARCHAR(64)  NOT NULL,
    -- bcrypt, not a plain hash. A password is low-entropy and guessable,
    -- unlike a token, so the cost of a slow hash is the point of it.
    password_hash VARCHAR(255) NOT NULL,
    created_at    DATETIME(3)  NOT NULL,
    updated_at    DATETIME(3)  NOT NULL,
    PRIMARY KEY (id),
    UNIQUE INDEX idx_users_username (username)
) ENGINE = InnoDB
  DEFAULT CHARSET = utf8mb4
  COLLATE = utf8mb4_0900_ai_ci;

-- A client was the owner of everything; it is now only a credential, so it is
-- named for what it is. It keeps its own active conversation, because a person
-- may be speaking to a speaker in one room and typing at a laptop in another.
RENAME TABLE clients TO devices;

ALTER TABLE devices
    ADD COLUMN user_id CHAR(30) NULL AFTER id,
    ADD CONSTRAINT fk_devices_user
        FOREIGN KEY (user_id) REFERENCES users (id)
        ON DELETE CASCADE,
    ADD INDEX idx_devices_user (user_id);

-- Conversations move from the device to the person. Rows created before users
-- existed have no owner and are unreachable, which is correct: they belonged
-- to a client that can no longer authenticate.
ALTER TABLE conversations
    DROP FOREIGN KEY fk_conversations_client,
    DROP INDEX idx_conversations_client,
    DROP COLUMN client_id,
    ADD COLUMN user_id CHAR(30) NULL AFTER id,
    ADD CONSTRAINT fk_conversations_user
        FOREIGN KEY (user_id) REFERENCES users (id)
        ON DELETE CASCADE,
    ADD INDEX idx_conversations_user (user_id, updated_at);

-- +goose Down
ALTER TABLE conversations
    DROP FOREIGN KEY fk_conversations_user,
    DROP INDEX idx_conversations_user,
    DROP COLUMN user_id,
    ADD COLUMN client_id CHAR(30) NULL AFTER id,
    ADD INDEX idx_conversations_client (client_id, updated_at);

ALTER TABLE devices
    DROP FOREIGN KEY fk_devices_user,
    DROP INDEX idx_devices_user,
    DROP COLUMN user_id;

RENAME TABLE devices TO clients;

DROP TABLE users;
