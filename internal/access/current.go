package access

import (
	"context"
	"sort"
)

type CurrentAccess struct {
	Roles        []Assignment `json:"roles"`
	Capabilities []Capability `json:"capabilities"`
}

// Current describes current effective role policy for UI decisions. It is not
// an authorization credential; every protected action still calls CheckTx.
func (s *Service) Current(ctx context.Context, actor string) (CurrentAccess, error) {
	assignments, err := NewRepository(s.db).Assignments(ctx, actor)
	if err != nil {
		return CurrentAccess{}, err
	}
	result := CurrentAccess{Roles: []Assignment{}, Capabilities: []Capability{}}
	seen := map[Capability]bool{}
	for _, assignment := range assignments {
		capabilities := capabilitiesFor(assignment)
		if len(capabilities) == 0 && !(assignment.Role == "finance_officer" && assignment.Scope == "") {
			continue
		}
		result.Roles = append(result.Roles, assignment)
		for _, capability := range capabilities {
			if !seen[capability] {
				result.Capabilities = append(result.Capabilities, capability)
				seen[capability] = true
			}
		}
	}
	sort.Slice(result.Capabilities, func(i, j int) bool {
		a, b := result.Capabilities[i], result.Capabilities[j]
		if a.Permission == b.Permission {
			return a.Scope < b.Scope
		}
		return a.Permission < b.Permission
	})
	return result, nil
}
