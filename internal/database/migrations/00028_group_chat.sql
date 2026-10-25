-- +goose Up
CREATE TABLE group_chat_messages (
    sequence bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    id uuid NOT NULL UNIQUE,
    group_id uuid NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    sender_id uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    client_message_id uuid NOT NULL,
    body text NOT NULL,
    created_at timestamptz NOT NULL,
    deleted_at timestamptz,
    deleted_by uuid REFERENCES accounts(id),
    CONSTRAINT group_chat_message_body_valid CHECK (
      (deleted_at IS NULL AND deleted_by IS NULL AND char_length(body) BETWEEN 1 AND 4000 AND octet_length(body) <= 16384)
      OR (deleted_at IS NOT NULL AND deleted_by IS NOT NULL AND body='')
    ),
    CONSTRAINT group_chat_message_idempotency_unique UNIQUE (group_id,sender_id,client_message_id)
);
CREATE INDEX group_chat_messages_history ON group_chat_messages(group_id,sequence DESC);
CREATE INDEX group_chat_messages_unread ON group_chat_messages(group_id,sequence) INCLUDE (sender_id) WHERE deleted_at IS NULL;

CREATE TABLE group_chat_reads (
    group_id uuid NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    account_id uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    last_read_sequence bigint NOT NULL DEFAULT 0 CHECK (last_read_sequence >= 0),
    last_read_at timestamptz,
    PRIMARY KEY(group_id,account_id)
);

CREATE TABLE group_chat_moderation_events (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    group_id uuid NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    message_id uuid NOT NULL REFERENCES group_chat_messages(id) ON DELETE CASCADE,
    actor_id uuid NOT NULL REFERENCES accounts(id) ON DELETE RESTRICT,
    action text NOT NULL CHECK (action='deleted'),
    created_at timestamptz NOT NULL
);
CREATE INDEX group_chat_moderation_group ON group_chat_moderation_events(group_id,id);

-- +goose Down
DROP TABLE group_chat_moderation_events;
DROP TABLE group_chat_reads;
DROP TABLE group_chat_messages;
