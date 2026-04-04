package tui

import (
	"fmt"
	"os"
)

// sendNotification emits terminal escape sequences to alert the user of new messages.
// It uses BEL (works everywhere) and OSC 9 (desktop notification on modern terminals).
func sendNotification(channelName string, messageCount int) {
	tty, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0)
	if err != nil {
		return
	}
	defer tty.Close()

	// BEL — triggers terminal bell (audible or visual depending on user's settings).
	fmt.Fprint(tty, "\a")

	// OSC 9 — desktop notification (supported by iTerm2, kitty, WezTerm, foot, others).
	// Silently ignored by terminals that don't support it.
	title := fmt.Sprintf("Slackers: %d new message(s) in %s", messageCount, channelName)
	fmt.Fprintf(tty, "\033]9;%s\a", title)

	// OSC 777 — urgency notification (supported by rxvt-unicode, some others).
	fmt.Fprintf(tty, "\033]777;notify;Slackers;%s\a", title)
}

// setWindowUrgent sets the terminal urgency hint so the taskbar/tab flashes.
func setWindowUrgent() {
	tty, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0)
	if err != nil {
		return
	}
	defer tty.Close()

	// xterm/VTE urgency hint — makes the window/tab flash in most desktop environments.
	fmt.Fprint(tty, "\033]0;* Slackers - new messages\a")
}

// clearWindowUrgent resets the terminal title.
func clearWindowUrgent() {
	tty, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0)
	if err != nil {
		return
	}
	defer tty.Close()

	fmt.Fprint(tty, "\033]0;Slackers\a")
}
