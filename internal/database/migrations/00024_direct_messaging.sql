-- +goose Up
CREATE TABLE direct_conversations (
    id uuid PRIMARY KEY,
    participant_low uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    participant_high uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL,
    CONSTRAINT direct_conversation_pair_sorted CHECK (participant_low < participant_high),
    CONSTRAINT direct_conversation_pair_unique UNIQUE (participant_low, participant_high)
);

CREATE TABLE direct_conversation_members (
    conversation_id uuid NOT NULL REFERENCES direct_conversations(id) ON DELETE CASCADE,
    account_id uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    joined_at timestamptz NOT NULL,
    last_read_sequence bigint NOT NULL DEFAULT 0 CHECK (last_read_sequence >= 0),
    last_read_at timestamptz,
    PRIMARY KEY (conversation_id, account_id)
);

CREATE TABLE direct_messages (
    sequence bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    id uuid NOT NULL UNIQUE,
    conversation_id uuid NOT NULL,
    sender_id uuid NOT NULL,
    client_message_id uuid NOT NULL,
    body text NOT NULL CHECK (char_length(body) BETWEEN 1 AND 4000 AND octet_length(body) <= 16384),
    created_at timestamptz NOT NULL,
    CONSTRAINT direct_message_sender_is_member FOREIGN KEY (conversation_id, sender_id)
      REFERENCES direct_conversation_members(conversation_id, account_id) ON DELETE CASCADE,
    CONSTRAINT direct_message_idempotency_unique UNIQUE (conversation_id, sender_id, client_message_id)
);
CREATE INDEX direct_messages_history ON direct_messages(conversation_id, sequence DESC);
CREATE INDEX direct_messages_unread ON direct_messages(conversation_id, sequence) INCLUDE (sender_id);

-- +goose Down
DROP TABLE direct_messages;
DROP TABLE direct_conversation_members;
DROP TABLE direct_conversations;
