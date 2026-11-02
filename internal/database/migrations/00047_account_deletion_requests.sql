-- +goose Up
ALTER TABLE accounts ADD COLUMN deletion_requested_at timestamptz;
CREATE TABLE account_deletion_requests(
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 account_id uuid NOT NULL UNIQUE REFERENCES accounts(id),
 status text NOT NULL DEFAULT 'requested' CHECK(status IN ('requested','reviewing')),
 version integer NOT NULL DEFAULT 1 CHECK(version>0),
 policy_version text NOT NULL,
 consent boolean NOT NULL CHECK(consent),
 created_at timestamptz NOT NULL,
 updated_at timestamptz NOT NULL
);
CREATE INDEX account_deletion_queue ON account_deletion_requests(status,id);
CREATE TABLE account_deletion_audit_events(
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 request_id uuid NOT NULL REFERENCES account_deletion_requests(id),
 actor_id uuid NOT NULL REFERENCES accounts(id),
 action text NOT NULL CHECK(action IN ('requested','listed','viewed','reviewing')),
 status text NOT NULL CHECK(status IN ('requested','reviewing')),
 version integer NOT NULL CHECK(version>0),
 occurred_at timestamptz NOT NULL
);
-- Requests disable accounts for review; no shared content or audit is erased.
