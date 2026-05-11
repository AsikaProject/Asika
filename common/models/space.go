package models

import "time"

// TeamSpace represents an isolated team space with its own repos and members.
type TeamSpace struct {
	Name        string        `json:"name"`
	Description string        `json:"description"`
	CreatedAt   time.Time     `json:"created_at"`
	CreatedBy   string        `json:"created_by"`
	Members     []SpaceMember `json:"members,omitempty"`
	RepoGroups  []string      `json:"repo_groups"` // repo group names belonging to this space
}

// SpaceMember represents a member of a team space.
type SpaceMember struct {
	Username string    `json:"username"`
	Role     string    `json:"role"` // "space_admin" | "space_operator" | "space_viewer"
	JoinedAt time.Time `json:"joined_at"`
}
