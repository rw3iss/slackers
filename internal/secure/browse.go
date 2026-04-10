package secure

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// BrowseEntry is one file or directory in a shared folder listing.
type BrowseEntry struct {
	Name    string    `json:"name"`
	Size    int64     `json:"size"`
	IsDir   bool      `json:"is_dir"`
	ModTime time.Time `json:"mod_time"`
}

// BrowseResponse is the payload returned by a MsgTypeBrowseResponse
// message. Carried in the P2PMessage.Text field as JSON.
type BrowseResponse struct {
	Path    string        `json:"path"`    // relative path that was listed
	Entries []BrowseEntry `json:"entries"` // files + dirs in the listed path
	Error   string        `json:"error,omitempty"`
}

// ValidateSharedPath checks that requestedRelPath resolves to a
// location within sharedRoot. Returns the cleaned absolute path
// or an error. Used by both the browse handler and the
// file-by-path download handler to prevent path traversal attacks.
func ValidateSharedPath(sharedRoot, requestedRelPath string) (string, error) {
	if sharedRoot == "" {
		return "", fmt.Errorf("no shared folder configured")
	}
	cleaned := filepath.Clean(requestedRelPath)
	// Reject any path component that is literally ".." after cleaning.
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") || strings.Contains(cleaned, "/../") {
		return "", fmt.Errorf("path escapes shared folder")
	}
	abs := filepath.Join(sharedRoot, cleaned)
	abs = filepath.Clean(abs)
	// Double-check via Rel: the result must not start with "..".
	rel, err := filepath.Rel(sharedRoot, abs)
	if err != nil {
		return "", fmt.Errorf("invalid path")
	}
	if strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("path escapes shared folder")
	}
	return abs, nil
}
