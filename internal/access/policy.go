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

// Only implemented policy belongs here. Reserved church roles and future
// finance/care/group actions remain denied until their protected flows exist.
var roleRules = [...]roleRule{
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
