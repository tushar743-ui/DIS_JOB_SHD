package authz

import (
	"testing"
)

func TestRoleHierarchy(t *testing.T) {
	tests := []struct {
		holder, required Role
		want             bool
	}{
		{RoleViewer, RoleViewer, true},
		{RoleViewer, RoleMember, false},
		{RoleViewer, RoleAdmin, false},
		{RoleViewer, RoleOwner, false},

		{RoleMember, RoleViewer, true},
		{RoleMember, RoleMember, true},
		{RoleMember, RoleAdmin, false},
		{RoleMember, RoleOwner, false},

		{RoleAdmin, RoleViewer, true},
		{RoleAdmin, RoleMember, true},
		{RoleAdmin, RoleAdmin, true},
		{RoleAdmin, RoleOwner, false},

		{RoleOwner, RoleViewer, true},
		{RoleOwner, RoleMember, true},
		{RoleOwner, RoleAdmin, true},
		{RoleOwner, RoleOwner, true},
	}

	for _, tc := range tests {
		if got := tc.holder.AtLeast(tc.required); got != tc.want {
			t.Errorf("%s.AtLeast(%s) = %v, want %v", tc.holder, tc.required, got, tc.want)
		}
	}
}

func TestUnknownRoleHasNoPrivileges(t *testing.T) {
	rogue := Role("superuser")

	if rogue.Valid() {
		t.Fatal("an unrecognised role reported itself as valid")
	}
	for _, required := range Roles() {
		if rogue.AtLeast(required) {
			t.Errorf("unrecognised role satisfied %s, roles must fail closed", required)
		}
	}
}

func TestEmptyRoleHasNoPrivileges(t *testing.T) {
	if Role("").AtLeast(RoleViewer) {
		t.Fatal("an empty role satisfied viewer, a missing membership must fail closed")
	}
}

func TestKnownRolesAreValid(t *testing.T) {
	for _, role := range Roles() {
		if !role.Valid() {
			t.Errorf("%s is not reported as a valid role", role)
		}
	}
}

func TestRolesAreTotallyOrdered(t *testing.T) {
	ordered := Roles()
	for i := 1; i < len(ordered); i++ {
		lower, higher := ordered[i-1], ordered[i]
		if !higher.AtLeast(lower) {
			t.Errorf("%s does not outrank %s", higher, lower)
		}
		if lower.AtLeast(higher) {
			t.Errorf("%s wrongly outranks %s", lower, higher)
		}
	}
}

func TestEveryAddressableResourceHasAScopingQuery(t *testing.T) {
	wantParams := []string{"orgID", "projectID", "queueID", "jobID", "workerID", "dlqID"}

	found := map[string]bool{}
	for _, res := range resources {
		if res.query == "" {
			t.Errorf("resource %q has no scoping query", res.param)
		}
		found[res.param] = true
	}

	for _, param := range wantParams {
		if !found[param] {
			t.Errorf("no authorization query resolves the %q route parameter, "+
				"routes using it would be unscoped", param)
		}
	}
}

func TestScopingQueriesJoinOrganizationMembership(t *testing.T) {
	for _, res := range resources {
		if !contains(res.query, "organization_members") {
			t.Errorf("query for %q does not join organization_members, "+
				"it would authorize across tenants", res.param)
		}
		if !contains(res.query, "om.user_id = $2") {
			t.Errorf("query for %q does not bind the caller to $2", res.param)
		}
	}
}

func TestQuoteEscapesJSONControlCharacters(t *testing.T) {
	tests := map[string]string{
		`plain`:            `"plain"`,
		`say "hi"`:         `"say \"hi\""`,
		`back\slash`:       `"back\\slash"`,
		"line\nbreak":      `"line\nbreak"`,
		"tab\there":        `"tab\there"`,
		"carriage\rreturn": `"carriage\rreturn"`,
		"null\x00byte":     `"nullbyte"`,
	}

	for input, want := range tests {
		if got := quote(input); got != want {
			t.Errorf("quote(%q) = %s, want %s", input, got, want)
		}
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
