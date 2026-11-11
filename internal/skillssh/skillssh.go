// Package skillssh is a client for the skills.sh search and preview API.
package skillssh

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/legibet/myskills/internal/source"
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
	return &Client{BaseURL: DefaultBaseURL, HTTP: &http.Client{Timeout: 15 * time.Second}}
}

// Result is one search hit.
type Result struct {
	Source   string `json:"source"`  // GitHub owner/repo
	SkillID  string `json:"skillId"` // skill name
	Name     string `json:"name"`    // display name, may differ from SkillID
	Installs int    `json:"installs"`
}

// repoRe matches a GitHub owner/repo.
var repoRe = regexp.MustCompile(`^[A-Za-z0-9._-]+/[A-Za-z0-9._-]+$`)

// Search returns results for query in the order skills.sh ranks them. The
// frontends add a result through git from what it says, so a result that
// doesn't name a skill in a GitHub repository is dropped: skills.sh is not
// trusted, and must not choose another kind of source.
func (c *Client) Search(ctx context.Context, query string, limit int) ([]Result, error) {
	q := url.Values{"q": {query}, "limit": {strconv.Itoa(limit)}}
	var body struct {
		Skills []Result `json:"skills"`
	}
	if err := c.get(ctx, "/api/search?"+q.Encode(), &body); err != nil {
		return nil, fmt.Errorf("skills.sh search: %w", err)
	}
	return slices.DeleteFunc(body.Skills, func(r Result) bool {
		return !repoRe.MatchString(r.Source) || r.SkillID == ""
	}), nil
}

// Spec returns the source that adds the result (docs/library.md): its GitHub
// repository, restricted to the skill.
func (r Result) Spec() source.Spec {
	return source.Spec{
		Kind:   source.Git,
		Input:  r.Source + "@" + r.SkillID,
		URL:    "https://github.com/" + r.Source + ".git",
		Skills: []string{r.SkillID},
	}
}

// SkillMarkdown returns the SKILL.md of a skill from the skills.sh snapshot.
func (c *Client) SkillMarkdown(ctx context.Context, source, skillID string) (string, error) {
	owner, repo, ok := strings.Cut(source, "/")
	if !ok || owner == "" || repo == "" || strings.Contains(repo, "/") || skillID == "" {
		return "", fmt.Errorf("invalid skills.sh skill %q", source+"@"+skillID)
	}
	var body struct {
		Files []struct {
			Path     string `json:"path"`
			Contents string `json:"contents"`
		} `json:"files"`
	}
	p := "/api/download/" + url.PathEscape(owner) + "/" + url.PathEscape(repo) + "/" + url.PathEscape(skillID)
	if err := c.get(ctx, p, &body); err != nil {
		if se, ok := errors.AsType[*statusError](err); ok && se.code == http.StatusNotFound {
			return "", errors.New("skill not available on skills.sh")
		}
		return "", fmt.Errorf("skills.sh download: %w", err)
	}
	found := -1
	for i, f := range body.Files {
		if f.Path == "SKILL.md" {
			return f.Contents, nil
		}
		if found < 0 && strings.HasSuffix(f.Path, "/SKILL.md") {
			found = i
		}
	}
	if found < 0 {
		return "", fmt.Errorf("skills.sh has no SKILL.md for %s@%s", source, skillID)
	}
	return body.Files[found].Contents, nil
}

// statusError is a non-2xx HTTP response.
type statusError struct {
	code   int
	status string
}

func (e *statusError) Error() string { return e.status }

// get fetches path (with query) from the API and decodes the JSON response
// into v.
func (c *Client) get(ctx context.Context, path string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(c.BaseURL, "/")+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "myskills")
	req.Header.Set("Accept", "application/json")
	client := c.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return &statusError{code: resp.StatusCode, status: resp.Status}
	}
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}
