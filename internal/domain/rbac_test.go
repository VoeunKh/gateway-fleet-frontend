package domain

import "testing"

func TestCan(t *testing.T) {
	all := []Permission{PermRollout, PermConfig, PermPackages, PermFirmware, PermRemote, PermAck}
	want := map[Role]map[Permission]bool{
		RoleAdmin:   {PermRollout: true, PermConfig: true, PermPackages: true, PermFirmware: true, PermRemote: true, PermAck: true},
		RoleRelease: {PermRollout: true, PermConfig: true, PermPackages: true, PermFirmware: true},
		RoleViewer:  {},
		"hacker":    {},
	}
	for role, perms := range want {
		for _, p := range all {
			if got := Can(role, p); got != perms[p] {
				t.Errorf("Can(%s, %s) = %v, want %v", role, p, got, perms[p])
			}
		}
	}
}

func TestRoleNames(t *testing.T) {
	tests := []struct {
		role  Role
		valid bool
		msg   string
	}{
		{RoleAdmin, true, "Your role (Admin) can't do this"},
		{RoleRelease, true, "Your role (Release engineer) can't do this"},
		{RoleViewer, true, "Your role (Viewer) can't do this"},
		{"x", false, "Your role (x) can't do this"},
	}
	for _, tc := range tests {
		if tc.role.Valid() != tc.valid || DenyMessage(tc.role) != tc.msg {
			t.Errorf("%s: valid=%v msg=%q", tc.role, tc.role.Valid(), DenyMessage(tc.role))
		}
	}
}
