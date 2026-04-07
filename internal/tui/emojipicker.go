package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/rw3iss/slackers/internal/format"
)

// EmojiPickerPurpose indicates why the picker was opened.
type EmojiPickerPurpose int

const (
	EmojiPurposeInsert   EmojiPickerPurpose = iota // insert into text input
	EmojiPurposeReaction                            // react to a message
)

// EmojiSelectedMsg is sent when the user picks an emoji.
type EmojiSelectedMsg struct {
	Code    string             // shortcode (e.g. "thumbsup")
	Emoji   string             // unicode (e.g. "👍")
	Purpose EmojiPickerPurpose
}

// EmojiPickerModel is a modal emoji picker with category tabs and grid.
type EmojiPickerModel struct {
	categories []emojiTab
	activeTab  int
	gridCols   int
	gridRows   int // visible rows
	cursorR    int // row within visible grid
	cursorC    int // column
	scrollOff  int // scroll offset (rows)
	favorites  []string // shortcodes in order
	favDirty   bool
	purpose    EmojiPickerPurpose
	padding    int // grid cell padding (0 = no gap), split symmetrically on both axes
	// extraRightPad adds N additional columns of horizontal padding to the right
	// of each emoji (without affecting vertical spacing).
	extraRightPad int
	width      int
	height     int

	// Render layout (computed in View, used by mouse hit-testing).
	boxX, boxY  int // top-left of the rendered box
	boxW, boxH  int
	tabRowY     int // screen Y of the tab row
	tabsPerRow  int // wrapping limit for tabs
	gridStartY  int // screen Y of the first grid row
	gridStartX  int // screen X of the first grid cell
	tabsPositions []tabPos // (cat index, x, y) for click hit-testing
}

type tabPos struct {
	idx  int
	x, y int
	w    int
}

type emojiTab struct {
	name  string
	icon  string
	items []format.EmojiEntry
}

// NewEmojiPicker creates an emoji picker. favorites is the ordered list of shortcodes.
func NewEmojiPicker(favorites []string, purpose EmojiPickerPurpose) EmojiPickerModel {
	var tabs []emojiTab

	// Favorites tab (only if there are favorites).
	if len(favorites) > 0 {
		var favItems []format.EmojiEntry
		for _, code := range favorites {
			if e := format.FindByCode(code); e != nil {
				favItems = append(favItems, *e)
			}
		}
		if len(favItems) > 0 {
			tabs = append(tabs, emojiTab{name: "Favorites", icon: "⭐", items: favItems})
		}
	}

	// Category tabs from structured data.
	for _, cat := range format.Categories {
		tabs = append(tabs, emojiTab{name: cat.Name, icon: cat.Icon, items: cat.Items})
	}

	return EmojiPickerModel{
		categories:    tabs,
		favorites:     append([]string{}, favorites...),
		purpose:       purpose,
		padding:       2,
		extraRightPad: 1,
		gridCols:      8,
		gridRows:      6,
	}
}

func (m *EmojiPickerModel) SetSize(w, h int) {
	m.width = w
	m.height = h

	// Cell dimensions: emoji (2 chars wide) + symmetric padding + extra right pad.
	cellW := 2 + m.padding + m.extraRightPad
	innerW := min(60, w-8) - 6
	maxCols := innerW / cellW
	if maxCols < 4 {
		maxCols = 4
	}
	if maxCols > 8 {
		maxCols = 8
	}
	m.gridCols = maxCols

	rowH := 1 + m.padding
	availH := min(h-4, 40) - 9
	maxRows := availH / rowH
	if maxRows < 3 {
		maxRows = 3
	}
	if maxRows > 10 {
		maxRows = 10
	}
	m.gridRows = maxRows

	// Compute layout positions for click hit-testing.
	m.computeLayout()
}

// computeLayout calculates the box position and grid/tab coordinates.
// Mirrors the layout used by View so click hit-testing matches rendering.
func (m *EmojiPickerModel) computeLayout() {
	cellW := 2 + m.padding + m.extraRightPad
	boxInner := m.gridCols*cellW + m.padding/2

	boxWidth := boxInner + 8
	if boxWidth > m.width-4 {
		boxWidth = m.width - 4
	}
	if boxWidth < 20 {
		boxWidth = 20
	}
	boxHeight := m.height - 4
	if boxHeight < 12 {
		boxHeight = 12
	}
	m.boxW = boxWidth
	m.boxH = boxHeight
	m.boxX = (m.width - boxWidth) / 2
	m.boxY = (m.height - boxHeight) / 2
	if m.boxX < 0 {
		m.boxX = 0
	}
	if m.boxY < 0 {
		m.boxY = 0
	}

	// Tab layout.
	// Each tab cell is 5 cols wide; cells are separated by 1 col.
	const tabCellInnerW = 5
	tabItemWidth := tabCellInnerW + 1 // 6
	tabsPerRow := boxInner / tabItemWidth
	if tabsPerRow < 3 {
		tabsPerRow = 3
	}
	m.tabsPerRow = tabsPerRow

	// Box content area: top border(1) + padding(1) = +2 from box top.
	// Then "Emoji Picker" title row (1) + blank (1) = tabs at +4.
	contentLeftX := m.boxX + 2
	m.tabRowY = m.boxY + 3

	m.tabsPositions = nil
	tabRowOffset := 0
	tabColOffset := 0
	for ti := range m.categories {
		// y points to the icon row (the second of the 2-line tab block);
		// the click test allows either line.
		m.tabsPositions = append(m.tabsPositions, tabPos{
			idx: ti,
			x:   tabColOffset,
			y:   tabRowOffset + 1,
			w:   tabCellInnerW,
		})
		tabColOffset += tabCellInnerW
		if (ti+1)%tabsPerRow != 0 && ti < len(m.categories)-1 {
			tabColOffset++ // separator space
		}
		if (ti+1)%tabsPerRow == 0 && ti < len(m.categories)-1 {
			tabRowOffset += 3 // 2 lines tab + 1 blank between rows
			tabColOffset = 0
		}
	}

	tabRowsUsed := (len(m.categories) + tabsPerRow - 1) / tabsPerRow
	if tabRowsUsed < 1 {
		tabRowsUsed = 1
	}
	// Each tab row is a 3-line block (above/icons/below), no blank between rows.
	// After the last row: "\n" + separator + "\n" → grid starts 2 lines after
	// the last "below" line.
	// Last row's "below" line is at offset (rowsUsed-1)*3 + 2.
	m.gridStartY = m.tabRowY + (tabRowsUsed-1)*3 + 2 + 2
	m.gridStartX = contentLeftX
}

func (m *EmojiPickerModel) currentItems() []format.EmojiEntry {
	if m.activeTab < 0 || m.activeTab >= len(m.categories) {
		return nil
	}
	return m.categories[m.activeTab].items
}

func (m *EmojiPickerModel) totalRows() int {
	items := m.currentItems()
	if len(items) == 0 {
		return 0
	}
	return (len(items) + m.gridCols - 1) / m.gridCols
}

func (m *EmojiPickerModel) selectedIndex() int {
	return (m.scrollOff+m.cursorR)*m.gridCols + m.cursorC
}

func (m *EmojiPickerModel) selectedEmoji() *format.EmojiEntry {
	items := m.currentItems()
	idx := m.selectedIndex()
	if idx < 0 || idx >= len(items) {
		return nil
	}
	return &items[idx]
}

func (m *EmojiPickerModel) isFavorite(code string) bool {
	for _, f := range m.favorites {
		if f == code {
			return true
		}
	}
	return false
}

func (m *EmojiPickerModel) clampCursor() {
	total := m.totalRows()
	maxScroll := total - m.gridRows
	if maxScroll < 0 {
		maxScroll = 0
	}
	if m.scrollOff > maxScroll {
		m.scrollOff = maxScroll
	}
	if m.scrollOff < 0 {
		m.scrollOff = 0
	}
	if m.cursorR >= m.gridRows {
		m.cursorR = m.gridRows - 1
	}
	if m.cursorR < 0 {
		m.cursorR = 0
	}
	if m.cursorC >= m.gridCols {
		m.cursorC = m.gridCols - 1
	}
	if m.cursorC < 0 {
		m.cursorC = 0
	}
	// Clamp to actual items in the last row.
	items := m.currentItems()
	idx := m.selectedIndex()
	if idx >= len(items) && len(items) > 0 {
		lastIdx := len(items) - 1
		m.cursorC = lastIdx % m.gridCols
	}
}

// Favorites returns the current favorites list (for saving to config).
func (m *EmojiPickerModel) Favorites() []string {
	return m.favorites
}

// FavDirty returns true if favorites were modified.
func (m *EmojiPickerModel) FavDirty() bool {
	return m.favDirty
}

// ClearFavDirty resets the dirty flag (called after the model has been
// persisted by the caller).
func (m *EmojiPickerModel) ClearFavDirty() {
	m.favDirty = false
}

func (m *EmojiPickerModel) toggleFavorite() {
	e := m.selectedEmoji()
	if e == nil {
		return
	}
	// Remove if already favorited.
	for i, f := range m.favorites {
		if f == e.Code {
			m.favorites = append(m.favorites[:i], m.favorites[i+1:]...)
			m.favDirty = true
			m.rebuildFavTab()
			return
		}
	}
	// Add to end.
	m.favorites = append(m.favorites, e.Code)
	m.favDirty = true
	m.rebuildFavTab()
}

func (m *EmojiPickerModel) rebuildFavTab() {
	if len(m.categories) == 0 {
		return
	}
	// Check if first tab is favorites.
	if m.categories[0].name == "Favorites" {
		if len(m.favorites) == 0 {
			// Remove favorites tab.
			m.categories = m.categories[1:]
			if m.activeTab > 0 {
				m.activeTab--
			}
		} else {
			var items []format.EmojiEntry
			for _, code := range m.favorites {
				if e := format.FindByCode(code); e != nil {
					items = append(items, *e)
				}
			}
			m.categories[0].items = items
		}
	} else if len(m.favorites) > 0 {
		// Insert favorites tab.
		var items []format.EmojiEntry
		for _, code := range m.favorites {
			if e := format.FindByCode(code); e != nil {
				items = append(items, *e)
			}
		}
		m.categories = append([]emojiTab{{name: "Favorites", icon: "⭐", items: items}}, m.categories...)
		m.activeTab++
	}
}

func (m *EmojiPickerModel) moveFavorite(dr, dc int) {
	if m.activeTab >= len(m.categories) || m.categories[m.activeTab].name != "Favorites" {
		return
	}
	idx := m.selectedIndex()
	if idx < 0 || idx >= len(m.favorites) {
		return
	}
	newIdx := idx
	if dc != 0 {
		newIdx = idx + dc
	}
	if dr != 0 {
		newIdx = idx + dr*m.gridCols
	}
	if newIdx < 0 || newIdx >= len(m.favorites) || newIdx == idx {
		return
	}
	// Swap.
	m.favorites[idx], m.favorites[newIdx] = m.favorites[newIdx], m.favorites[idx]
	m.favDirty = true
	m.rebuildFavTab()
	// Move cursor to new position.
	m.cursorR = (newIdx - m.scrollOff*m.gridCols) / m.gridCols
	m.cursorC = newIdx % m.gridCols
	// Adjust scroll if needed.
	if m.cursorR < 0 {
		m.scrollOff += m.cursorR
		m.cursorR = 0
	}
	if m.cursorR >= m.gridRows {
		m.scrollOff += m.cursorR - m.gridRows + 1
		m.cursorR = m.gridRows - 1
	}
}

// Update handles keyboard and mouse input.
func (m EmojiPickerModel) Update(msg tea.Msg) (EmojiPickerModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "left", "h":
			m.cursorC--
			if m.cursorC < 0 {
				m.cursorC = m.gridCols - 1
				m.cursorR--
			}
			m.clampCursor()
		case "right", "l":
			m.cursorC++
			if m.cursorC >= m.gridCols {
				m.cursorC = 0
				m.cursorR++
			}
			m.clampCursor()
		case "up", "k":
			m.cursorR--
			if m.cursorR < 0 && m.scrollOff > 0 {
				m.scrollOff--
				m.cursorR = 0
			}
			m.clampCursor()
		case "down", "j":
			m.cursorR++
			if m.cursorR >= m.gridRows && m.scrollOff+m.gridRows < m.totalRows() {
				m.scrollOff++
				m.cursorR = m.gridRows - 1
			}
			m.clampCursor()
		case "tab":
			m.activeTab = (m.activeTab + 1) % len(m.categories)
			m.scrollOff = 0
			m.cursorR = 0
			m.cursorC = 0
		case "shift+tab":
			m.activeTab--
			if m.activeTab < 0 {
				m.activeTab = len(m.categories) - 1
			}
			m.scrollOff = 0
			m.cursorR = 0
			m.cursorC = 0
		case "enter":
			if e := m.selectedEmoji(); e != nil {
				return m, func() tea.Msg {
					return EmojiSelectedMsg{Code: e.Code, Emoji: e.Emoji, Purpose: m.purpose}
				}
			}
		case "f":
			m.toggleFavorite()
		case "ctrl+up":
			if m.activeTab < len(m.categories) && m.categories[m.activeTab].name == "Favorites" {
				m.moveFavorite(-1, 0)
			}
		case "ctrl+down":
			if m.activeTab < len(m.categories) && m.categories[m.activeTab].name == "Favorites" {
				m.moveFavorite(1, 0)
			}
		case "ctrl+left":
			if m.activeTab < len(m.categories) && m.categories[m.activeTab].name == "Favorites" {
				m.moveFavorite(0, -1)
			}
		case "ctrl+right":
			if m.activeTab < len(m.categories) && m.categories[m.activeTab].name == "Favorites" {
				m.moveFavorite(0, 1)
			}
		}
	case tea.MouseMsg:
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			m.scrollOff--
			m.clampCursor()
		case tea.MouseButtonWheelDown:
			m.scrollOff++
			m.clampCursor()
		case tea.MouseButtonLeft:
			if msg.Action != tea.MouseActionPress {
				return m, nil
			}
			// Tab click hit-test. Each tab is a 3-line block; accept clicks on
			// the highlight row above (iconY-1), the icon row (iconY), or
			// the highlight row below (iconY+1).
			for _, tp := range m.tabsPositions {
				screenX := m.boxX + 1 + 2 + tp.x // box border + padding-left
				iconY := m.tabRowY + tp.y
				if msg.X >= screenX && msg.X < screenX+tp.w &&
					msg.Y >= iconY-1 && msg.Y <= iconY+1 {
					m.activeTab = tp.idx
					m.scrollOff = 0
					m.cursorR = 0
					m.cursorC = 0
					return m, nil
				}
			}
			// Grid cell click hit-test.
			if msg.Y >= m.gridStartY && msg.X >= m.gridStartX {
				cellW := 2 + m.padding + m.extraRightPad
				rowH := 1 + m.padding
				dx := msg.X - m.gridStartX
				dy := msg.Y - m.gridStartY
				col := dx / cellW
				row := dy / rowH
				if col >= 0 && col < m.gridCols && row >= 0 && row < m.gridRows {
					items := m.currentItems()
					idx := (m.scrollOff+row)*m.gridCols + col
					if idx >= 0 && idx < len(items) {
						m.cursorR = row
						m.cursorC = col
						e := items[idx]
						return m, func() tea.Msg {
							return EmojiSelectedMsg{Code: e.Code, Emoji: e.Emoji, Purpose: m.purpose}
						}
					}
				}
			}
		}
	}
	return m, nil
}

// View renders the emoji picker.
func (m *EmojiPickerModel) View() string {
	m.clampCursor()

	// Active tab is rendered as a 2-line block (top = background only, bottom =
	// emoji + background) so the highlight extends one row above the icon.
	// Right padding is 2 columns instead of 1 so the highlight extends 1 column
	// further right of the emoji as well.
	activeBg := lipgloss.Color("236")
	activeBgStyle := lipgloss.NewStyle().Background(activeBg)
	activeIconStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorPrimary).Background(activeBg)
	inactiveIconStyle := lipgloss.NewStyle().Foreground(ColorMuted)
	// tab cell layout: " EE  " (1 left pad + 2 emoji + 2 right pad = 5 cols).
	const tabCellLeftPad = 1
	const tabCellRightPad = 2
	const tabCellInnerW = tabCellLeftPad + 2 + tabCellRightPad // 5
	cellStyle := lipgloss.NewStyle()
	selectedCellStyle := lipgloss.NewStyle().Background(lipgloss.Color("240"))
	favCellStyle := lipgloss.NewStyle().Background(lipgloss.Color("235"))
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorPrimary)
	dimStyle := lipgloss.NewStyle().Foreground(ColorMuted).Italic(true)
	codeStyle := lipgloss.NewStyle().Foreground(ColorAccent)

	var b strings.Builder

	// Title.
	b.WriteString(titleStyle.Render("Emoji Picker"))
	b.WriteString("\n\n")

	// Tabs — manually wrap into rows. Each row is a 3-line block:
	//   line 0: background-only highlight bar above active tabs
	//   line 1: emoji icons (with active tabs highlighted)
	//   line 2: background-only highlight bar below active tabs
	// No blank line between rows — the upper/lower highlights provide spacing.
	cellWidth := 2 + m.padding + m.extraRightPad
	boxInner := m.gridCols*cellWidth + m.padding/2

	// 1 col separator between adjacent tab cells.
	tabItemWidth := tabCellInnerW + 1 // 5 + 1 separator = 6
	tabsPerRow := boxInner / tabItemWidth
	if tabsPerRow < 3 {
		tabsPerRow = 3
	}
	m.tabsPerRow = tabsPerRow

	// Track tab positions relative to the box content area for click hit-testing.
	m.tabsPositions = nil
	rowAbove := strings.Builder{}
	rowIcons := strings.Builder{}
	rowBelow := strings.Builder{}
	tabRowOffset := 0 // line offset from start of tab block
	tabColOffset := 0 // column offset from start of row
	emptyCell := strings.Repeat(" ", tabCellInnerW)
	flushRow := func(last bool) {
		b.WriteString(rowAbove.String())
		b.WriteString("\n")
		b.WriteString(rowIcons.String())
		b.WriteString("\n")
		b.WriteString(rowBelow.String())
		rowAbove.Reset()
		rowIcons.Reset()
		rowBelow.Reset()
		if !last {
			// Tabs in the next row sit directly under this row's lower
			// highlight — no blank line between rows.
			b.WriteString("\n")
			tabRowOffset += 3 // above + icons + below
			tabColOffset = 0
		}
	}
	for ti, tab := range m.categories {
		isActive := ti == m.activeTab
		// Record the tab's screen position. y points to the icon row (line 1
		// of the 3-line block); the click test allows the rows above/below too.
		m.tabsPositions = append(m.tabsPositions, tabPos{
			idx: ti,
			x:   tabColOffset,
			y:   tabRowOffset + 1,
			w:   tabCellInnerW,
		})
		// Build the cell (above + icons + below).
		iconCell := strings.Repeat(" ", tabCellLeftPad) + tab.icon + strings.Repeat(" ", tabCellRightPad)
		if isActive {
			rowAbove.WriteString(activeBgStyle.Render(emptyCell))
			rowIcons.WriteString(activeIconStyle.Render(iconCell))
			rowBelow.WriteString(activeBgStyle.Render(emptyCell))
		} else {
			rowAbove.WriteString(emptyCell)
			rowIcons.WriteString(inactiveIconStyle.Render(iconCell))
			rowBelow.WriteString(emptyCell)
		}
		tabColOffset += tabCellInnerW
		// Separator space between tabs (no background).
		if (ti+1)%tabsPerRow != 0 && ti < len(m.categories)-1 {
			rowAbove.WriteString(" ")
			rowIcons.WriteString(" ")
			rowBelow.WriteString(" ")
			tabColOffset++
		}
		// End of row?
		endOfRow := (ti+1)%tabsPerRow == 0 && ti < len(m.categories)-1
		if endOfRow {
			flushRow(false)
		}
	}
	// Flush the final row.
	if rowIcons.Len() > 0 {
		flushRow(true)
	}
	// Single newline before separator (no extra blank — the lower highlight
	// already provides bottom spacing for the active tab).
	b.WriteString("\n")
	b.WriteString(strings.Repeat("─", boxInner))
	b.WriteString("\n")

	// Grid — padding: odd = gap after emoji, even = split before/after.
	items := m.currentItems()
	if len(items) == 0 {
		b.WriteString(dimStyle.Render("  (empty)"))
		b.WriteString("\n")
	} else {
		// Horizontal: split symmetric padding + extra columns to the right.
		padBefore := m.padding / 2
		padAfter := m.padding - padBefore + m.extraRightPad
		hBefore := strings.Repeat(" ", padBefore)
		hAfter := strings.Repeat(" ", padAfter)
		// Vertical: odd puts extra BEFORE the emoji so highlight extends above.
		vAfter := m.padding / 2
		vBefore := m.padding - vAfter

		// Full cell width for background fill on vertical padding lines.
		fullCellW := 2 + m.padding + m.extraRightPad // emoji width + total h-padding

		for r := 0; r < m.gridRows; r++ {
			rowStart := (m.scrollOff + r) * m.gridCols
			if rowStart >= len(items) {
				break
			}

			// Vertical gap before row — extend selected/fav background.
			for v := 0; v < vBefore; v++ {
				var vRow strings.Builder
				for c := 0; c < m.gridCols; c++ {
					idx := rowStart + c
					style := cellStyle
					if idx < len(items) {
						if r == m.cursorR && c == m.cursorC {
							style = selectedCellStyle
						} else if m.isFavorite(items[idx].Code) {
							style = favCellStyle
						}
					}
					vRow.WriteString(style.Render(strings.Repeat(" ", fullCellW)))
				}
				b.WriteString(vRow.String())
				b.WriteString("\n")
			}

			// Emoji row — render padding inside the style.
			var row strings.Builder
			for c := 0; c < m.gridCols; c++ {
				idx := rowStart + c
				if idx >= len(items) {
					break
				}
				e := items[idx]
				style := cellStyle
				if r == m.cursorR && c == m.cursorC {
					style = selectedCellStyle
				} else if m.isFavorite(e.Code) {
					style = favCellStyle
				}
				// Render the full cell (padding + emoji + padding) with background.
				cell := hBefore + e.Emoji + hAfter
				row.WriteString(style.Render(cell))
			}
			b.WriteString(row.String())
			b.WriteString("\n")

			// Vertical gap after row — extend selected/fav background.
			for v := 0; v < vAfter; v++ {
				var vRow strings.Builder
				for c := 0; c < m.gridCols; c++ {
					idx := rowStart + c
					style := cellStyle
					if idx < len(items) {
						if r == m.cursorR && c == m.cursorC {
							style = selectedCellStyle
						} else if m.isFavorite(items[idx].Code) {
							style = favCellStyle
						}
					}
					vRow.WriteString(style.Render(strings.Repeat(" ", fullCellW)))
				}
				b.WriteString(vRow.String())
				b.WriteString("\n")
			}
		}
	}

	// Selected emoji info.
	b.WriteString("\n")
	if e := m.selectedEmoji(); e != nil {
		b.WriteString(fmt.Sprintf("  %s %s", e.Emoji, codeStyle.Render(":"+e.Code+":")))
		if m.isFavorite(e.Code) {
			b.WriteString(dimStyle.Render(" [fav]"))
		}
	}

	// Scroll indicator.
	total := m.totalRows()
	if total > m.gridRows {
		b.WriteString(dimStyle.Render(fmt.Sprintf("  [%d/%d]", m.scrollOff+1, total-m.gridRows+1)))
	}

	// Help.
	b.WriteString("\n\n")
	isFavTab := m.activeTab < len(m.categories) && m.categories[m.activeTab].name == "Favorites"
	if isFavTab {
		b.WriteString(dimStyle.Render("  Arrows: move | Enter: select | f: unfav | Ctrl+Arrows: reorder"))
	} else {
		b.WriteString(dimStyle.Render("  Arrows: move | Tab: next cat | Enter: select | f: fav | Esc: close"))
	}

	content := b.String()

	// Calculate box dimensions.
	boxWidth := boxInner + 8 // padding + border
	if boxWidth > m.width-4 {
		boxWidth = m.width - 4
	}
	if boxWidth < 20 {
		boxWidth = 20
	}
	boxHeight := m.height - 4
	if boxHeight < 12 {
		boxHeight = 12
	}

	// Pad the content to the full inner height with explicit space-filled
	// lines so the terminal actually clears any leftover glyphs from the
	// previous tab/frame. Lipgloss .Height() otherwise just appends bare
	// newlines, which leave wide-emoji artifacts behind on switch.
	innerH := boxHeight - 4 // top/bottom borders + top/bottom padding
	innerW := boxWidth - 6  // left/right borders + left/right padding
	if innerW < 1 {
		innerW = 1
	}
	contentLines := strings.Split(content, "\n")
	for len(contentLines) < innerH {
		contentLines = append(contentLines, strings.Repeat(" ", innerW))
	}
	content = strings.Join(contentLines, "\n")

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorPrimary).
		Padding(1, 2).
		Width(boxWidth).
		Height(boxHeight)

	box := boxStyle.Render(content)

	// Compute the box's top-left when centered.
	m.boxW = lipgloss.Width(box)
	m.boxH = strings.Count(box, "\n") + 1
	m.boxX = (m.width - m.boxW) / 2
	m.boxY = (m.height - m.boxH) / 2
	if m.boxX < 0 {
		m.boxX = 0
	}
	if m.boxY < 0 {
		m.boxY = 0
	}
	// Content area starts at boxY + border(1) + padding(1) = boxY + 2.
	// Then title(1) + blank(1) = +2 more → tabs start at boxY+4.
	contentTopY := m.boxY + 2
	contentLeftX := m.boxX + 1 + 2 // border + padding-left
	m.tabRowY = contentTopY + 2    // after "Emoji Picker\n\n"

	// Grid starts after: tabs (variable rows) + "\n\n" + separator + "\n"
	// We computed tabRowOffset which is the row index of the LAST tab row.
	// Plus the separator and blank line: +3 more rows after the tab rows.
	// Actually: after last tab line we wrote "\n\n" then "─" then "\n", so:
	//   tab last row + 2 (the \n\n) = sep row, + 1 = grid first row
	tabRowsUsed := (len(m.categories) + m.tabsPerRow - 1) / m.tabsPerRow
	if tabRowsUsed < 1 {
		tabRowsUsed = 1
	}
	// tabRowsUsed rows of tabs, each followed by "\n\n" except the last has "\n\n" separator after.
	// Last tab row at: tabRowY + (tabRowsUsed-1)*2
	// Then sep at: tabRowY + (tabRowsUsed-1)*2 + 2
	// Then grid first row at: tabRowY + (tabRowsUsed-1)*2 + 3
	m.gridStartY = m.tabRowY + (tabRowsUsed-1)*2 + 3
	m.gridStartX = contentLeftX

	return lipgloss.Place(m.width, m.height,
		lipgloss.Center, lipgloss.Center,
		box)
}
