# Groups and membership API

Groups belong to the church. Every verified signed-in member can discover
`church` audience groups. A separate `leadership` audience is available only to
the Super Admin, Assistant Pastors, and assigned Ministry Leaders. Staff do not
automatically receive access to that leadership group. Group membership controls
participation; it does not grant church-wide permissions.

All endpoints require a verified account and Bearer access token. Write requests
also require an allowed `Origin`. Account and IP limits apply.

`GET /api/v1/groups?limit=20&cursor=<group-id>` returns active groups visible to
the caller, the caller's own membership state, and active member count.

`GET /api/v1/groups/{groupId}/members` returns verified active members' account
IDs and names. Active members and authorized church-wide or group-scoped managers
can view the roster. Leadership-group eligibility is checked first. Email,
phone, and other profile fields are never included.

`POST /api/v1/admin/groups` creates a church-audience group for Super Admin,
Assistant Pastor, and Staff. Only Super Admin can create the leadership-audience
group. Creation is audited. The church can have only one active leadership group.

```json
{
  "name": "Choir",
  "description": "Sunday worship team",
  "audience": "church",
  "joinPolicy": "approval"
}
```

`audience` is `church` or `leadership`; `joinPolicy` is `open`, `approval`, or
`closed`. Church groups are discoverable by all verified members. The leadership
group is visible and joinable by the Super Admin, Assistant Pastors, and accounts
assigned a Ministry Leader role. Other accounts receive the same not-found result
for leadership group reads and join attempts, even if they know its ID. A role
change takes effect on the next request.

An `open` group activates membership immediately. An `approval` group creates a
pending request. A `closed` group rejects join attempts. These join rules apply
to both audiences, while the leadership role gate is checked first.

`POST /api/v1/groups/{groupId}/membership` accepts `{}` and creates or returns
the caller's membership/request state. `DELETE` on the same route leaves the
group. Both operations are idempotent for retries.

`GET /api/v1/groups/{groupId}/membership-requests` returns pending verified
members' IDs, first/last names, and request times. Super Admin and Assistant
Pastors can review church-wide. Ministry Leaders can review requests only for the
group scope assigned to them. The leadership group itself can be reviewed only by
eligible leadership accounts; an unscoped Staff role cannot read or decide its
requests.

`PATCH /api/v1/groups/{groupId}/membership-requests/{accountId}` accepts:

```json
{"decision":"approve","reason":"Reviewed request"}
```

Use `decline` for a rejection. The request actor cannot approve their own request.
Repeated identical decisions return `changed: false`; a different decision after
the request has already been resolved returns a validation error.

Staff managers use `GET /api/v1/admin/groups` to list church groups. Add
`?includeArchived=true` to include archived groups. The leadership-audience group
is omitted from the response unless the actor is Super Admin, Assistant Pastor,
or an assigned Ministry Leader. Members cannot use this management route.

`PATCH /api/v1/admin/groups/{groupId}` accepts `{"status":"archived",
"reason":"Ministry paused"}` or status `active` to restore. Only Super Admin,
Assistant Pastor, and Staff can transition church groups; the leadership group
remains Super Admin only. Transitions are repeat-safe and write a reasoned audit
event in the same transaction. Archiving removes the group from member reads and
join requests but preserves its roster and leader assignments for restoration.

`GET /api/v1/admin/groups/{groupId}/leaders` returns the appointed leaders'
verified names and account IDs. Staff cannot query leadership-group leaders.
Appointment/removal uses the existing `/api/v1/admin/accounts/{accountId}/church-roles`
and `/revoke` routes with role `ministry_leader` and scope set to the group UUID.
Those mutations retain their fresh-identity and mandatory app-MFA checks and
write the access audit. The React group administration screen links these flows
to each group; role IDs and protected-group visibility are always checked by the
API.

The React group directory, details, My Groups, roster, join/leave, request
review, and staff administration screens use these APIs. Membership approvals and
declines create transactional, retry-safe notices. See [group posts/events](group-content.md)
for scoped content and [notification delivery](notification-delivery.md) for opt-in push.
