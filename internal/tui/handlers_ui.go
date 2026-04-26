package tui

// This file holds the UI-layer mouse / focus / resize handlers that
// used to live in model.go. They were extracted as part of the
// Phase C "split model.go" pass — see docs/phase-c-plan-2026-04-08.md.
//
// These methods all operate on *Model but are concerned purely with
// how the user interacts with the chrome (resize, focus, clicks) as
// opposed to domain logic (Slack / friend state). Keeping them in a
// sibling file lets model.go focus on Init/Update/View and the
// cross-cutting state transitions.

import (
	"fmt"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/rw3iss/slackers/internal/config"
	"github.com/rw3iss/slackers/internal/debug"
	"github.com/rw3iss/slackers/internal/types"
)

func (m *Model) applySettings() {
	m.cfg = m.settings.Config()
	if m.cfg.ReplyFormat != "" {
		m.messages.SetReplyFormat(m.cfg.ReplyFormat)
	}
	sortAsc := true
	if m.cfg.ChannelSortAsc != nil {
		sortAsc = *m.cfg.ChannelSortAsc
	}
	sortBy := m.cfg.ChannelSortBy
	if sortBy == "" {
		sortBy = SortByType
	}
	m.channels.SetSort(sortBy, sortAsc)
	m.channels.SetItemSpacing(m.cfg.SidebarItemSpacing)
	m.messages.SetItemSpacing(m.cfg.MessageItemSpacing)
	m.resizeComponents()
}

// resizeComponents calculates and sets sizes for all sub-models.
func (m *Model) resizeComponents() {
	sidebarWidth := m.cfg.SidebarWidth
	if sidebarWidth < 10 {
		sidebarWidth = 10
	}
	if sidebarWidth > m.width/2 {
		sidebarWidth = m.width / 2
	}

	// In full mode, hide sidebar unless it's focused.
	showSidebar := true
	if m.fullMode && m.focus != types.FocusSidebar {
		showSidebar = false
	}

	inputHeight := m.input.DisplayHeight()
	statusHeight := 1
	topHeight := m.height - inputHeight - statusHeight - 2

	var msgWidth int
	if showSidebar {
		msgWidth = m.width - sidebarWidth - 2
	} else {
		sidebarWidth = 0
		msgWidth = m.width - 2
	}

	if topHeight < 1 {
		topHeight = 1
	}
	if msgWidth < 1 {
		msgWidth = 1
	}

	m.sidebarWidth = sidebarWidth
	m.msgTop = 0
	m.inputTop = topHeight + 2 // after sidebar/messages + borders

	m.channels.SetSize(sidebarWidth, topHeight)
	m.messages.SetSize(msgWidth, topHeight)
	// Keep the output pane sized to the same slot as the messages
	// pane so toggling between them is seamless after a window
	// resize.
	m.outputView.SetSize(msgWidth, topHeight)
	m.input.SetSize(m.width - 2)
}

// handleOverlayMouse delegates mouse events to the active overlay.
func (m Model) handleOverlayMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	switch m.overlay {
	case overlayHelp:
		var cmd tea.Cmd
		m.help, cmd = m.help.Update(msg)
		return m, cmd
	case overlaySettings:
		var cmd tea.Cmd
		m.settings, cmd = m.settings.Update(msg)
		return m, cmd
	case overlaySearch:
		var cmd tea.Cmd
		m.search, cmd = m.search.Update(msg)
		return m, cmd
	case overlayHidden:
		var cmd tea.Cmd
		m.hidden, cmd = m.hidden.Update(msg)
		return m, cmd
	case overlayShortcuts:
		var cmd tea.Cmd
		m.shortcutsEditor, cmd = m.shortcutsEditor.Update(msg)
		return m, cmd
	case overlayWhitelist:
		var cmd tea.Cmd
		m.whitelist, cmd = m.whitelist.Update(msg)
		return m, cmd
	case overlayFriendsConfig:
		var cmd tea.Cmd
		m.friendsConfig, cmd = m.friendsConfig.Update(msg)
		return m, cmd
	case overlayNotifications:
		var cmd tea.Cmd
		m.notifs, cmd = m.notifs.Update(msg)
		return m, cmd
	case overlayEmojiPicker:
		var cmd tea.Cmd
		m.emojiPicker, cmd = m.emojiPicker.Update(msg)
		return m, cmd
	case overlayFileBrowser:
		var cmd tea.Cmd
		m.fileBrowser, cmd = m.fileBrowser.UpdateMouse(msg)
		return m, cmd
	case overlayMsgOptions:
		// Click outside the popup box → close the overlay.
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
			if !m.msgOptions.ClickInside(msg.X, msg.Y) {
				m.overlay = overlayNone
				return m, nil
			}
		}
		var cmd tea.Cmd
		m.msgOptions, cmd = m.msgOptions.Update(msg)
		return m, cmd
	case overlaySidebarOptions:
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
			if !m.sidebarOptions.ClickInside(msg.X, msg.Y) {
				m.overlay = overlayNone
				return m, nil
			}
		}
		var cmd tea.Cmd
		m.sidebarOptions, cmd = m.sidebarOptions.Update(msg)
		return m, cmd
	case overlayFriendCardOptions:
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
			if !m.friendCardOpts.ClickInside(msg.X, msg.Y) {
				m.overlay = overlayNone
				return m, nil
			}
		}
		var cmd tea.Cmd
		m.friendCardOpts, cmd = m.friendCardOpts.Update(msg)
		return m, cmd
	case overlayChatOptions:
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
			if !m.chatOptions.ClickInside(msg.X, msg.Y) {
				m.overlay = overlayNone
				return m, nil
			}
		}
		var cmd tea.Cmd
		m.chatOptions, cmd = m.chatOptions.Update(msg)
		return m, cmd
	}
	return m, nil
}

// handleMouse processes mouse click and scroll events.
func (m Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	x, y := msg.X, msg.Y
	// Recalculate inputTop dynamically — it changes when the input
	// textarea grows/shrinks with multi-line content.
	m.inputTop = m.height - m.input.DisplayHeight() - 1
	// Log every right-click that reaches the messages-pane handler so
	// we can prove the build under test contains the friend-pill
	// detection code (and tell the difference between "code not in
	// build" and "code ran but missed").
	if msg.Button == tea.MouseButtonRight && msg.Action == tea.MouseActionPress {
		debug.Log("[handle-mouse] right-click press at (%d,%d) overlay=%d", x, y, m.overlay)
	}

	// Handle drag for sidebar resize.
	if m.dragging {
		switch msg.Action {
		case tea.MouseActionMotion:
			newWidth := x
			if newWidth < 10 {
				newWidth = 10
			}
			maxWidth := m.width / 2
			if newWidth > maxWidth {
				newWidth = maxWidth
			}
			m.cfg.SidebarWidth = newWidth
			m.resizeComponents()
			return m, nil
		case tea.MouseActionRelease:
			m.dragging = false
			config.SaveDebounced(m.cfg)
			return m, nil
		}
		return m, nil
	}

	switch msg.Action {
	case tea.MouseActionPress:
		// Right-click on messages area opens the message options
		// menu; right-click on the sidebar opens a channel context
		// menu with Hide / Rename / Invite / View Contact Info.
		if msg.Button == tea.MouseButtonRight {
			// Messages pane right-click. The priority order is
			// item-first, message-fallback:
			//
			//   1. Friend-card pill → opens its own context menu
			//      (Add Friend / View / Copy). The hit-test is the
			//      precise pill rectangle, not the whole line.
			//   2. Reaction badge → no item-specific menu yet, but
			//      the click is *consumed* so the parent message
			//      menu doesn't pop over the badge. Right-click on
			//      a reaction is therefore a no-op for now.
			//   3. File row → same as reactions: consumed, no menu.
			//   4. Anywhere else inside a message → falls through
			//      to MessageAtClick and shows the parent message
			//      options menu (React / Reply / Edit / Delete).
			//
			// Without this prioritisation, right-clicking the
			// friend pill (which sits inside the message body)
			// always raised the parent message menu instead of
			// the friend menu, because the message-body line was
			// matched first.
			if y < m.inputTop && x >= m.sidebarWidth+1 {
				msgPaneX := x - m.sidebarWidth - 2
				debug.Log("[right-click] screen=(%d,%d) sidebar=%d msgPaneX=%d inputTop=%d",
					x, y, m.sidebarWidth, msgPaneX, m.inputTop)

				// 1. Friend card pill.
				if card := m.messages.FriendCardAtClick(msgPaneX, y); card != nil {
					isSelf := m.isOwnCard(*card)
					isFriend := false
					if !isSelf && m.friendStore != nil {
						isFriend = m.friendStore.FindByCard(*card) != nil
					}
					minX := m.sidebarWidth + 2
					m.friendCardOpts = NewFriendCardOptions(*card, isFriend, isSelf, x+1, y, minX)
					m.friendCardOpts.SetSize(m.width, m.height)
					m.overlay = overlayFriendCardOptions
					return m, nil
				}

				// 2. Reaction badge — consume the click so the
				// parent message menu doesn't open over it. No
				// item-specific menu exists yet.
				if reactMsgID, _ := m.messages.ReactionAtClick(msgPaneX, y); reactMsgID != "" {
					return m, nil
				}

				// 3. File row — same treatment.
				if file := m.messages.FileAtClick(y); file != nil {
					return m, nil
				}

				// 4. Fall through: parent message options menu.
				msgID, preview := m.messages.MessageAtClick(y)
				if msgID != "" {
					// Popup minimum X is the chat history left edge.
					minX := m.sidebarWidth + 2
					allowDelete := false
					if mm := m.messages.MessageByID(msgID); mm != nil && m.isMyMessage(*mm) {
						allowDelete = true
					}
					hasFiles := false
					if mm := m.messages.MessageByID(msgID); mm != nil {
						hasFiles = len(mm.Files) > 0
					}
					m.msgOptions = NewMsgOptions(msgID, preview, x+1, y, minX, allowDelete, hasFiles)
					m.msgOptions.SetSize(m.width, m.height)
					m.overlay = overlayMsgOptions
					return m, nil
				}

				// 5. Empty space: chat-pane context menu.
				if items := m.buildChatOptionsItems(); len(items) > 0 {
					channelID := ""
					userID := ""
					if m.currentCh != nil {
						channelID = m.currentCh.ID
						userID = m.currentCh.UserID
					}
					chatLeft := m.sidebarWidth + 2
					chatRight := m.width - 2
					m.chatOptions = NewChatOptions(channelID, userID, items, x, y)
					m.chatOptions.SetBounds(chatLeft, 1, chatRight, m.inputTop-1)
					m.chatOptions.SetSize(m.width, m.height)
					m.overlay = overlayChatOptions
					return m, nil
				}
			}
			// Sidebar right-click: identify the row and open the
			// sidebar context menu. Uses ChannelByRow (non-mutating)
			// rather than SelectByRow so the right-click does NOT
			// move the sidebar cursor — the user's currently
			// active channel stays highlighted, and the menu
			// captures the right-clicked channel ID/UserID
			// independently for action dispatch.
			if !m.fullMode && y < m.inputTop && x < m.sidebarWidth+1 {
				wsOff := 0
				if m.channels.workspaceName != "" {
					wsOff = 2
				}
				viewportY := y - 1 - wsOff
				if viewportY < 0 {
					return m, nil
				}
				ch, isChannel, _ := m.channels.ChannelByRow(viewportY)
				if isChannel && ch != nil {
					items := m.buildSidebarOptionsItems(*ch)
					if len(items) == 0 {
						return m, nil
					}
					m.sidebarOptions = NewSidebarOptions(ch.ID, ch.UserID, items, x+1, y)
					m.sidebarOptions.SetSize(m.width, m.height)
					m.overlay = overlaySidebarOptions
					return m, nil
				}
			}
			return m, nil
		}
		if msg.Button == tea.MouseButtonLeft {
			// Check if clicking on the sidebar divider (within 1 char of the border).
			if !m.fullMode && y < m.inputTop {
				dividerX := m.sidebarWidth + 1
				if x >= dividerX-1 && x <= dividerX+1 {
					m.dragging = true
					return m, nil
				}
			}
			// Determine which panel was clicked based on layout.
			// Check input bar first since it spans full width.
			// Click on status bar (last line) cancels download.
			if y >= m.height-1 && m.downloading && m.downloadCancel != nil {
				m.downloadCancel()
				m.downloading = false
				m.downloadCancel = nil
				m.warning = "Download cancelled"
				return m, nil
			}
			// Click on the settings cog in the bottom-right of the status bar.
			if y >= m.height-1 {
				if cogStart, cogEnd := m.settingsCogClickArea(); cogEnd > cogStart && x >= cogStart && x < cogEnd {
					m.settings = NewSettingsModel(m.cfg, m.version)
					m.settings.SetSize(m.width, m.height)
					m.overlay = overlaySettings
					return m, nil
				}
			}

			// Click on the floating "X Notifications" indicator at
			// the top-centre of the message pane. Takes priority
			// over the underlying row content since the button
			// visually overlays it. Opens the notifications panel,
			// Downloads taskbar button.
			if dx0, dx1, dy, dVisible := m.downloadsButtonClickArea(); dVisible && y == dy && x >= dx0 && x < dx1 {
				return m, func() tea.Msg { return DownloadsOpenMsg{} }
			}
			// Background game taskbar button.
			if gx0, gx1, gy, gVisible := m.gameTaskbarClickArea(); gVisible && y == gy && x >= gx0 && x < gx1 {
				if m.backgroundGame != nil {
					gameName := m.backgroundGame.gameName
					return m, func() tea.Msg { return GameOverlayOpenMsg{GameName: gameName} }
				}
			}
			// Audio call taskbar button.
			if ax0, ax1, ay, aVisible := m.audioCallButtonClickArea(); aVisible && y == ay && x >= ax0 && x < ax1 {
				m.audioCallModel = NewAudioCallModel(m.activeCall)
				m.audioCallModel.SetSize(m.width, m.height)
				m.overlay = overlayAudioCall
				return m, nil
			}
			// matching the keyboard binding behaviour.
			if nx0, nx1, ny, visible := m.notificationsButtonClickArea(); visible && y == ny && x >= nx0 && x < nx1 {
				m.notifs = NewNotificationsOverlay(m.notifStore.All())
				m.notifs.SetSize(m.width, m.height)
				m.overlay = overlayNotifications
				return m, nil
			}

			if y >= m.inputTop {
				m.focus = types.FocusInput
				m.updateFocus()
			} else if x < m.sidebarWidth+1 {
				// Sidebar clicked.
				m.focus = types.FocusSidebar
				m.updateFocus()

				// SelectByRow expects a viewport-relative row. The sidebar
				// has a top border (1 row) before its content, and optionally
				// a workspace name header (2 rows: name + blank) above the channel list.
				wsHeaderOffset := 0
				if m.channels.workspaceName != "" {
					wsHeaderOffset = 2
				}
				viewportY := y - 1 - wsHeaderOffset
				if viewportY < 0 {
					return m, nil
				}
				ch, isChannel, headerKey := m.channels.SelectByRow(viewportY)
				if headerKey != "" {
					// Header clicked — toggle collapse.
					m.channels.ToggleCollapse(headerKey)
					m.channels.buildRows()
					m.cfg.CollapsedGroups = m.channels.CollapsedGroups()
					config.SaveDebounced(m.cfg)
				} else if isChannel && ch != nil {
					// Switching channels exits thread view if it
					// was open — the thread belongs to whatever
					// chat we were just viewing.
					if m.messages.InThreadMode() {
						m.messages.ExitThreadMode()
					}
					m.currentCh = ch
					m.channels.ClearUnread(ch.ID)
					m.markSlackRead(ch)
					m.clearChannelNotifs(ch.ID)
					m.setChannelHeader()
					m.saveLastChannel(ch.ID)
					// Move focus to the input so the user can
					// start typing immediately after picking a
					// channel via mouse.
					m.focus = types.FocusInput
					m.updateFocus()
					if ch.IsFriend {
						m.loadFriendHistory(ch.UserID)
						return m, nil
					}
					return m, loadHistoryCmd(m.slackSvc, ch.ID)
				}
			} else {
				// Messages area clicked.
				m.focus = types.FocusMessages
				m.updateFocus()

				msgPaneX := x - m.sidebarWidth - 2

				// Friend chat: header line is at y == 1 (top border at 0).
				// If the user clicked the cog icon in the upper-right of
				// the header, open Friend Details for the current friend.
				if y == 1 && m.currentCh != nil && m.currentCh.IsFriend && m.currentCh.UserID != "" {
					if cs, ce := m.messages.FriendCogPaneClickArea(); ce > cs && msgPaneX >= cs && msgPaneX < ce {
						friendID := m.currentCh.UserID
						return m, func() tea.Msg { return FriendsConfigOpenMsg{FriendID: friendID} }
					}
				}

				// Check if a [FRIEND:...] pill was clicked.
				if card := m.messages.FriendCardAtClick(msgPaneX, y); card != nil {
					return m, func() tea.Msg { return FriendCardClickedMsg{Card: *card} }
				}

				// Check if a reaction badge was clicked — toggle the reaction.
				if reactMsgID, emoji := m.messages.ReactionAtClick(msgPaneX, y); reactMsgID != "" {
					m.toggleReaction(reactMsgID, emoji)
					return m, nil
				}

				// Check if a "X replies" line was clicked.
				if replyParentID := m.messages.ReplyLineMessageID(y); replyParentID != "" {
					if m.cfg.ReplyFormat == "inside" {
						// Find parent index and enter thread mode.
						for i, mm := range m.messages.messages {
							if mm.MessageID == replyParentID {
								m.messages.EnterThreadMode(i)
								break
							}
						}
					} else {
						// Inline mode: toggle collapse.
						m.messages.ToggleReplyCollapse(replyParentID)
					}
					return m, nil
				}

				// Check if a file was clicked.
				file := m.messages.FileAtClick(y)
				if file != nil {
					if file.Uploading {
						// Click on an uploading file: queue a
						// confirmation in the status bar.
						msgID := m.messages.MessageIDForFile(file.ID)
						if msgID != "" {
							m.pendingCancelUploadKey = msgID + "|" + file.ID
							m.warning = fmt.Sprintf("Cancel upload of %s? [y/N]", file.Name)
						}
						return m, nil
					}
					downloadPath := m.cfg.DownloadPath
					if downloadPath == "" {
						home, _ := os.UserHomeDir()
						downloadPath = filepath.Join(home, "Downloads")
					}
					destPath := filepath.Join(downloadPath, file.Name)
					m.warning = fmt.Sprintf("Downloading %s...", file.Name)
					return m, m.startDownload(*file, destPath)
				}
			}

		} else if msg.Button == tea.MouseButtonWheelUp {
			lines := 3
			if msg.Ctrl || msg.Shift {
				lines = 15
			}
			// Scroll based on mouse position, not focus.
			if y >= m.inputTop {
				m.focus = types.FocusInput
				m.updateFocus()
				for i := 0; i < lines; i++ {
					m.input, _ = m.input.Update(tea.KeyMsg{Type: tea.KeyUp})
				}
			} else if x < m.sidebarWidth+1 && !m.fullMode {
				m.focus = types.FocusSidebar
				m.updateFocus()
				m.channels.selected -= lines
				if m.channels.selected < 0 {
					m.channels.selected = 0
				}
				m.channels.ensureVisible()
			} else if m.outputActive {
				// Output pane is replacing the messages view.
				// Route wheel scroll to the output viewport.
				m.focus = types.FocusMessages
				m.updateFocus()
				for i := 0; i < lines; i++ {
					m.outputView, _ = m.outputView.Update(msg)
				}
			} else {
				m.focus = types.FocusMessages
				m.updateFocus()
				for i := 0; i < lines; i++ {
					m.messages, _ = m.messages.Update(tea.KeyMsg{Type: tea.KeyUp})
				}
			}
			return m, nil

		} else if msg.Button == tea.MouseButtonWheelDown {
			lines := 3
			if msg.Ctrl || msg.Shift {
				lines = 15
			}
			if y >= m.inputTop {
				m.focus = types.FocusInput
				m.updateFocus()
				for i := 0; i < lines; i++ {
					m.input, _ = m.input.Update(tea.KeyMsg{Type: tea.KeyDown})
				}
			} else if x < m.sidebarWidth+1 && !m.fullMode {
				m.focus = types.FocusSidebar
				m.updateFocus()
				m.channels.selected += lines
				if m.channels.selected >= len(m.channels.rows) {
					m.channels.selected = len(m.channels.rows) - 1
				}
				m.channels.ensureVisible()
			} else if m.outputActive {
				// Output pane is replacing the messages view.
				// Route wheel scroll to the output viewport.
				m.focus = types.FocusMessages
				m.updateFocus()
				for i := 0; i < lines; i++ {
					m.outputView, _ = m.outputView.Update(msg)
				}
			} else {
				m.focus = types.FocusMessages
				m.updateFocus()
				for i := 0; i < lines; i++ {
					m.messages, _ = m.messages.Update(tea.KeyMsg{Type: tea.KeyDown})
				}
			}
			return m, nil
		}
	}

	return m, nil
}

// Tab cycle: Sidebar → Input → Messages → Sidebar.
func (m *Model) cycleFocusForward() {
	switch m.focus {
	case types.FocusSidebar:
		m.focus = types.FocusInput
	case types.FocusInput:
		m.focus = types.FocusMessages
	case types.FocusMessages:
		m.focus = types.FocusSidebar
	}
}

func (m *Model) cycleFocusBackward() {
	switch m.focus {
	case types.FocusSidebar:
		m.focus = types.FocusMessages
	case types.FocusInput:
		m.focus = types.FocusSidebar
	case types.FocusMessages:
		m.focus = types.FocusInput
	}
}

func (m *Model) updateFocus() {
	m.channels.SetFocused(m.focus == types.FocusSidebar)
	m.messages.SetFocused(m.focus == types.FocusMessages)
	m.input.SetFocused(m.focus == types.FocusInput)
	// Output pane shares the messages-pane focus slot — when the
	// user Tabs to "messages" and the output is active, the
	// output pane gets the active border (and its key handler
	// receives scroll/esc events). Tabbing away (to input or
	// sidebar) clears the active border so the visual cue
	// matches where keystrokes actually go.
	m.outputView.SetFocused(m.focus == types.FocusMessages && m.outputActive)
	// When leaving the sidebar, reset selection to the current channel.
	if m.focus != types.FocusSidebar && m.currentCh != nil {
		m.channels.SelectByID(m.currentCh.ID)
	}
	if m.fullMode {
		m.resizeComponents()
	}
}
