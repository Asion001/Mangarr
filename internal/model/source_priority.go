package model

import "github.com/uptrace/bun"

// SourcePriorityList contains portable catalog keys (moduleId:sourceId).
// Scope is language:<code> or library:<rootFolderId>.
type SourcePriorityList struct {
	bun.BaseModel `bun:"table:source_priority_lists"`
	Scope         string   `bun:"scope,pk" json:"scope"`
	Sources       []string `bun:"sources,notnull" json:"sources"`
}
