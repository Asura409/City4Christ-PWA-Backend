-- +goose Up
CREATE TABLE prayer_requests (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 author_id uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
 body text NOT NULL CHECK (length(btrim(body)) BETWEEN 1 AND 3000),
 visibility text NOT NULL CHECK (visibility IN ('community','group','private')),
 group_id uuid REFERENCES groups(id) ON DELETE RESTRICT,
 anonymous boolean NOT NULL DEFAULT false,
 status text NOT NULL DEFAULT 'open' CHECK (status IN ('open','answered','archived')),
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 answered_at timestamptz,
 archived_at timestamptz,
 CHECK ((visibility='group' AND group_id IS NOT NULL) OR (visibility<>'group' AND group_id IS NULL))
);
CREATE INDEX prayer_requests_community_feed ON prayer_requests(created_at DESC,id DESC)
 WHERE visibility='community' AND status<>'archived';
CREATE INDEX prayer_requests_group_feed ON prayer_requests(group_id,created_at DESC,id DESC)
 WHERE visibility='group' AND status<>'archived';
CREATE INDEX prayer_requests_owner_feed ON prayer_requests(author_id,created_at DESC,id DESC)
 WHERE status<>'archived';
CREATE TABLE prayer_acknowledgements (
 request_id uuid NOT NULL REFERENCES prayer_requests(id) ON DELETE CASCADE,
 account_id uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
 prayed_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(request_id,account_id)
);

-- +goose Down
DROP TABLE prayer_acknowledgements;
DROP TABLE prayer_requests;
