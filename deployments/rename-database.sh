#!/bin/bash
# Rename the database and its user from friday to assistant.
#
# Needs MySQL administrator access, which the application's own user does not
# have: it holds ALL PRIVILEGES on `friday`.* and USAGE on everything else, so
# it cannot create the new database. Run this as a MySQL admin:
#
#   sudo mysql < deployments/rename-database.sh        # socket auth, or
#   mysql -u root -p < deployments/rename-database.sh
#
# MySQL has no RENAME DATABASE, so every table is moved across one at a time.
# That is a metadata operation, not a copy, so it is fast and the rows are
# never rewritten. Take the backup below first regardless.
#
# Before running:
#   1. Stop the server:  make stop
#   2. Back up:          mysqldump -u root -p friday | gzip > friday-backup.sql.gz
#
# After running, set the new name and user in config.ini:
#   [database]
#   user = assistant
#   name = assistant
# and give the new user the password you set below.

-- The new home. utf8mb4 throughout, matching what the migrations create.
CREATE DATABASE IF NOT EXISTS assistant
  CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;

-- Move every table across. RENAME TABLE across schemas moves the table
-- without copying its rows.
RENAME TABLE
  friday.users        TO assistant.users,
  friday.clients      TO assistant.clients,
  friday.sessions     TO assistant.sessions,
  friday.chats        TO assistant.chats,
  friday.chat_updates TO assistant.chat_updates,
  friday.messages     TO assistant.messages;

-- goose's own bookkeeping, so migrations do not re-run from the beginning.
RENAME TABLE friday.goose_db_version TO assistant.goose_db_version;

-- The application user. Change the password here before running.
CREATE USER IF NOT EXISTS 'assistant'@'127.0.0.1' IDENTIFIED BY 'CHANGE_ME';
GRANT ALL PRIVILEGES ON assistant.* TO 'assistant'@'127.0.0.1';
FLUSH PRIVILEGES;

-- Left for you to run once the server is up and answering on the new
-- database, so there is a way back until then:
--   DROP DATABASE friday;
--   DROP USER 'friday'@'127.0.0.1';
