-- +goose Up
CREATE TABLE notification_preferences(
 account_id uuid PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE,
 push_enabled boolean NOT NULL DEFAULT false,
 groups boolean NOT NULL DEFAULT true,
 events boolean NOT NULL DEFAULT true,
 pastoral boolean NOT NULL DEFAULT true,
 messages boolean NOT NULL DEFAULT true,
 announcements boolean NOT NULL DEFAULT true
);
ALTER TABLE notifications ADD COLUMN group_id uuid REFERENCES groups(id);
CREATE TABLE notification_broadcasts(
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 actor_id uuid NOT NULL REFERENCES accounts(id),
 title text NOT NULL CHECK(char_length(title) BETWEEN 1 AND 120),
 body text NOT NULL CHECK(char_length(body) BETWEEN 1 AND 500),
 group_id uuid REFERENCES groups(id),
 status text NOT NULL DEFAULT 'draft' CHECK(status IN ('draft','sent')),
 version integer NOT NULL DEFAULT 1 CHECK(version>0),
 recipient_count integer NOT NULL DEFAULT 0,
 created_at timestamptz NOT NULL DEFAULT now(),
 sent_at timestamptz
);
CREATE TABLE notification_broadcast_audit(
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 broadcast_id uuid NOT NULL REFERENCES notification_broadcasts(id),
 actor_id uuid NOT NULL REFERENCES accounts(id),
 action text NOT NULL CHECK(action IN ('created','sent')),
 recipient_count integer NOT NULL DEFAULT 0,
 occurred_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE notification_push_subscriptions(
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 account_id uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
 session_id uuid NOT NULL REFERENCES auth_sessions(id) ON DELETE CASCADE,
 endpoint text NOT NULL UNIQUE CHECK(char_length(endpoint)<=2048),
 p256dh text NOT NULL CHECK(char_length(p256dh)<=100),
 auth_secret text NOT NULL CHECK(char_length(auth_secret)<=32),
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX notification_push_account ON notification_push_subscriptions(account_id);
CREATE TABLE notification_push_jobs(
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 notification_id uuid NOT NULL REFERENCES notifications(id) ON DELETE CASCADE,
 subscription_id uuid NOT NULL REFERENCES notification_push_subscriptions(id) ON DELETE CASCADE,
 state text NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','processing','delivered','failed','cancelled')),
 attempts integer NOT NULL DEFAULT 0,
 available_at timestamptz NOT NULL DEFAULT now(),
 lease_until timestamptz,
 lease_token uuid,
 created_at timestamptz NOT NULL DEFAULT now(),
 finished_at timestamptz,
 UNIQUE(notification_id,subscription_id)
);
CREATE INDEX notification_push_ready ON notification_push_jobs(available_at,id) WHERE state IN ('pending','processing');
