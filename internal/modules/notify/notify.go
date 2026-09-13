// Package notify defines notification modules (Telegram, Discord, ntfy, ...).
package notify

import (
	"context"

	"github.com/Asion001/mangarr/internal/modules"
)

type Message struct {
	Event    string   `json:"event"`
	Title    string   `json:"title"`
	Body     string   `json:"body"`
	Items    []string `json:"items,omitempty"`
	URL      string   `json:"url,omitempty"`
	ImageURL string   `json:"imageUrl,omitempty"`
	SeriesID int64    `json:"seriesId,omitempty"`
	// Series title for providers that format it separately.
	Series string `json:"series,omitempty"`
}

// Text renders a plain text version of the message.
func (m Message) Text() string {
	s := m.Body
	for _, it := range m.Items {
		s += "\n• " + it
	}
	return s
}

type Module interface {
	modules.Instance
	Send(ctx context.Context, msg Message) error
}
