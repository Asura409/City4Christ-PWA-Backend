package access

// Capability is an effective role-derived permission at one exact scope.
// Empty scope means global. Ministry permissions require a canonical UUID.
type Capability struct {
	Permission string `json:"permission"`
	Scope      string `json:"scope"`
}

type roleRule struct {
	role, permission string
	ministry         bool
}

// Only implemented policy belongs here. Administrative member access, role
// delegation, and church-wide group administration are Super Admin duties.
// Group leaders receive only exact ministry-scoped capabilities.
var roleRules = [...]roleRule{
	{role: "super_administrator", permission: "accounts.deletion.review"},
	{role: "super_administrator", permission: "visitors.follow_up"},
	{role: "assistant_pastor", permission: "visitors.follow_up"},
	{role: "staff", permission: "visitors.follow_up"},
	{role: "super_administrator", permission: "broadcasts.send"},
	{role: "assistant_pastor", permission: "broadcasts.send"},
	{role: "staff", permission: "broadcasts.send"},
	{role: "super_administrator", permission: "roles.read"},
	{role: "super_administrator", permission: "members.records.read"},
	{role: "super_administrator", permission: "members.records.audit.read"},
	{role: "super_administrator", permission: "roles.operational.manage"},
	{role: "super_administrator", permission: "roles.privileged.manage"},
	{role: "super_administrator", permission: "roles.manage"},
	{role: "super_administrator", permission: "sermons.manage"},
	{role: "super_administrator", permission: "devotionals.manage"},
	{role: "super_administrator", permission: "groups.create"},
	{role: "super_administrator", permission: "groups.manage"},
	{role: "super_administrator", permission: "groups.members.manage"},
	{role: "super_administrator", permission: "events.manage"},
	{role: "super_administrator", permission: "prayer.moderate"},
	{role: "super_administrator", permission: "groups.content.moderate"},
	{role: "assistant_pastor", permission: "sermons.manage"},
	{role: "assistant_pastor", permission: "devotionals.manage"},
	{role: "assistant_pastor", permission: "events.manage"},
	{role: "assistant_pastor", permission: "prayer.moderate"},
	{role: "assistant_pastor", permission: "groups.content.moderate"},
	{role: "staff", permission: "sermons.manage"},
	{role: "staff", permission: "devotionals.manage"},
	{role: "staff", permission: "events.manage"},
	// Staff has operational follow-up and publishing access only. Group and
	// prayer moderation require a separate scoped or pastoral workflow.
	{role: "content_editor", permission: "sermons.manage"},
	{role: "content_editor", permission: "devotionals.manage"},
	{role: "system_administrator", permission: "sermons.manage"},
	{role: "system_administrator", permission: "devotionals.manage"},
	{role: "ministry_leader", permission: "ministry.members.manage", ministry: true},
	{role: "ministry_leader", permission: "prayer.moderate", ministry: true},
	{role: "ministry_leader", permission: "groups.content.moderate", ministry: true},
}

func (rule roleRule) acceptsScope(scope string) bool {
	if rule.ministry {
		return validID(scope)
	}
	return scope == ""
}
func rolesForPermission(permission, scope string) []string {
	roles := []string{}
	for _, rule := range roleRules {
		if rule.permission == permission && rule.acceptsScope(scope) {
			roles = append(roles, rule.role)
		}
	}
	return roles
}
func capabilitiesFor(a Assignment) []Capability {
	result := []Capability{}
	for _, rule := range roleRules {
		if rule.role == a.Role && rule.acceptsScope(a.Scope) {
			result = append(result, Capability{Permission: rule.permission, Scope: a.Scope})
		}
	}
	return result
}
