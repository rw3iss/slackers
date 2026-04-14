package tui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/rw3iss/slackers/internal/debug"
)

// ClipboardImageReadyMsg is returned when a clipboard image has been
// saved to a temp file and is ready for the user to confirm sending.
type ClipboardImageReadyMsg struct {
	Path     string // temp file path
	MimeType string // e.g. "image/png"
	Size     int64  // file size in bytes
}

// ClipboardImageNoneMsg indicates no image was found in the clipboard.
type ClipboardImageNoneMsg struct{}

// ClipboardImageSentMsg signals the temp file can be cleaned up.
type ClipboardImageSentMsg struct {
	Path string
}

// probeClipboardImageCmd checks whether the system clipboard contains
// an image. If so, writes it to a temp file and returns
// ClipboardImageReadyMsg. Otherwise returns ClipboardImageNoneMsg.
//
// Supports Wayland (wl-paste), X11 (xclip), and macOS (pbpaste).
func probeClipboardImageCmd() tea.Cmd {
	return func() tea.Msg {
		// Detect session type and available tools.
		sessionType := os.Getenv("XDG_SESSION_TYPE")

		var mimeType string
		var data []byte
		var err error

		switch {
		case sessionType == "wayland" && commandExists("wl-paste"):
			mimeType, data, err = probeWayland()
		case commandExists("xclip"):
			mimeType, data, err = probeX11()
		case commandExists("pbpaste"):
			// macOS: pbpaste doesn't support images directly.
			// Could use osascript but that's complex. Skip for now.
			return ClipboardImageNoneMsg{}
		default:
			return ClipboardImageNoneMsg{}
		}

		if err != nil || len(data) == 0 {
			debug.Log("[clipboard] no image: %v", err)
			return ClipboardImageNoneMsg{}
		}

		// Determine file extension from mime type.
		ext := ".png"
		switch mimeType {
		case "image/jpeg":
			ext = ".jpg"
		case "image/gif":
			ext = ".gif"
		case "image/webp":
			ext = ".webp"
		case "image/bmp":
			ext = ".bmp"
		case "image/svg+xml":
			ext = ".svg"
		}

		// Write to temp file.
		tmpDir := os.TempDir()
		tmpFile, err := os.CreateTemp(tmpDir, "slackers-paste-*"+ext)
		if err != nil {
			debug.Log("[clipboard] temp file error: %v", err)
			return ClipboardImageNoneMsg{}
		}
		if _, err := tmpFile.Write(data); err != nil {
			tmpFile.Close()
			os.Remove(tmpFile.Name())
			return ClipboardImageNoneMsg{}
		}
		tmpFile.Close()

		info, _ := os.Stat(tmpFile.Name())
		size := int64(0)
		if info != nil {
			size = info.Size()
		}

		debug.Log("[clipboard] image saved: %s (%s, %d bytes)", tmpFile.Name(), mimeType, size)
		return ClipboardImageReadyMsg{
			Path:     tmpFile.Name(),
			MimeType: mimeType,
			Size:     size,
		}
	}
}

// probeWayland checks the Wayland clipboard for image content.
func probeWayland() (mimeType string, data []byte, err error) {
	// List available clipboard types.
	out, err := exec.Command("wl-paste", "--list-types").Output()
	if err != nil {
		return "", nil, fmt.Errorf("wl-paste --list-types: %w", err)
	}
	types := strings.Split(strings.TrimSpace(string(out)), "\n")

	// Find the best image type.
	preferred := []string{"image/png", "image/jpeg", "image/gif", "image/webp", "image/bmp"}
	for _, pref := range preferred {
		for _, t := range types {
			if strings.TrimSpace(t) == pref {
				mimeType = pref
				break
			}
		}
		if mimeType != "" {
			break
		}
	}
	if mimeType == "" {
		return "", nil, fmt.Errorf("no image type in clipboard")
	}

	// Read the image data.
	data, err = exec.Command("wl-paste", "--type", mimeType).Output()
	if err != nil {
		return "", nil, fmt.Errorf("wl-paste --type %s: %w", mimeType, err)
	}
	return mimeType, data, nil
}

// probeX11 checks the X11 clipboard for image content.
func probeX11() (mimeType string, data []byte, err error) {
	// List available targets.
	out, err := exec.Command("xclip", "-selection", "clipboard", "-t", "TARGETS", "-o").Output()
	if err != nil {
		return "", nil, fmt.Errorf("xclip targets: %w", err)
	}
	types := strings.Split(strings.TrimSpace(string(out)), "\n")

	preferred := []string{"image/png", "image/jpeg", "image/gif", "image/bmp"}
	for _, pref := range preferred {
		for _, t := range types {
			if strings.TrimSpace(t) == pref {
				mimeType = pref
				break
			}
		}
		if mimeType != "" {
			break
		}
	}
	if mimeType == "" {
		return "", nil, fmt.Errorf("no image type in clipboard")
	}

	data, err = exec.Command("xclip", "-selection", "clipboard", "-t", mimeType, "-o").Output()
	if err != nil {
		return "", nil, fmt.Errorf("xclip -t %s: %w", mimeType, err)
	}
	return mimeType, data, nil
}

// cleanupClipboardTempFile removes a temp file created by clipboard paste.
func cleanupClipboardTempFile(path string) {
	if path == "" {
		return
	}
	// Only remove files in the system temp dir with our prefix.
	base := filepath.Base(path)
	if strings.HasPrefix(base, "slackers-paste-") {
		os.Remove(path)
		debug.Log("[clipboard] cleaned up temp file: %s", path)
	}
}
