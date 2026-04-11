package downloads

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Status represents the state of a download.
type Status int

const (
	StatusDownloading Status = iota
	StatusCompleted
	StatusFailed
	StatusCancelled
)

func (s Status) String() string {
	switch s {
	case StatusDownloading:
		return "downloading"
	case StatusCompleted:
		return "completed"
	case StatusFailed:
		return "failed"
	case StatusCancelled:
		return "cancelled"
	}
	return "unknown"
}

// Download tracks a single file download.
type Download struct {
	ID          string
	FileName    string
	DestPath    string
	URL         string    // for HTTP downloads
	PeerUID     string    // for P2P downloads
	PeerName    string    // display name of the peer
	Size        int64     // total size in bytes (0 if unknown)
	Downloaded  int64     // bytes downloaded so far
	Status      Status
	Error       string    // error message if failed
	StartedAt   time.Time
	CompletedAt time.Time
	cancel      context.CancelFunc
}

// Progress returns download progress as a float 0.0-1.0.
func (d *Download) Progress() float64 {
	if d.Size <= 0 {
		return 0
	}
	p := float64(d.Downloaded) / float64(d.Size)
	if p > 1 {
		p = 1
	}
	return p
}

// Manager tracks all downloads and manages concurrency.
type Manager struct {
	mu          sync.RWMutex
	downloads   []*Download
	maxActive   int
	nextID      int
	downloadDir string
}

// NewManager creates a download manager.
func NewManager(downloadDir string, maxActive int) *Manager {
	if maxActive <= 0 {
		maxActive = 5
	}
	return &Manager{
		downloadDir: downloadDir,
		maxActive:   maxActive,
	}
}

// Add creates a new download entry and returns its ID.
// The download doesn't start until Start is called.
func (m *Manager) Add(fileName, destPath, url, peerUID, peerName string, size int64) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextID++
	id := fmt.Sprintf("dl-%d", m.nextID)
	dl := &Download{
		ID:        id,
		FileName:  fileName,
		DestPath:  destPath,
		URL:       url,
		PeerUID:   peerUID,
		PeerName:  peerName,
		Size:      size,
		Status:    StatusDownloading,
		StartedAt: time.Now(),
	}
	m.downloads = append(m.downloads, dl)
	return id
}

// Get returns a download by ID.
func (m *Manager) Get(id string) *Download {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, dl := range m.downloads {
		if dl.ID == id {
			return dl
		}
	}
	return nil
}

// Cancel cancels an active download.
func (m *Manager) Cancel(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, dl := range m.downloads {
		if dl.ID == id && dl.Status == StatusDownloading {
			if dl.cancel != nil {
				dl.cancel()
			}
			dl.Status = StatusCancelled
			dl.Error = "cancelled by user"
			dl.CompletedAt = time.Now()
		}
	}
}

// Complete marks a download as completed.
func (m *Manager) Complete(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, dl := range m.downloads {
		if dl.ID == id {
			dl.Status = StatusCompleted
			dl.CompletedAt = time.Now()
			if dl.Size == 0 {
				// Try to get actual size from file.
				if info, err := os.Stat(dl.DestPath); err == nil {
					dl.Size = info.Size()
					dl.Downloaded = dl.Size
				}
			}
		}
	}
}

// Fail marks a download as failed with an error.
func (m *Manager) Fail(id string, errMsg string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, dl := range m.downloads {
		if dl.ID == id {
			dl.Status = StatusFailed
			dl.Error = errMsg
			dl.CompletedAt = time.Now()
		}
	}
}

// UpdateProgress updates the bytes downloaded for a download.
func (m *Manager) UpdateProgress(id string, downloaded int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, dl := range m.downloads {
		if dl.ID == id {
			dl.Downloaded = downloaded
		}
	}
}

// SetCancel stores the cancel function for an active download.
func (m *Manager) SetCancel(id string, cancel context.CancelFunc) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, dl := range m.downloads {
		if dl.ID == id {
			dl.cancel = cancel
		}
	}
}

// Active returns all active downloads.
func (m *Manager) Active() []*Download {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []*Download
	for _, dl := range m.downloads {
		if dl.Status == StatusDownloading {
			out = append(out, dl)
		}
	}
	return out
}

// ActiveCount returns the number of active downloads.
func (m *Manager) ActiveCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	count := 0
	for _, dl := range m.downloads {
		if dl.Status == StatusDownloading {
			count++
		}
	}
	return count
}

// Failed returns all failed/cancelled downloads.
func (m *Manager) Failed() []*Download {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []*Download
	for _, dl := range m.downloads {
		if dl.Status == StatusFailed || dl.Status == StatusCancelled {
			out = append(out, dl)
		}
	}
	return out
}

// Completed returns all completed downloads, most recent first.
func (m *Manager) Completed() []*Download {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []*Download
	for _, dl := range m.downloads {
		if dl.Status == StatusCompleted {
			out = append(out, dl)
		}
	}
	// Most recent first.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// All returns all downloads.
func (m *Manager) All() []*Download {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Download, len(m.downloads))
	copy(out, m.downloads)
	return out
}

// CanStartMore returns true if we haven't hit the concurrent limit.
func (m *Manager) CanStartMore() bool {
	return m.ActiveCount() < m.maxActive
}

// Remove deletes a download from the list (for cleanup).
func (m *Manager) Remove(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, dl := range m.downloads {
		if dl.ID == id {
			m.downloads = append(m.downloads[:i], m.downloads[i+1:]...)
			return
		}
	}
}

// DownloadDir returns the configured download directory.
func (m *Manager) DownloadDir() string {
	return m.downloadDir
}

// DownloadHTTP downloads a file from a URL with progress tracking.
func (m *Manager) DownloadHTTP(id, url, destPath string) error {
	ctx, cancel := context.WithCancel(context.Background())
	m.SetCancel(id, cancel)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 0} // no timeout — large files
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	dir := filepath.Dir(destPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	out, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer out.Close()

	// Track progress.
	buf := make([]byte, 32*1024)
	var total int64
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, writeErr := out.Write(buf[:n]); writeErr != nil {
				return writeErr
			}
			total += int64(n)
			m.UpdateProgress(id, total)
		}
		if readErr != nil {
			if readErr == io.EOF {
				break
			}
			return readErr
		}
	}
	return nil
}

// FormatSize returns a human-readable size string.
func FormatSize(size int64) string {
	const (
		_  = iota
		kB int64 = 1 << (10 * iota)
		mB
		gB
	)
	switch {
	case size >= gB:
		return fmt.Sprintf("%.1f GB", float64(size)/float64(gB))
	case size >= mB:
		return fmt.Sprintf("%.1f MB", float64(size)/float64(mB))
	case size >= kB:
		return fmt.Sprintf("%.1f KB", float64(size)/float64(kB))
	default:
		return fmt.Sprintf("%d B", size)
	}
}
