// Package changes fetches commit metadata from GitHub's public REST
// API for use in the in-app `/changes` view. Unauthenticated calls
// are rate-limited to ~60/hour per IP — fine for casual viewing.
package changes

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Commit is the trimmed-down view of a GitHub commit the UI
// renders. Only the fields the overlay actually displays are kept;
// the rest of the API payload is discarded at decode time so the
// in-memory list stays small.
type Commit struct {
	SHA     string    // full 40-char hash; the UI shows the first 7
	Author  string    // commit author's display name
	Date    time.Time // commit author date
	Message string    // raw commit message body (subject + body)
	HTMLURL string    // canonical web URL — used by "copy link"
}

// Subject returns the first line of the commit message.
func (c Commit) Subject() string {
	for i := 0; i < len(c.Message); i++ {
		if c.Message[i] == '\n' {
			return c.Message[:i]
		}
	}
	return c.Message
}

// ShortSHA returns the first 7 hex chars of the commit hash, the
// conventional GitHub short form. Returns "" when SHA is unset.
func (c Commit) ShortSHA() string {
	if len(c.SHA) < 7 {
		return c.SHA
	}
	return c.SHA[:7]
}

// ghCommitResp matches the slice of objects GitHub's
// /repos/:owner/:repo/commits endpoint returns. We decode only the
// fields we care about; unknown fields are ignored.
type ghCommitResp struct {
	SHA     string `json:"sha"`
	HTMLURL string `json:"html_url"`
	Commit  struct {
		Author struct {
			Name string    `json:"name"`
			Date time.Time `json:"date"`
		} `json:"author"`
		Message string `json:"message"`
	} `json:"commit"`
}

// httpClient wraps the default client with a 10 s timeout so a
// stalled GitHub request can't hang the UI thread indefinitely.
var httpClient = &http.Client{Timeout: 10 * time.Second}

// Fetch returns up to perPage commits from the given public repo.
// Pages are 1-indexed (GitHub's convention). perPage is clamped
// to GitHub's [1, 100] range. The returned slice is in GitHub's
// default order — newest first.
func Fetch(owner, repo string, page, perPage int) ([]Commit, error) {
	if perPage < 1 {
		perPage = 1
	}
	if perPage > 100 {
		perPage = 100
	}
	if page < 1 {
		page = 1
	}
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/commits?page=%d&per_page=%d",
		owner, repo, page, perPage)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("github fetch: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("github api returned %d: %s", resp.StatusCode, string(body))
	}
	var raw []ghCommitResp
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("github decode: %w", err)
	}
	out := make([]Commit, 0, len(raw))
	for _, r := range raw {
		out = append(out, Commit{
			SHA:     r.SHA,
			Author:  r.Commit.Author.Name,
			Date:    r.Commit.Author.Date,
			Message: r.Commit.Message,
			HTMLURL: r.HTMLURL,
		})
	}
	return out, nil
}
