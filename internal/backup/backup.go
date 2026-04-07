// Package backup provides import/export of the entire slackers user
// configuration directory as a single zip archive.
//
// The exported archive contains every file under the user's
// $XDG_CONFIG_HOME/slackers directory: config.json, themes/, friends data,
// chat history, secure key, etc. Tokens are stored as-is — treat the
// archive like any sensitive credential file.
package backup

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rw3iss/slackers/internal/config"
)

// MergeMode controls how an import should resolve collisions with existing
// data on disk.
type MergeMode int

const (
	// MergeReplace deletes the destination's contents (or per-file overrides
	// where merging makes no sense) and unpacks the archive on top.
	MergeReplace MergeMode = iota
	// MergeUnion keeps existing data and adds/updates entries from the
	// archive: friends are unioned by user ID, chat histories merged by
	// message ID, emoji favorites unioned, themes added (or replaced when
	// the names collide), but the main config.json is overlaid (any field
	// set in the imported config wins, any field unset stays from local).
	MergeUnion
)

// DefaultExportDir returns the directory where exports are written.
// Defaults to ~/Downloads, falling back to the user home directory.
func DefaultExportDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return os.TempDir()
	}
	dl := filepath.Join(home, "Downloads")
	if info, err := os.Stat(dl); err == nil && info.IsDir() {
		return dl
	}
	return home
}

// DefaultExportName returns a timestamped filename for a new export.
func DefaultExportName() string {
	return fmt.Sprintf("slackers-export-%s.zip", time.Now().Format("20060102-150405"))
}

// Export writes the entire slackers config directory to a zip file at
// destPath. Returns the absolute destination path on success.
func Export(destPath string) (string, error) {
	srcDir := config.DefaultConfigDir()
	if _, err := os.Stat(srcDir); err != nil {
		return "", fmt.Errorf("config dir not found: %w", err)
	}

	// Ensure parent dir exists.
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return "", fmt.Errorf("creating export dir: %w", err)
	}

	out, err := os.Create(destPath)
	if err != nil {
		return "", fmt.Errorf("creating export file: %w", err)
	}
	defer out.Close()

	zw := zip.NewWriter(out)
	defer zw.Close()

	err = filepath.Walk(srcDir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		// Use forward slashes inside the zip for portability.
		rel = filepath.ToSlash(rel)
		hdr, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		hdr.Name = rel
		hdr.Method = zip.Deflate
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			return err
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(w, f)
		return err
	})
	if err != nil {
		return "", fmt.Errorf("packing export: %w", err)
	}

	abs, _ := filepath.Abs(destPath)
	return abs, nil
}

// Import unpacks the archive at srcPath into the user's slackers config
// directory using the requested merge mode.
func Import(srcPath string, mode MergeMode) error {
	if _, err := os.Stat(srcPath); err != nil {
		return fmt.Errorf("archive not found: %w", err)
	}
	zr, err := zip.OpenReader(srcPath)
	if err != nil {
		return fmt.Errorf("opening archive: %w", err)
	}
	defer zr.Close()

	dstDir := config.DefaultConfigDir()
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		return fmt.Errorf("creating config dir: %w", err)
	}

	// In replace mode, wipe the directory first (but keep the directory
	// itself so any process holding a handle survives).
	if mode == MergeReplace {
		if err := wipeContents(dstDir); err != nil {
			return fmt.Errorf("wiping existing config: %w", err)
		}
	}

	// Process each archive entry.
	for _, f := range zr.File {
		if err := importEntry(f, dstDir, mode); err != nil {
			return fmt.Errorf("importing %s: %w", f.Name, err)
		}
	}
	return nil
}

// importEntry handles one zip entry. For mode-specific paths (config.json,
// friends data, history, themes, emoji favorites), the entry is dispatched
// to the right merge function. Everything else is written verbatim.
func importEntry(f *zip.File, dstDir string, mode MergeMode) error {
	rel := filepath.FromSlash(f.Name)
	if strings.Contains(rel, "..") {
		return errors.New("archive contains an unsafe path")
	}
	if f.FileInfo().IsDir() {
		return os.MkdirAll(filepath.Join(dstDir, rel), 0o755)
	}

	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		return err
	}

	dstPath := filepath.Join(dstDir, rel)
	if err := os.MkdirAll(filepath.Dir(dstPath), 0o755); err != nil {
		return err
	}

	// Replace mode just writes everything verbatim (the dst was already wiped).
	if mode == MergeReplace {
		return os.WriteFile(dstPath, data, 0o644)
	}

	// Union mode: merge by file type.
	switch {
	case rel == "config.json":
		return mergeJSONOverlay(dstPath, data)
	case rel == "friends.json":
		return mergeFriendsJSON(dstPath, data)
	case strings.HasPrefix(rel, "friend_history/"):
		// Per-peer history file. Try to merge by message ID.
		return mergeHistoryJSON(dstPath, data)
	case strings.HasPrefix(rel, "themes/"):
		// User themes overwrite by name (the file path IS the identity).
		return os.WriteFile(dstPath, data, 0o644)
	default:
		// Anything else (key files, etc): only write if missing in union mode
		// to avoid clobbering local credentials.
		if _, err := os.Stat(dstPath); errors.Is(err, os.ErrNotExist) {
			return os.WriteFile(dstPath, data, 0o644)
		}
		return nil
	}
}

// wipeContents removes all entries inside dir (but keeps dir itself).
func wipeContents(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		p := filepath.Join(dir, e.Name())
		if err := os.RemoveAll(p); err != nil {
			return err
		}
	}
	return nil
}

// mergeJSONOverlay reads the existing JSON object at dstPath and overlays
// any keys present in `incoming` on top, then writes the result back. Used
// for config.json so user-only settings (tokens, paths) are preserved when
// the imported config doesn't define them.
func mergeJSONOverlay(dstPath string, incoming []byte) error {
	var dst, src map[string]any
	if data, err := os.ReadFile(dstPath); err == nil {
		_ = json.Unmarshal(data, &dst)
	}
	if dst == nil {
		dst = map[string]any{}
	}
	if err := json.Unmarshal(incoming, &src); err != nil {
		return err
	}
	for k, v := range src {
		// Merge emoji_favorites and similar list fields by union.
		if k == "emoji_favorites" {
			dst[k] = mergeStringSet(dst[k], v)
			continue
		}
		dst[k] = v
	}
	out, err := json.MarshalIndent(dst, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(dstPath, out, 0o644)
}

// mergeFriendsJSON unions two friend lists by user_id. Existing entries are
// preserved; new entries are appended.
func mergeFriendsJSON(dstPath string, incoming []byte) error {
	type friendRecord = map[string]any
	var dstList, srcList []friendRecord

	if data, err := os.ReadFile(dstPath); err == nil {
		_ = json.Unmarshal(data, &dstList)
	}
	if err := json.Unmarshal(incoming, &srcList); err != nil {
		// If the existing format is an object, just write the incoming as-is.
		return os.WriteFile(dstPath, incoming, 0o644)
	}

	// Index existing friends by user_id.
	idx := map[string]int{}
	for i, f := range dstList {
		if uid, ok := f["user_id"].(string); ok {
			idx[uid] = i
		}
	}
	for _, f := range srcList {
		uid, _ := f["user_id"].(string)
		if uid == "" {
			dstList = append(dstList, f)
			continue
		}
		if i, ok := idx[uid]; ok {
			// Existing friend: keep, but fill in any blank fields from incoming.
			for k, v := range f {
				if _, exists := dstList[i][k]; !exists || dstList[i][k] == "" || dstList[i][k] == nil {
					dstList[i][k] = v
				}
			}
		} else {
			idx[uid] = len(dstList)
			dstList = append(dstList, f)
		}
	}

	out, err := json.MarshalIndent(dstList, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(dstPath, out, 0o644)
}

// mergeHistoryJSON unions two chat-history JSON files by message_id. Both
// the inbound and existing files are encrypted blobs in the live app, but
// in the simple case (decrypted JSON arrays of message objects) we can do
// a basic union. If the formats don't match what we expect, we keep the
// existing file untouched to avoid losing data.
func mergeHistoryJSON(dstPath string, incoming []byte) error {
	var dstList, srcList []map[string]any
	if data, err := os.ReadFile(dstPath); err == nil {
		if err := json.Unmarshal(data, &dstList); err != nil {
			// Existing file isn't a plain JSON array (likely encrypted).
			// Skip merging and leave it as-is to avoid corrupting it.
			return nil
		}
	}
	if err := json.Unmarshal(incoming, &srcList); err != nil {
		// Same caveat for the incoming side.
		return nil
	}

	idx := map[string]int{}
	for i, m := range dstList {
		if id, ok := m["message_id"].(string); ok {
			idx[id] = i
		}
	}
	for _, m := range srcList {
		id, _ := m["message_id"].(string)
		if id == "" {
			dstList = append(dstList, m)
			continue
		}
		if _, ok := idx[id]; !ok {
			idx[id] = len(dstList)
			dstList = append(dstList, m)
		}
	}

	out, err := json.MarshalIndent(dstList, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(dstPath, out, 0o644)
}

// mergeStringSet unions two values that are []string-shaped under JSON.
func mergeStringSet(dst, src any) any {
	toSlice := func(v any) []string {
		if v == nil {
			return nil
		}
		arr, ok := v.([]any)
		if !ok {
			return nil
		}
		out := make([]string, 0, len(arr))
		for _, e := range arr {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	a := toSlice(dst)
	b := toSlice(src)
	seen := map[string]bool{}
	merged := make([]any, 0, len(a)+len(b))
	for _, s := range a {
		if !seen[s] {
			seen[s] = true
			merged = append(merged, s)
		}
	}
	for _, s := range b {
		if !seen[s] {
			seen[s] = true
			merged = append(merged, s)
		}
	}
	return merged
}
