-- +goose Up
-- Definitions are metadata, not grants. Effective authorization stays in the
-- server policy. Reserved church roles remain rejected by account_roles_role_scope
-- until protected appointment, MFA and bootstrap workflows are implemented.
CREATE TABLE role_definitions (
    role text PRIMARY KEY,
    display_name text NOT NULL CHECK (length(btrim(display_name)) BETWEEN 1 AND 100),
    scope_kind text NOT NULL CHECK (scope_kind IN ('global','ministry')),
    assignment_enabled boolean NOT NULL,
    delegation_policy text NOT NULL CHECK (
        delegation_policy IN ('legacy_operator','legacy_access_admin','protected_pending')
    ),
    CHECK ((assignment_enabled AND delegation_policy <> 'protected_pending') OR
           (NOT assignment_enabled AND delegation_policy = 'protected_pending'))
);

INSERT INTO role_definitions(role,display_name,scope_kind,assignment_enabled,delegation_policy) VALUES
 ('content_editor','Content Editor','global',true,'legacy_access_admin'),
 ('system_administrator','Legacy System Administrator','global',true,'legacy_operator'),
 ('access_administrator','Access Administrator','global',true,'legacy_operator'),
 ('ministry_leader','Ministry Leader','ministry',true,'legacy_access_admin'),
 ('super_administrator','Super Administrator','global',false,'protected_pending'),
 ('assistant_pastor','Assistant Pastor','global',false,'protected_pending'),
 ('staff','Staff','global',false,'protected_pending'),
 ('finance_officer','Finance Officer','global',false,'protected_pending');

ALTER TABLE account_roles ADD CONSTRAINT account_roles_definition
    FOREIGN KEY (role) REFERENCES role_definitions(role);
-- Historical audit records intentionally do not reference this catalog: their
-- role names must remain readable even if definitions change in a later release.
