// Package skillssh is a client for the skills.sh search and preview API.
package skillssh

import (
	"context"
	"net/http"
)

// DefaultBaseURL is the skills.sh API origin.
const DefaultBaseURL = "https://skills.sh"

// Client calls the skills.sh API.
type Client struct {
	BaseURL string
	HTTP    *http.Client
}

// New returns a client for DefaultBaseURL with a sensible timeout.
func New() *Client {
	panic("not implemented")
}

// Result is one search hit.
type Result struct {
	ID       string `json:"id"`
	Source   string `json:"source"`  // GitHub owner/repo
	SkillID  string `json:"skillId"` // skill name
	Name     string `json:"name"`    // display name, may differ from SkillID
	Installs int    `json:"installs"`
}

// Search returns results for query in the order skills.sh ranks them.
func (c *Client) Search(ctx context.Context, query string, limit int) ([]Result, error) {
	panic("not implemented")
}

// SkillMarkdown returns the SKILL.md of a skill from the skills.sh snapshot.
func (c *Client) SkillMarkdown(ctx context.Context, source, skillID string) (string, error) {
	panic("not implemented")
}
