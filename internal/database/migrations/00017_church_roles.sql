-- +goose Up
-- Activate definitions only. No account is promoted and bootstrap is explicit.
ALTER TABLE role_definitions DROP CONSTRAINT role_definitions_delegation_policy_check;
ALTER TABLE role_definitions ADD CONSTRAINT role_definitions_delegation_policy_check CHECK (
 delegation_policy IN ('legacy_operator','legacy_access_admin','protected_pending','protected_church')
);
UPDATE role_definitions SET assignment_enabled=true,delegation_policy='protected_church'
 WHERE role IN ('super_administrator','assistant_pastor','staff','finance_officer');

ALTER TABLE account_roles DROP CONSTRAINT account_roles_role_scope;
ALTER TABLE account_roles ADD CONSTRAINT account_roles_role_scope CHECK (
 (role IN ('content_editor','system_administrator','access_administrator',
           'super_administrator','assistant_pastor','staff','finance_officer') AND scope='') OR
 (role='ministry_leader' AND scope<>'')
);

-- Singleton proof of the explicit operator appointment; intentionally unseeded.
CREATE TABLE church_role_bootstrap (
 id boolean PRIMARY KEY CHECK (id),
 account_id uuid NOT NULL REFERENCES accounts(id),
 reason text NOT NULL CHECK (length(btrim(reason)) BETWEEN 1 AND 500),
 occurred_at timestamptz NOT NULL DEFAULT now()
);

-- Protected self-resignation is the sole self-change audit exception. The
-- service additionally serializes and preserves the final effective Super Admin.
ALTER TABLE access_audit_events DROP CONSTRAINT access_audit_events_check;
ALTER TABLE access_audit_events ADD CONSTRAINT access_audit_events_check CHECK (
 actor_id<>target_id OR (action='revoked' AND role='super_administrator')
);
