#!/usr/bin/env bash
#
# Nightly backup of FRIDAY's database.
#
# One machine means one backup problem. Everything FRIDAY knows is in MySQL:
# lose it and every session, every answer and every login is gone.
#
# Credentials are read from FRIDAY's own config so there is no second copy to
# keep in step.

set -euo pipefail

CONFIG="${ASSISTANT_CONFIG:-/opt/friday/config.ini}"
DEST="${ASSISTANT_BACKUP_DIR:-/var/backups/friday}"
KEEP_DAYS="${FRIDAY_BACKUP_KEEP_DAYS:-14}"

# users, clients, sessions, tasks, task_messages, and goose's own. A dump with
# fewer has lost something.
EXPECTED_TABLES="${FRIDAY_BACKUP_EXPECTED_TABLES:-6}"

# setting : Reads one value from the [database] section.
#
# Section-aware on purpose: "name" appears under [database] and [provider]
# both, so a file-wide search would find whichever came first and back up
# against the wrong credentials, or silently against nothing.
setting() {
    awk -F= -v key="$1" '
        /^[[:space:]]*\[/ { section = $0; gsub(/[][[:space:]]/, "", section) }
        section == "database" && $1 ~ "^[[:space:]]*" key "[[:space:]]*$" {
            sub(/^[^=]*=[[:space:]]*/, "")
            sub(/[[:space:]]+$/, "")
            print
            exit
        }
    ' "$CONFIG"
}

HOST=$(setting host); HOST=${HOST:-127.0.0.1}
PORT=$(setting port); PORT=${PORT:-3306}
USER=$(setting user); USER=${USER:-friday}
NAME=$(setting name); NAME=${NAME:-friday}
PASSWORD=$(setting password)

# The environment wins, as it does for FRIDAY itself, so a deployment keeping
# its secrets out of the file still backs up.
HOST="${FRIDAY_DATABASE_HOST:-$HOST}"
PORT="${FRIDAY_DATABASE_PORT:-$PORT}"
USER="${FRIDAY_DATABASE_USER:-$USER}"
NAME="${FRIDAY_DATABASE_NAME:-$NAME}"
PASSWORD="${FRIDAY_DATABASE_PASSWORD:-$PASSWORD}"

mkdir -p "$DEST"
STAMP=$(date -u +%Y%m%dT%H%M%SZ)
FILE="$DEST/friday-$STAMP.sql.gz"

# --single-transaction takes a consistent snapshot without locking the tables,
# so a backup running at 3am does not block a task that happens to be running.
# --no-tablespaces is required, not cosmetic: without it mysqldump reads
# INFORMATION_SCHEMA.FILES, which needs the PROCESS privilege — a
# server-wide one this user has no business holding just to back up its own
# database.
MYSQL_PWD="$PASSWORD" mysqldump \
    --host="$HOST" --port="$PORT" --user="$USER" \
    --single-transaction --quick --routines --triggers \
    --no-tablespaces \
    --default-character-set=utf8mb4 \
    "$NAME" | gzip --best > "$FILE.partial"

# Renamed only once written, so a backup interrupted halfway is never mistaken
# for a complete one.
mv "$FILE.partial" "$FILE"
chmod 600 "$FILE"

# A dump that restores nothing is worse than no dump, because it is trusted.
if ! gzip -t "$FILE"; then
    echo "backup is corrupt: $FILE" >&2
    exit 1
fi

# Counted rather than matched with grep -q. Under `set -o pipefail`, grep -q
# exits at the first match and closes the pipe, zcat is killed by SIGPIPE, and
# the pipeline reports failure — so a perfectly good backup is declared
# corrupt. Counting reads the whole stream and cannot do that.
TABLES=$(zcat "$FILE" | grep -c '^CREATE TABLE' || true)
if [ "$TABLES" -lt "$EXPECTED_TABLES" ]; then
    echo "backup holds $TABLES tables, expected at least $EXPECTED_TABLES: $FILE" >&2
    exit 1
fi

find "$DEST" -name 'friday-*.sql.gz' -mtime "+$KEEP_DAYS" -delete
find "$DEST" -name '*.partial' -mtime +1 -delete

echo "backed up to $FILE ($(du -h "$FILE" | cut -f1))"
