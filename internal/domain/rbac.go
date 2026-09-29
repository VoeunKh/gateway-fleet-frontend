package domain

import "fmt"

// Role is a user role.
type Role string

// Roles.
const (
	RoleAdmin   Role = "admin"
	RoleRelease Role = "release"
	RoleViewer  Role = "viewer"
)

// Permission is an action a role may perform.
type Permission string

// Permissions.
const (
	PermRollout  Permission = "rollout"
	PermConfig   Permission = "config"
	PermPackages Permission = "packages"
	PermFirmware Permission = "firmware"
	PermRemote   Permission = "remote"
	PermAck      Permission = "ack"
)

// sample: const CAN = {admin:['rollout','config','packages','firmware','remote','ack'],
// release:['rollout','config','packages','firmware'], viewer:[]};
var rolePerms = map[Role][]Permission{
	RoleAdmin:   {PermRollout, PermConfig, PermPackages, PermFirmware, PermRemote, PermAck},
	RoleRelease: {PermRollout, PermConfig, PermPackages, PermFirmware},
	RoleViewer:  {},
}

// sample: const ROLE_NAME = {admin:'Admin', release:'Release engineer', viewer:'Viewer'};
var roleNames = map[Role]string{
	RoleAdmin:   "Admin",
	RoleRelease: "Release engineer",
	RoleViewer:  "Viewer",
}

// Valid reports whether r is a known role.
func (r Role) Valid() bool {
	_, ok := rolePerms[r]
	return ok
}

// Name is the display name of the role.
func (r Role) Name() string {
	if n, ok := roleNames[r]; ok {
		return n
	}
	return string(r)
}

// Can reports whether role may perform perm. Unknown roles can do nothing.
func Can(role Role, perm Permission) bool {
	for _, p := range rolePerms[role] {
		if p == perm {
			return true
		}
	}
	return false
}

// DenyMessage is the text shown when a role lacks a permission.
//
// sample: title="Your role (${ROLE_NAME[S.role]}) can't do this"
func DenyMessage(role Role) string {
	return fmt.Sprintf("Your role (%s) can't do this", role.Name())
}
