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
	padding    int // grid cell padding (0 = no gap)
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
		categories: tabs,
		favorites:  append([]string{}, favorites...),
		purpose:    purpose,
		padding:    1,
		gridCols:   8,
		gridRows:   6,
	}
}

func (m *EmojiPickerModel) SetSize(w, h int) {
	m.width = w
	m.height = h

	// Cell dimensions:
	// - horizontal: padding left + emoji (2) + padding right
	// - vertical: 1 line above each emoji + emoji line = 2 rows always
	hPad := m.padding
	cellW := 2 + 2*hPad
	rowH := 2

	innerW := min(80, w-8) - 6
	maxCols := innerW / cellW
	if maxCols < 4 {
		maxCols = 4
	}
	if maxCols > 12 {
		maxCols = 12
	}
	m.gridCols = maxCols

	availH := min(h-4, 40) - 9
	maxRows := availH / rowH
	if maxRows < 3 {
		maxRows = 3
	}
	if maxRows > 10 {
		maxRows = 10
	}
	m.gridRows = maxRows

	m.computeLayout()
}

// computeLayout calculates the box position and grid/tab coordinates.
// Mirrors the layout used by View so click hit-testing matches rendering.
func (m *EmojiPickerModel) computeLayout() {
	hPad := m.padding
	cellW := 2 + 2*hPad
	boxInner := m.gridCols * cellW

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
	tabItemWidth := 5
	tabsPerRow := boxInner / tabItemWidth
	if tabsPerRow < 3 {
		tabsPerRow = 3
	}
	m.tabsPerRow = tabsPerRow

	// Box content area: top border(1) + padding(1) = +2 from box top.
	// Then "Emoji Picker" title row (1) + blank (1) = tabs at +4.
	// Tabs render 1 row higher and 1 col left of computed (observed offset).
	contentLeftX := m.boxX + 2 // border + 1 padding (observed)
	m.tabRowY = m.boxY + 3

	m.tabsPositions = nil
	tabRowOffset := 0
	tabColOffset := 0
	for ti := range m.categories {
		m.tabsPositions = append(m.tabsPositions, tabPos{
			idx: ti,
			x:   tabColOffset,
			y:   tabRowOffset,
			w:   tabItemWidth,
		})
		tabColOffset += tabItemWidth
		if (ti+1)%tabsPerRow == 0 && ti < len(m.categories)-1 {
			tabRowOffset += 2
			tabColOffset = 0
		}
	}

	tabRowsUsed := (len(m.categories) + tabsPerRow - 1) / tabsPerRow
	if tabRowsUsed < 1 {
		tabRowsUsed = 1
	}
	// Grid first row = tab base + (rows-1)*2 + 3 (for "\n\n" + sep + "\n").
	m.gridStartY = m.tabRowY + (tabRowsUsed-1)*2 + 3
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
			// Tab click hit-test.
			for _, tp := range m.tabsPositions {
				screenX := m.boxX + 2 + tp.x // box border + padding (observed)
				screenY := m.tabRowY + tp.y
				if msg.X >= screenX && msg.X < screenX+tp.w && msg.Y == screenY {
					m.activeTab = tp.idx
					m.scrollOff = 0
					m.cursorR = 0
					m.cursorC = 0
					return m, nil
				}
			}
			// Grid cell click hit-test.
			if msg.Y >= m.gridStartY && msg.X >= m.gridStartX {
				cellW := 2 + 2*m.padding
				rowH := 2
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

	tabActiveStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorPrimary).Background(lipgloss.Color("236")).Padding(0, 1)
	tabInactiveStyle := lipgloss.NewStyle().Foreground(ColorMuted).Padding(0, 1)
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

	// Tabs — manually wrap into rows with vertical spacing.
	cellWidth := 2 + m.padding
	boxInner := m.gridCols*cellWidth + m.padding/2

	// Each tab icon takes ~4 display chars (emoji 2 + padding 2 from style).
	tabItemWidth := 5
	tabsPerRow := boxInner / tabItemWidth
	if tabsPerRow < 3 {
		tabsPerRow = 3
	}
	m.tabsPerRow = tabsPerRow

	// Track tab positions relative to the box content area for click hit-testing.
	// Content area starts after: top border (1) + padding(1) + title(1) + blank(1) = 4 from box top.
	m.tabsPositions = nil
	tabRowOffset := 0 // rows below content start
	tabColOffset := 0 // cols within current row
	for ti, tab := range m.categories {
		label := tab.icon
		m.tabsPositions = append(m.tabsPositions, tabPos{
			idx: ti,
			x:   tabColOffset,
			y:   tabRowOffset,
			w:   tabItemWidth,
		})
		if ti == m.activeTab {
			b.WriteString(tabActiveStyle.Render(label))
		} else {
			b.WriteString(tabInactiveStyle.Render(label))
		}
		b.WriteString(" ")
		tabColOffset += tabItemWidth
		// End of row — add vertical spacing.
		if (ti+1)%tabsPerRow == 0 && ti < len(m.categories)-1 {
			b.WriteString("\n\n")
			tabRowOffset += 2
			tabColOffset = 0
		}
	}
	b.WriteString("\n\n")
	b.WriteString(strings.Repeat("─", boxInner))
	b.WriteString("\n")

	// Grid — horizontal padding from m.padding, always 1 blank line above each emoji.
	items := m.currentItems()
	if len(items) == 0 {
		b.WriteString(dimStyle.Render("  (empty)"))
		b.WriteString("\n")
	} else {
		hBefore := strings.Repeat(" ", m.padding)
		hAfter := strings.Repeat(" ", m.padding)
		vBefore := 1
		vAfter := 0

		// Full cell width for background fill on vertical padding lines.
		fullCellW := 2 + 2*m.padding

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
