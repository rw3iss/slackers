package debug

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var (
	logger  *log.Logger
	file    *os.File
	enabled bool
	mu      sync.Mutex
)

// Init enables debug logging to the given file path.
// If path is empty, logs to ~/.config/slackers/debug.log.
func Init(path string) error {
	mu.Lock()
	defer mu.Unlock()

	if path == "" {
		base, err := os.UserConfigDir()
		if err != nil {
			base = filepath.Join(os.Getenv("HOME"), ".config")
		}
		path = filepath.Join(base, "slackers", "debug.log")
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}

	file = f
	logger = log.New(f, "", 0)
	enabled = true

	Log("--- debug session started at %s ---", time.Now().Format(time.RFC3339))
	return nil
}

// Enabled returns true if debug logging is active.
func Enabled() bool {
	return enabled
}

// Log writes a formatted message to the debug log.
func Log(format string, args ...interface{}) {
	if !enabled {
		return
	}
	ts := time.Now().Format("15:04:05.000")
	msg := fmt.Sprintf(format, args...)
	logger.Printf("%s  %s", ts, msg)
}

// Close flushes and closes the log file.
func Close() {
	mu.Lock()
	defer mu.Unlock()
	if file != nil {
		file.Close()
		file = nil
	}
	enabled = false
}
