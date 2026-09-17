package model

import (
	"time"

	"github.com/uptrace/bun"
)

// UIPreferences belong to the account, independently of its reader settings.
type UIPreferences struct {
	bun.BaseModel `bun:"table:user_ui_preferences"`
	UserID        int64     `bun:"user_id,pk" json:"-"`
	Locale        string    `bun:"locale,notnull" json:"locale" enum:"auto,en,ru,uk"`
	Mode          string    `bun:"mode,notnull" json:"mode" enum:"reading,editing"`
	UpdatedAt     time.Time `bun:"updated_at,notnull" json:"updatedAt"`
}
