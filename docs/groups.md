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

`POST /api/v1/admin/groups` creates a group and is restricted to Super Admin.

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

The React group directory, details, My Groups, roster, join/leave, and request
review screens use these APIs. Archive/restore, broader group leader assignment,
and notifications remain on the implementation checklist. No member content feed
or media upload endpoints are implied by group membership.
