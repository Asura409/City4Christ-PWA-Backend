-- +goose Up
CREATE TABLE pastor_profiles (
 account_id uuid PRIMARY KEY REFERENCES accounts(id),
 bio text NOT NULL DEFAULT '' CHECK(length(bio)<=1000),
 time_zone text NOT NULL CHECK(length(time_zone) BETWEEN 1 AND 100),
 listed boolean NOT NULL DEFAULT true,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE pastor_slots (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 pastor_id uuid NOT NULL REFERENCES pastor_profiles(account_id),
 starts_at timestamptz NOT NULL,
 ends_at timestamptz NOT NULL,
 active boolean NOT NULL DEFAULT true,
 created_at timestamptz NOT NULL DEFAULT now(),
 CHECK(ends_at-starts_at BETWEEN interval '15 minutes' AND interval '2 hours')
);
CREATE INDEX pastor_slots_available ON pastor_slots(pastor_id,starts_at,id) WHERE active;
CREATE TABLE pastoral_audit_events (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 actor_id uuid NOT NULL REFERENCES accounts(id),
 pastor_id uuid NOT NULL REFERENCES pastor_profiles(account_id),
 resource_type text NOT NULL CHECK(resource_type IN ('profile','slot','booking')),
 resource_id uuid NOT NULL,
 action text NOT NULL,
 occurred_at timestamptz NOT NULL
);
-- Existing accounts and appointments are never seeded or reset.
