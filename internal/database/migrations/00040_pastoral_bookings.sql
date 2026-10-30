-- +goose Up
CREATE TABLE pastor_bookings (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 slot_id uuid NOT NULL REFERENCES pastor_slots(id),
 pastor_id uuid NOT NULL REFERENCES pastor_profiles(account_id),
 member_id uuid NOT NULL REFERENCES accounts(id),
 starts_at timestamptz NOT NULL,
 ends_at timestamptz NOT NULL,
 status text NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','approved','declined','cancelled')),
 version integer NOT NULL DEFAULT 1 CHECK(version>0),
 created_at timestamptz NOT NULL,
 updated_at timestamptz NOT NULL,
 CHECK(pastor_id<>member_id),
 CHECK(ends_at>starts_at)
);
CREATE UNIQUE INDEX pastor_bookings_active_slot ON pastor_bookings(slot_id) WHERE status IN ('pending','approved');
CREATE INDEX pastor_bookings_member ON pastor_bookings(member_id,id);
CREATE INDEX pastor_bookings_pastor ON pastor_bookings(pastor_id,id);
-- No reasons or confidential care notes are collected in this slice.
