package team

import "time"

// RAMRoleMapping is the versioned many-to-many authorization mapping for one
// functional Team Role. RAM roles own permissions; RoleConfig does not.
type RAMRoleMapping struct {
	TeamID     TeamID    `json:"team_id"`
	TeamRole   string    `json:"team_role"`
	RAMRoleIDs []string  `json:"ram_role_ids"`
	Version    int       `json:"version"`
	UpdatedAt  time.Time `json:"updated_at,omitempty"`
	UpdatedBy  string    `json:"updated_by,omitempty"`
}

// RAMRoleMappingImpact describes the immediate blast radius of replacing a
// mapping. MemberCount includes every membership currently using TeamRole;
// ProjectIDs are the scopes derived from this team's project associations.
type RAMRoleMappingImpact struct {
	TeamID         TeamID   `json:"team_id"`
	TeamRole       string   `json:"team_role"`
	CurrentRoleIDs []string `json:"current_ram_role_ids"`
	NextRoleIDs    []string `json:"next_ram_role_ids"`
	AddedRoleIDs   []string `json:"added_ram_role_ids"`
	RemovedRoleIDs []string `json:"removed_ram_role_ids"`
	MemberCount    int      `json:"affected_members"`
	ProjectIDs     []string `json:"affected_project_ids"`
	Version        int      `json:"version"`
}
