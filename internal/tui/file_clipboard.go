package tui

// File copy-to-clipboard feature for file-select mode.
//
// When the user is in message file-select mode and presses 'c' on a
// selected (non-uploading) file, slackers will:
//   1. Validate the file is a text file (MIME or extension); binaries
//      are rejected with "download instead".
//   2. Warn if the file is > 10 MiB, prompting y/N in the status bar.
//   3. Download the file to a temporary location (whichever backend
//      owns it — Slack HTTP or P2P friend stream), read the contents,
//      copy to the system clipboard via the existing copyToClipboard
//      helper, then delete the temporary file.
//   4. Report "Copied <name> to clipboard" or an error in the
//      status bar.

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/rw3iss/slackers/internal/secure"
	slackpkg "github.com/rw3iss/slackers/internal/slack"
	"github.com/rw3iss/slackers/internal/types"
)

// copyFileSizeLimit is the threshold above which slackers prompts the
// user before loading a file into memory + the clipboard. Files
// larger than this will trigger a y/N confirmation in the status bar.
const copyFileSizeLimit int64 = 10 * 1024 * 1024 // 10 MiB

// FileCopyRequestMsg is emitted by the message-view file-select mode
// when the user presses 'c' on a non-uploading file.
type FileCopyRequestMsg struct {
	File types.FileInfo
}

// FileCopyCompleteMsg is delivered after an async copy-to-clipboard
// finishes (either successfully or with an error).
type FileCopyCompleteMsg struct {
	Name string
	Err  error
}

// textFileExts lists extensions that are always considered copyable
// text regardless of the declared MIME type. Slack sometimes reports
// "application/octet-stream" for files it can't classify, so the
// extension is the more reliable signal for these cases.
var textFileExts = map[string]struct{}{
	".txt":        {},
	".md":         {},
	".markdown":   {},
	".rst":        {},
	".log":        {},
	".csv":        {},
	".tsv":        {},
	".json":       {},
	".jsonl":      {},
	".ndjson":     {},
	".xml":        {},
	".yaml":       {},
	".yml":        {},
	".toml":       {},
	".ini":        {},
	".cfg":        {},
	".conf":       {},
	".env":        {},
	".html":       {},
	".htm":        {},
	".css":        {},
	".scss":       {},
	".sass":       {},
	".less":       {},
	".js":         {},
	".jsx":        {},
	".ts":         {},
	".tsx":        {},
	".mjs":        {},
	".cjs":        {},
	".go":         {},
	".py":         {},
	".rb":         {},
	".rs":         {},
	".java":       {},
	".kt":         {},
	".kts":        {},
	".swift":      {},
	".c":          {},
	".h":          {},
	".cpp":        {},
	".cc":         {},
	".hpp":        {},
	".cs":         {},
	".php":        {},
	".pl":         {},
	".lua":        {},
	".sh":         {},
	".bash":       {},
	".zsh":        {},
	".fish":       {},
	".ps1":        {},
	".psm1":       {},
	".bat":        {},
	".cmd":        {},
	".sql":        {},
	".graphql":    {},
	".gql":        {},
	".proto":      {},
	".dockerfile": {},
	".makefile":   {},
	".mk":         {},
	".gradle":     {},
	".tf":         {},
	".tfvars":     {},
	".hcl":        {},
	".vue":        {},
	".svelte":     {},
	".astro":      {},
	".ex":         {},
	".exs":        {},
	".elm":        {},
	".clj":        {},
	".cljs":       {},
	".edn":        {},
	".r":          {},
	".dart":       {},
	".nim":        {},
	".zig":        {},
	".srt":        {},
	".vtt":        {},
	".patch":      {},
	".diff":       {},
}

// isCopyableTextFile returns (true, "") when the given file looks
// like plaintext that's safe to copy to the clipboard, and (false,
// reason) when it's rejected. The reason is phrased for direct
// display in the status bar.
func isCopyableTextFile(f types.FileInfo) (bool, string) {
	mime := strings.ToLower(strings.TrimSpace(f.MimeType))
	// Explicit MIME hits first — these are unambiguous.
	if mime != "" {
		if strings.HasPrefix(mime, "text/") {
			return true, ""
		}
		switch mime {
		case "application/json",
			"application/ld+json",
			"application/xml",
			"application/xhtml+xml",
			"application/javascript",
			"application/ecmascript",
			"application/x-yaml",
			"application/yaml",
			"application/toml",
			"application/x-sh",
			"application/x-shellscript",
			"application/x-www-form-urlencoded",
			"application/sql":
			return true, ""
		}
		// Obvious binary MIME families → hard reject without even
		// looking at the extension.
		if strings.HasPrefix(mime, "image/") ||
			strings.HasPrefix(mime, "video/") ||
			strings.HasPrefix(mime, "audio/") ||
			strings.HasPrefix(mime, "font/") {
			return false, "binary file (" + mime + ") cannot be copied to clipboard — download instead"
		}
	}

	// Extension fallback.
	name := strings.ToLower(f.Name)
	ext := filepath.Ext(name)
	if ext == "" {
		// Files like "Dockerfile" or "Makefile" have no extension
		// but are plaintext. Allow a small allowlist.
		base := filepath.Base(name)
		switch base {
		case "dockerfile", "makefile", "rakefile", "gemfile", "procfile", "readme", "license", "notice", "authors", "contributors", "changelog", "copying":
			return true, ""
		}
	}
	if _, ok := textFileExts[ext]; ok {
		return true, ""
	}

	// If MIME was application/octet-stream and extension looks
	// binary, reject with a helpful message.
	if mime == "application/octet-stream" || mime == "" {
		return false, "file type not recognised as text — download instead"
	}
	return false, "binary file (" + mime + ") cannot be copied to clipboard — download instead"
}

// contentIsBinary sniffs the first few kilobytes of a file for NUL
// bytes, which are a reliable indicator of non-text content. This
// is the belt-and-suspenders check for files that passed the
// MIME / extension allowlist but turn out to contain binary data
// anyway (e.g. a `.txt` file that's actually a renamed ZIP).
func contentIsBinary(data []byte) bool {
	limit := len(data)
	if limit > 4096 {
		limit = 4096
	}
	return bytes.IndexByte(data[:limit], 0) != -1
}

// copyFileToClipboardCmd produces a tea.Cmd that downloads the given
// file to a temporary path, reads its contents, copies them to the
// system clipboard, deletes the temp file, and returns a
// FileCopyCompleteMsg with the outcome.
//
// The caller is responsible for validating the MIME/size up front —
// this function assumes the user has already confirmed any large-
// file prompt.
func copyFileToClipboardCmd(svc slackpkg.SlackService, p2p *secure.P2PNode, file types.FileInfo) tea.Cmd {
	return func() tea.Msg {
		tmpDir, err := os.MkdirTemp("", "slackers-clip-*")
		if err != nil {
			return FileCopyCompleteMsg{Name: file.Name, Err: fmt.Errorf("create temp dir: %w", err)}
		}
		defer os.RemoveAll(tmpDir)

		// Sanitize the filename so weird characters can't cause
		// path issues — we only need something that works as a
		// temp filename, not the user-visible display name.
		safeName := file.ID
		if safeName == "" {
			safeName = "content"
		}
		tmpPath := filepath.Join(tmpDir, safeName)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		if strings.HasPrefix(file.URL, "p2p://") {
			if p2p == nil {
				return FileCopyCompleteMsg{Name: file.Name, Err: fmt.Errorf("P2P not available")}
			}
			parts := strings.SplitN(strings.TrimPrefix(file.URL, "p2p://"), "/", 2)
			if len(parts) != 2 {
				return FileCopyCompleteMsg{Name: file.Name, Err: fmt.Errorf("malformed p2p url")}
			}
			peerUID, fileID := parts[0], parts[1]
			if err := p2p.DownloadFileFromPeer(ctx, peerUID, fileID, tmpPath); err != nil {
				return FileCopyCompleteMsg{Name: file.Name, Err: fmt.Errorf("p2p download: %w", err)}
			}
		} else {
			if svc == nil {
				return FileCopyCompleteMsg{Name: file.Name, Err: fmt.Errorf("Slack not available")}
			}
			if err := svc.DownloadFile(ctx, file.URL, tmpPath); err != nil {
				return FileCopyCompleteMsg{Name: file.Name, Err: fmt.Errorf("slack download: %w", err)}
			}
		}

		data, err := os.ReadFile(tmpPath)
		if err != nil {
			return FileCopyCompleteMsg{Name: file.Name, Err: fmt.Errorf("read temp: %w", err)}
		}
		if contentIsBinary(data) {
			return FileCopyCompleteMsg{
				Name: file.Name,
				Err:  fmt.Errorf("file contains binary data — not copied"),
			}
		}
		if !copyToClipboard(string(data)) {
			return FileCopyCompleteMsg{
				Name: file.Name,
				Err:  fmt.Errorf("clipboard tool unavailable (install xclip / xsel / wl-copy)"),
			}
		}
		return FileCopyCompleteMsg{Name: file.Name}
	}
}

// formatCopyFileSize is a small helper that returns a human-readable
// size string for the large-file confirmation prompt. Reuses the
// package-level formatFileSize if the size is known; otherwise
// returns an "unknown size" placeholder.
func formatCopyFileSize(f types.FileInfo) string {
	if f.Size <= 0 {
		return "unknown size"
	}
	return formatFileSize(f.Size)
}
