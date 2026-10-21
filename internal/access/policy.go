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

// Only implemented policy belongs here. Future finance/care/group actions
// remain denied until their domain flows and authorization boundaries exist.
var roleRules = [...]roleRule{
	{role: "super_administrator", permission: "roles.read"},
	{role: "super_administrator", permission: "roles.operational.manage"},
	{role: "super_administrator", permission: "roles.privileged.manage"},
	{role: "super_administrator", permission: "roles.manage"},
	{role: "super_administrator", permission: "sermons.manage"},
	{role: "assistant_pastor", permission: "roles.read"},
	{role: "assistant_pastor", permission: "roles.operational.manage"},
	{role: "assistant_pastor", permission: "sermons.manage"},
	{role: "staff", permission: "roles.read"},
	{role: "staff", permission: "roles.operational.manage"},
	{role: "staff", permission: "sermons.manage"},
	{role: "content_editor", permission: "sermons.manage"},
	{role: "system_administrator", permission: "sermons.manage"},
	{role: "access_administrator", permission: "roles.manage"},
	{role: "ministry_leader", permission: "ministry.members.manage", ministry: true},
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
