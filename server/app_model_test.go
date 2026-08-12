package server

import (
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func setupBookmarkTempDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	APP_DIR = dir
	return dir
}

func TestRenderDetailSupportsAtInUsername(t *testing.T) {
	rendered := renderDetail("name@domain@example.com:22", false)

	if !strings.Contains(rendered, "name@domain") {
		t.Fatalf("expected username with @ to be preserved, got %q", rendered)
	}
	if !strings.Contains(rendered, "example.com") {
		t.Fatalf("expected host to be preserved, got %q", rendered)
	}
	if !strings.Contains(rendered, ":22") {
		t.Fatalf("expected port to be preserved, got %q", rendered)
	}
}

func TestViewIncludesTipString(t *testing.T) {
	AM = AppModel{}
	am := &AM

	AM.BookmarkInfo.List = []BookmarkItem{
		{Title: "prod", Host: "10.0.0.1", Port: 22, Username: "root", EnableSSH: true},
	}
	AM.width = 80
	AM.height = 24
	createList()
	AM.TipString = "connection failed: timeout"

	view := am.View()
	if !strings.Contains(view, AM.TipString) {
		t.Fatalf("expected tip string %q to be rendered in view, got: %q", AM.TipString, view)
	}
}

func TestViewWrapsLongTipWithoutDroppingDetail(t *testing.T) {
	AM = AppModel{}
	am := &AM

	AM.BookmarkInfo.List = []BookmarkItem{
		{Title: "prod", Host: "10.0.0.1", Port: 22, Username: "root", EnableSSH: true},
	}
	AM.width = 100
	AM.height = 30
	createList()
	AM.tipLevel = tipError
	AM.TipString = "connection failed root@example.com:22 (password):\nReason: authentication failed\nCheck: username, password, private key, passphrase, and auth type\nDetail: ssh: handshake failed: ssh: unable to authenticate, attempted methods [none password], no supported methods remain"

	view := am.View()
	if !strings.Contains(view, "Reason: authentication failed") {
		t.Fatalf("expected wrapped tip to include reason, got: %q", view)
	}
	if !strings.Contains(view, "Detail: ssh: handshake failed") {
		t.Fatalf("expected wrapped tip to include detail prefix, got: %q", view)
	}
}
func TestWindowSizeMsgKeepsCurrentSelection(t *testing.T) {
	AM = AppModel{}
	am := &AM

	AM.BookmarkInfo.List = []BookmarkItem{
		{Title: "dev", Host: "10.0.0.1", Port: 22, Username: "root", EnableSSH: true},
		{Title: "staging", Host: "10.0.0.2", Port: 22, Username: "root", EnableSSH: true},
		{Title: "prod", Host: "10.0.0.3", Port: 22, Username: "root", EnableSSH: true},
	}
	AM.width = 100
	AM.height = 40
	createList()
	AM.list.Select(2)

	_, _ = am.Update(tea.WindowSizeMsg{Width: 120, Height: 50})

	if got := AM.list.GlobalIndex(); got != 2 {
		t.Fatalf("expected selected index to remain 2 after resize, got %d", got)
	}
}

func TestEnterShowsConnectingTipBeforeRunningConnectCmd(t *testing.T) {
	AM = AppModel{}
	am := &AM

	AM.BookmarkInfo.List = []BookmarkItem{
		{Title: "dev", Host: "10.0.0.1", Port: 22, Username: "root", EnableSSH: true},
	}
	AM.width = 60
	AM.height = 30
	createList()

	_, cmd := am.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatalf("expected enter to return async connect command")
	}

	if !strings.Contains(AM.TipString, "connecting") {
		t.Fatalf("expected immediate connecting tip, got: %q", AM.TipString)
	}

	if !AM.isConnecting {
		t.Fatalf("expected model to be in connecting state after enter")
	}
}

func TestViewRendersTipAsOverlayWithoutChangingHeight(t *testing.T) {
	AM = AppModel{}
	am := &AM

	AM.BookmarkInfo.List = []BookmarkItem{
		{Title: "dev", Host: "10.0.0.1", Port: 22, Username: "root", EnableSSH: true},
		{Title: "staging", Host: "10.0.0.2", Port: 22, Username: "root", EnableSSH: true},
	}
	AM.width = 80
	AM.height = 24
	createList()

	AM.TipString = ""
	viewWithoutTip := am.View()

	AM.TipString = "connecting root@10.0.0.1:22 (password)..."
	viewWithTip := am.View()

	if !strings.Contains(viewWithTip, "connecting") {
		t.Fatalf("expected view with tip to contain connecting message")
	}

	heightWithoutTip := strings.Count(viewWithoutTip, "\n") + 1
	heightWithTip := strings.Count(viewWithTip, "\n") + 1
	if heightWithTip != heightWithoutTip {
		t.Fatalf("expected overlay tip not to change view height, without=%d with=%d", heightWithoutTip, heightWithTip)
	}
}

func TestConnectingStateBlocksNavigationKeys(t *testing.T) {
	AM = AppModel{}
	am := &AM

	AM.BookmarkInfo.List = []BookmarkItem{
		{Title: "dev", Host: "10.0.0.1", Port: 22, Username: "root", EnableSSH: true},
		{Title: "staging", Host: "10.0.0.2", Port: 22, Username: "root", EnableSSH: true},
	}
	AM.width = 100
	AM.height = 30
	createList()
	AM.list.Select(0)
	AM.isConnecting = true

	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyDown})

	if got := AM.list.GlobalIndex(); got != 0 {
		t.Fatalf("expected selection to stay at 0 while connecting, got %d", got)
	}
}

func TestClearTipMsgOnlyClearsLatestTip(t *testing.T) {
	AM = AppModel{}

	_ = setTip("first", tipInfo)
	seq1 := AM.tipSeq
	_ = setTip("second", tipError)
	seq2 := AM.tipSeq

	if seq2 <= seq1 {
		t.Fatalf("expected tip sequence to increase, seq1=%d seq2=%d", seq1, seq2)
	}

	am := &AM
	_, _ = am.Update(clearTipMsg{Seq: seq1})
	if AM.TipString != "second" {
		t.Fatalf("expected old clear message not to clear latest tip, got %q", AM.TipString)
	}

	_, _ = am.Update(clearTipMsg{Seq: seq2})
	if AM.TipString != "" {
		t.Fatalf("expected latest clear message to clear tip, got %q", AM.TipString)
	}
}

func TestViewUsesWindowSizeForTopRightTipPositioning(t *testing.T) {
	AM = AppModel{}
	am := &AM

	AM.BookmarkInfo.List = []BookmarkItem{
		{Title: "dev", Host: "10.0.0.1", Port: 22, Username: "root", EnableSSH: true},
	}
	AM.width = 120
	AM.height = 30
	createList()
	_ = setTip("tip position test", tipInfo)

	view := am.View()
	if !strings.Contains(view, "tip position test") {
		t.Fatalf("expected tip text in view")
	}

	firstLine := strings.Split(view, "\n")[0]
	leftPadding := len(firstLine) - len(strings.TrimLeft(firstLine, " "))
	if leftPadding < 1 {
		t.Fatalf("expected tip to be placed near full-window right side, got left padding %d", leftPadding)
	}
}

func TestViewShowsCenterLockOverlayWhileConnecting(t *testing.T) {
	AM = AppModel{}
	am := &AM

	AM.BookmarkInfo.List = []BookmarkItem{
		{Title: "dev", Host: "10.0.0.1", Port: 22, Username: "root", EnableSSH: true},
	}
	AM.width = 100
	AM.height = 30
	createList()
	AM.isConnecting = true
	_ = setTip("connecting dev", tipProgress)

	view := am.View()
	if !strings.Contains(view, "Connecting") {
		t.Fatalf("expected center lock overlay while connecting")
	}
	if !strings.Contains(view, "connecting") && !strings.Contains(view, "Connecting") {
		t.Fatalf("expected loading indicator in global overlay")
	}
}

func TestBuildConnectingOverlayUsesAboutEightyPercentArea(t *testing.T) {
	overlay := buildConnectingOverlay(100, 40)
	w, h := lipgloss.Size(overlay)
	if w < 90 || w > 92 {
		t.Fatalf("expected overlay width around 90 (includes border), got %d", w)
	}
	if h < 36 || h > 38 {
		t.Fatalf("expected overlay height around 36 (includes border), got %d", h)
	}
}

func TestTopRightTipRemainsVisibleWhileConnecting(t *testing.T) {
	AM = AppModel{}
	am := &AM

	AM.BookmarkInfo.List = []BookmarkItem{{Title: "dev", Host: "10.0.0.1", Port: 22, Username: "root", EnableSSH: true}}
	AM.width = 100
	AM.height = 30
	createList()
	AM.isConnecting = true
	AM.TipString = "top-right highest layer"
	AM.tipLevel = tipProgress

	view := am.View()
	if !strings.Contains(view, "top-right highest layer") {
		t.Fatalf("expected top-right tip text visible above overlay")
	}
}

func TestBuildConnectingOverlayHasConsistentLineWidths(t *testing.T) {
	overlay := buildConnectingOverlay(100, 40)
	lines := strings.Split(overlay, "\n")
	if len(lines) == 0 {
		t.Fatalf("expected non-empty overlay")
	}
	firstWidth := ansi.StringWidth(lines[0])
	for i, line := range lines {
		w := ansi.StringWidth(line)
		if w != firstWidth {
			t.Fatalf("expected consistent overlay line width, line=%d width=%d first=%d", i, w, firstWidth)
		}
	}
}

func TestPaginationFormatIsExplicitInCurrentLocale(t *testing.T) {
	AM = AppModel{}
	am := &AM
	am.Init()
	AM.locale = localeEN
	AM.GistConfig.Locale = "en"

	AM.BookmarkInfo.List = make([]BookmarkItem, 35)
	for i := range AM.BookmarkInfo.List {
		AM.BookmarkInfo.List[i] = BookmarkItem{Title: "srv", Host: "10.0.0.1", Port: 22, Username: "root", EnableSSH: true}
	}
	AM.width = 80
	AM.height = 24
	createList()

	view := am.View()
	if !strings.Contains(view, "Page") {
		t.Fatalf("expected explicit english page indicator in view")
	}

	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'L'}})
	view = am.View()
	if !strings.Contains(view, "第") || !strings.Contains(view, "页") {
		t.Fatalf("expected explicit chinese page indicator in view after locale toggle")
	}
}

func TestOverlayAtKeepsOutputWidthConstantPerLine(t *testing.T) {
	base := lipgloss.NewStyle().Width(80).Height(20).Render("base")
	overlay := buildConnectingOverlay(72, 18)
	result := overlayCenter(base, overlay)
	lines := strings.Split(result, "\n")
	if len(lines) == 0 {
		t.Fatalf("expected non-empty output")
	}
	expected := ansi.StringWidth(lines[0])
	for i, line := range lines {
		if w := ansi.StringWidth(line); w != expected {
			t.Fatalf("line width drift at line %d: got %d expected %d", i, w, expected)
		}
	}
}

func TestOverlayAtPreservesLineWidthsAfterOverlay(t *testing.T) {
	base := lipgloss.NewStyle().Width(60).Height(10).Render("base")
	overlay := lipgloss.NewStyle().
		Width(50).
		Height(8).
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("252")).
		Render("")

	result := overlayCenter(base, overlay)
	lines := strings.Split(result, "\n")
	if len(lines) == 0 {
		t.Fatalf("expected non-empty overlay result")
	}

	firstWidth := ansi.StringWidth(lines[0])
	for i, line := range lines {
		w := ansi.StringWidth(line)
		if w != firstWidth {
			t.Fatalf("expected aligned line widths, line=%d width=%d first=%d", i, w, firstWidth)
		}
	}
}

func TestOverlayCenterPlacesTextInView(t *testing.T) {
	base := lipgloss.NewStyle().Width(40).Height(12).Render("list")
	overlay := lipgloss.NewStyle().Padding(0, 1).Render("LOCK")
	out := overlayCenter(base, overlay)
	if !strings.Contains(out, "LOCK") {
		t.Fatalf("expected centered overlay text in output")
	}
}

type timeoutTestError struct{}

func (timeoutTestError) Error() string   { return "i/o timeout" }
func (timeoutTestError) Timeout() bool   { return true }
func (timeoutTestError) Temporary() bool { return true }

func TestDescribeConnectErrorLocalizesAndClassifies(t *testing.T) {
	AM = AppModel{}
	AM.locale = localeEN

	if got := describeConnectError(timeoutTestError{}); !strings.Contains(got, "network timeout") || !strings.Contains(got, "i/o timeout") {
		t.Fatalf("expected english timeout detail, got %q", got)
	}
	if got := describeConnectError(errors.New("ssh: unable to authenticate")); !strings.Contains(got, "authentication failed") {
		t.Fatalf("expected english auth detail, got %q", got)
	}

	AM.locale = localeZH
	if got := describeConnectError(&net.DNSError{Err: "no such host", Name: "bad-host"}); !strings.Contains(got, "主机名无法解析") || !strings.Contains(got, "bad-host") {
		t.Fatalf("expected chinese dns detail, got %q", got)
	}
	if got := describeConnectError(errors.New("request remote PTY: ssh: pty-req failed")); !strings.Contains(got, "远端 PTY 分配失败") {
		t.Fatalf("expected chinese pty detail, got %q", got)
	}
}

func TestLanguageToggleWithLKey(t *testing.T) {
	AM = AppModel{}
	am := &AM

	if AM.locale != localeEN {
		t.Fatalf("expected default localeEN, got %v", AM.locale)
	}

	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'L'}})
	if AM.locale != localeZH {
		t.Fatalf("expected localeZH after first l key, got %v", AM.locale)
	}
	if !strings.Contains(AM.TipString, "中文") {
		t.Fatalf("expected Chinese switch confirmation, got %q", AM.TipString)
	}

	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'L'}})
	if AM.locale != localeEN {
		t.Fatalf("expected localeEN after second l key, got %v", AM.locale)
	}
	if !strings.Contains(AM.TipString, "English") {
		t.Fatalf("expected English switch confirmation, got %q", AM.TipString)
	}
}

func TestLocalizedTipAfterLanguageToggle(t *testing.T) {
	AM = AppModel{}
	am := &AM
	AM.Token = ""
	AM.GistID = "gid"

	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	if !strings.Contains(AM.TipString, "token") {
		t.Fatalf("expected english token warning, got %q", AM.TipString)
	}

	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'L'}})
	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	if !strings.Contains(AM.TipString, "未配置 token") {
		t.Fatalf("expected chinese token warning after toggle, got %q", AM.TipString)
	}
}

func TestLanguageToggleHelpHintShownInFooter(t *testing.T) {
	AM = AppModel{}
	am := &AM
	am.Init()
	AM.locale = localeEN
	AM.GistConfig.Locale = "en"

	AM.BookmarkInfo.List = []BookmarkItem{{Title: "dev", Host: "10.0.0.1", Port: 22, Username: "root", EnableSSH: true}}
	AM.width = 100
	AM.height = 30
	createList()
	am.applyListLocale()

	view := am.View()
	if !strings.Contains(view, "L") {
		t.Fatalf("expected help/footer to include L shortcut")
	}
	if !strings.Contains(view, "lang") {
		t.Fatalf("expected english help/footer text for language toggle")
	}

	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'L'}})
	view = am.View()
	if !strings.Contains(view, "语言") {
		t.Fatalf("expected chinese help/footer text after locale toggle")
	}
}

func TestLanguageToggleUpdatesHelpAndStatusBarLocale(t *testing.T) {
	AM = AppModel{}
	am := &AM
	am.Init()
	AM.locale = localeEN
	AM.GistConfig.Locale = "en"

	AM.BookmarkInfo.List = []BookmarkItem{{Title: "dev", Host: "10.0.0.1", Port: 22, Username: "root", EnableSSH: true}}
	AM.width = 100
	AM.height = 30
	createList()
	am.applyListLocale()

	if got := AM.list.KeyMap.Quit.Help().Desc; got != "quit" {
		t.Fatalf("expected default quit help in english, got %q", got)
	}

	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'L'}})
	if !strings.Contains(AM.list.Title, "TTM") || !strings.Contains(AM.list.Title, Version) {
		t.Fatalf("expected title with TTM and version after toggle, got %q", AM.list.Title)
	}

	if got := AM.list.KeyMap.Quit.Help().Desc; got != "退出" {
		t.Fatalf("expected quit help in chinese after toggle, got %q", got)
	}
	if got := AM.list.KeyMap.ShowFullHelp.Help().Desc; got != "更多" {
		t.Fatalf("expected full help label in chinese after toggle, got %q", got)
	}
	if got := AM.list.Paginator.ArabicFormat; got != "第%d/%d页" {
		t.Fatalf("expected chinese pagination format after toggle, got %q", got)
	}
	singular, plural := AM.list.StatusBarItemName()
	if singular != "书签" || plural != "书签" {
		t.Fatalf("expected status bar item labels in chinese, got singular=%q plural=%q", singular, plural)
	}

	view := am.View()
	if !strings.Contains(view, "语言") {
		t.Fatalf("expected rendered help/footer to show chinese language toggle hint")
	}

	_, _ = am.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	view = am.View()
	if !strings.Contains(view, "语言") {
		t.Fatalf("expected chinese help/footer hint to persist after redraw")
	}

	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'L'}})
	if !strings.Contains(AM.list.Title, "TTM") || !strings.Contains(AM.list.Title, Version) {
		t.Fatalf("expected title with version after second toggle, got %q", AM.list.Title)
	}
	if got := AM.list.Paginator.ArabicFormat; got != "Page %d/%d" {
		t.Fatalf("expected english pagination format after second toggle, got %q", got)
	}
}

func TestInitUsesPersistedLocaleFromConfig(t *testing.T) {
	AM = AppModel{}
	am := &AM
	am.GistConfig = GistConfig{Platform: "github", Token: "", GistID: "", Locale: "zh"}

	am.applyPersistedLocale()
	if AM.locale != localeZH {
		t.Fatalf("expected persisted zh locale to restore localeZH, got %v", AM.locale)
	}

	am.GistConfig.Locale = "en"
	am.applyPersistedLocale()
	if AM.locale != localeEN {
		t.Fatalf("expected persisted en locale to restore localeEN, got %v", AM.locale)
	}
}

func TestToggleLocalePersistsSelection(t *testing.T) {
	AM = AppModel{}
	am := &AM
	AM.locale = localeEN
	am.GistConfig = GistConfig{Platform: "github", Token: "", GistID: "", Locale: "en"}

	am.toggleLocale()
	if AM.locale != localeZH {
		t.Fatalf("expected localeZH after toggle, got %v", AM.locale)
	}
	if AM.GistConfig.Locale != "zh" {
		t.Fatalf("expected persisted locale to be zh, got %q", AM.GistConfig.Locale)
	}

	am.toggleLocale()
	if AM.locale != localeEN {
		t.Fatalf("expected localeEN after second toggle, got %v", AM.locale)
	}
	if AM.GistConfig.Locale != "en" {
		t.Fatalf("expected persisted locale to be en, got %q", AM.GistConfig.Locale)
	}
}

// ============================================================================
// FAILING TESTS - These expose visual width drift / line offset issues
// ============================================================================

// TestFullViewWithBothOverlaysLineWidthConsistency tests that when both
// the connecting overlay AND tip overlay are rendered, ALL lines maintain
// identical visual width. This catches drift from sequential overlay stacking.
func TestFullViewWithBothOverlaysLineWidthConsistency(t *testing.T) {
	AM = AppModel{}
	am := &AM

	AM.BookmarkInfo.List = []BookmarkItem{
		{Title: "dev", Host: "10.0.0.1", Port: 22, Username: "root", EnableSSH: true},
		{Title: "staging", Host: "10.0.0.2", Port: 22, Username: "root", EnableSSH: true},
	}
	AM.width = 80
	AM.height = 24
	createList()
	AM.isConnecting = true
	AM.TipString = "test"
	AM.tipLevel = tipProgress

	view := am.View()
	lines := strings.Split(view, "\n")
	if len(lines) == 0 {
		t.Fatalf("expected non-empty view")
	}

	// KEY ASSERTION: Every line must have identical visual width
	// This is what catches the drift - even 1 character offset fails
	referenceWidth := ansi.StringWidth(lines[0])
	for i, line := range lines {
		w := ansi.StringWidth(line)
		if w != referenceWidth {
			t.Fatalf("VISUAL WIDTH DRIFT at line %d: got %d expected %d (diff=%d). Line: %q",
				i, w, referenceWidth, w-referenceWidth, line)
		}
	}
}

// TestTipOverlayXPositionWithConnectingOverlay tests that tip overlay
// appears at consistent x-position even when connecting overlay is present.
// The bug: overlayTopRight calculates position from already-modified base.
func TestTipOverlayXPositionWithConnectingOverlay(t *testing.T) {
	AM = AppModel{}
	am := &AM

	AM.BookmarkInfo.List = []BookmarkItem{
		{Title: "dev", Host: "10.0.0.1", Port: 22, Username: "root", EnableSSH: true},
	}
	AM.width = 100
	AM.height = 30
	createList()
	AM.isConnecting = true
	AM.TipString = "X" // Single char to verify exact position
	AM.tipLevel = tipInfo

	view := am.View()
	lines := strings.Split(view, "\n")

	// Find the tip character
	tipRow := -1
	tipCol := -1
	for row, line := range lines {
		for col, ch := range line {
			if ch == 'X' {
				tipRow = row
				tipCol = col
				break
			}
		}
		if tipRow >= 0 {
			break
		}
	}

	if tipRow < 0 {
		t.Fatalf("tip character 'X' not found in view")
	}

	// Get expected position: frameWidth - tipWidth
	frameWidth, _ := getListSize(AM.width, AM.height)
	// tip overlay with border is ~5 chars wide, so expected x ~ frameWidth - 5
	expectedMinX := frameWidth - 10 // Allow some tolerance for borders

	if tipCol < expectedMinX-5 {
		t.Fatalf("tip x-position drift: got col %d but expected >= %d (frameWidth=%d)",
			tipCol, expectedMinX, frameWidth)
	}
}

// TestOverlayBorderContinuityWithBothOverlays tests that borders remain
// continuous when both overlays are present. Gaps indicate offset issues.
func TestOverlayBorderContinuityWithBothOverlays(t *testing.T) {
	AM = AppModel{}
	am := &AM

	AM.BookmarkInfo.List = []BookmarkItem{
		{Title: "server1", Host: "10.0.0.1", Port: 22, Username: "root", EnableSSH: true},
	}
	AM.width = 80
	AM.height = 24
	createList()
	AM.isConnecting = true
	AM.TipString = "error tip"
	AM.tipLevel = tipError

	view := am.View()
	lines := strings.Split(view, "\n")

	// Check that all lines have consistent width (border continuity)
	refWidth := ansi.StringWidth(lines[0])
	for i, line := range lines {
		w := ansi.StringWidth(line)
		// Allow max 1 char tolerance for edge cases
		if w < refWidth-1 || w > refWidth+1 {
			t.Fatalf("BORDER DISCONTINUITY at line %d: width=%d reference=%d. Line: %q",
				i, w, refWidth, line)
		}
	}
}

// TestViewOutputDimensionsMatchWindowSize verifies the final output
// dimensions match the expected window size - catches margin/frame mismatches.
func TestViewOutputDimensionsMatchWindowSize(t *testing.T) {
	AM = AppModel{}
	am := &AM

	testCases := []struct {
		width  int
		height int
	}{
		{80, 24},
		{100, 30},
		{120, 40},
		{60, 20},
	}

	for _, tc := range testCases {
		AM = AppModel{}
		am = &AM

		AM.BookmarkInfo.List = []BookmarkItem{
			{Title: "test", Host: "1.1.1.1", Port: 22, Username: "root", EnableSSH: true},
		}
		AM.width = tc.width
		AM.height = tc.height
		createList()
		AM.TipString = "tip"
		AM.tipLevel = tipInfo

		view := am.View()
		lines := strings.Split(view, "\n")

		// After docStyle.Render (which adds margin), dimensions should match
		lineWidth := ansi.StringWidth(lines[0])
		if lineWidth != tc.width {
			t.Fatalf("DIMENSION MISMATCH: window=%dx%d but output width=%d",
				tc.width, tc.height, lineWidth)
		}
		if len(lines) != tc.height {
			t.Fatalf("DIMENSION MISMATCH: window=%dx%d but output height=%d",
				tc.width, tc.height, len(lines))
		}
	}
}

// TestViewWithColoredListContent tests the fallback path where frameWidth/frameHeight
// are calculated using lipgloss.Width/Height vs ansi.StringWidth.
// BUG: lipgloss.Width returns different value than ansi.StringWidth for colored content.
func TestViewWithColoredListContent(t *testing.T) {
	AM = AppModel{}
	am := &AM

	AM.BookmarkInfo.List = []BookmarkItem{
		{Title: "dev", Host: "10.0.0.1", Port: 22, Username: "root", EnableSSH: true},
	}
	// Use dimensions that trigger fallback path (width=0)
	// Actually, we need to set width/height to trigger the <=0 check
	// But the normal path uses getListSize which returns positive values
	// Let's test with very small dimensions instead
	AM.width = 10 // Very small - will trigger fallback or weird behavior
	AM.height = 5
	createList()
	AM.TipString = "tip"
	AM.tipLevel = tipInfo

	// This tests the edge case where dimensions are small
	view := am.View()
	lines := strings.Split(view, "\n")

	// The view should still have consistent line widths
	if len(lines) == 0 {
		t.Fatalf("expected non-empty view")
	}

	refWidth := ansi.StringWidth(lines[0])
	for i, line := range lines {
		w := ansi.StringWidth(line)
		if w != refWidth {
			t.Fatalf("line width mismatch at line %d: got %d expected %d", i, w, refWidth)
		}
	}
}

// TestLipglossWidthVsAnsiStringWidthDifference exposes the potential bug
// where lipgloss.Width and ansi.StringWidth return different values for
// the same styled content.
func TestLipglossWidthVsAnsiStringWidthDifference(t *testing.T) {
	// Create styled content with colors (like the list would have)
	styled := lipgloss.NewStyle().
		Foreground(lipgloss.Color("green")).
		Render("test content")

	lipglossW := lipgloss.Width(styled)
	ansiW := ansi.StringWidth(styled)

	t.Logf("lipgloss.Width: %d, ansi.StringWidth: %d", lipglossW, ansiW)

	// They should be equal for plain content, but may differ with styling
	// This test documents the potential issue
	if lipglossW != ansiW {
		t.Logf("WIDTH CALCULATION DIFFERENCE: lipgloss=%d ansi=%d", lipglossW, ansiW)
	}
}

// TestFullRenderPipelineWithRealList tests the complete View() pipeline
// with actual list rendering to catch any drift from the real-world scenario.
// TestFullRenderPipelineWithRealList tests the complete View() pipeline
// with actual list rendering to catch any drift from the real-world scenario.
func TestFullRenderPipelineWithRealList(t *testing.T) {
	testCases := []struct {
		name       string
		width      int
		height     int
		connecting bool
		tip        string
	}{
		{"small", 40, 12, false, "short"},
		{"medium", 80, 24, false, "medium tip"},
		{"large", 120, 40, false, "a longer tip message"},
		{"with_connect_small", 40, 12, true, "connecting..."},
		{"with_connect_medium", 80, 24, true, "connecting..."},
		{"with_connect_large", 120, 40, true, "connecting server..."},
		{"odd_width_81", 81, 24, false, "tip"},
		{"odd_height_25", 80, 25, false, "tip"},
		{"narrow_40", 40, 14, false, "t"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			AM = AppModel{}
			am := &AM

			items := []BookmarkItem{
				{Title: "server1", Host: "10.0.0.1", Port: 22, Username: "root", EnableSSH: true},
				{Title: "server2", Host: "10.0.0.2", Port: 22, Username: "root", EnableSSH: true},
				{Title: "server3", Host: "10.0.0.3", Port: 22, Username: "root", EnableSSH: true},
			}
			AM.BookmarkInfo.List = items
			AM.width = tc.width
			AM.height = tc.height
			createList()
			AM.isConnecting = tc.connecting
			AM.TipString = tc.tip
			AM.tipLevel = tipInfo

			view := am.View()
			lines := strings.Split(view, "\n")

			if len(lines) == 0 {
				t.Fatalf("expected non-empty view for %s", tc.name)
			}

			// CRITICAL: All lines must have consistent visual width
			// This catches overlay drift / line offset issues
			refWidth := ansi.StringWidth(lines[0])
			for i, line := range lines {
				w := ansi.StringWidth(line)
				if w != refWidth {
					t.Fatalf("WIDTH DRIFT at line %d: got %d expected %d (diff=%d). Line: %q",
						i, w, refWidth, w-refWidth, line)
				}
			}

			if len(lines) < tc.height || len(lines) > tc.height+1 {
				t.Fatalf("HEIGHT MISMATCH: window=%dx%d but output=%dx%d",
					tc.width, tc.height, refWidth, len(lines))
			}
			if refWidth != tc.width {
				t.Fatalf("WIDTH MISMATCH: window=%dx%d but output=%dx%d",
					tc.width, tc.height, refWidth, len(lines))
			}
		})
	}
}

func TestAddBookmarkWithCtrlSAddsItemAndPersists(t *testing.T) {
	AM = AppModel{}
	am := &AM
	bookmarkDir := setupBookmarkTempDir(t)

	AM.BookmarkInfo.List = []BookmarkItem{{
		Title:     "dev",
		Host:      "10.0.0.1",
		Username:  "root",
		Port:      22,
		EnableSSH: true,
	}}
	AM.width = 100
	AM.height = 30
	createList()

	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	if AM.editor == nil {
		t.Fatalf("expected add key to open bookmark editor")
	}

	AM.editor.inputs[editorFieldTitle].SetValue("new node")
	AM.editor.inputs[editorFieldHost].SetValue("10.0.0.9")
	AM.editor.inputs[editorFieldUsername].SetValue("ubuntu")
	AM.editor.inputs[editorFieldPort].SetValue("2202")

	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyCtrlS})

	if AM.editor != nil {
		t.Fatalf("expected editor to close after successful save")
	}
	if len(AM.BookmarkInfo.List) != 2 {
		t.Fatalf("expected 2 bookmarks after add, got %d", len(AM.BookmarkInfo.List))
	}
	added := AM.BookmarkInfo.List[1]
	if added.Host != "10.0.0.9" || added.Port != 2202 || added.Username != "ubuntu" {
		t.Fatalf("unexpected added bookmark: %+v", added)
	}

	bookmarkFile := filepath.Join(bookmarkDir, "bookmarks.json")
	data, err := os.ReadFile(bookmarkFile)
	if err != nil {
		t.Fatalf("expected bookmarks file to be written: %v", err)
	}
	if !strings.Contains(string(data), "\"host\":\"10.0.0.9\"") {
		t.Fatalf("expected persisted bookmarks to include new host, got: %s", string(data))
	}
}

func TestEditBookmarkKeepsMaskedSecretsWhenUnchanged(t *testing.T) {
	AM = AppModel{}
	am := &AM
	_ = setupBookmarkTempDir(t)

	AM.BookmarkInfo.List = []BookmarkItem{{
		Title:      "prod",
		Host:       "10.0.0.8",
		Username:   "root",
		Port:       22,
		EnableSSH:  true,
		Password:   "old-password",
		AuthType:   "password",
		PrivateKey: "line1\nline2",
		Passphrase: "old-passphrase",
	}}
	AM.width = 100
	AM.height = 30
	createList()

	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	if AM.editor == nil {
		t.Fatalf("expected edit key to open bookmark editor")
	}

	AM.editor.inputs[editorFieldHost].SetValue("10.0.0.88")
	AM.editor.inputs[editorFieldPassword].SetValue(maskedSecretValue)
	AM.editor.inputs[editorFieldPrivateKey].SetValue(maskedSecretValue)
	AM.editor.inputs[editorFieldPassphrase].SetValue(maskedSecretValue)

	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyCtrlS})

	updated := AM.BookmarkInfo.List[0]
	if updated.Host != "10.0.0.88" {
		t.Fatalf("expected host to be updated, got %q", updated.Host)
	}
	if updated.Password != "old-password" {
		t.Fatalf("expected password to be preserved, got %q", updated.Password)
	}
	if updated.PrivateKey != "line1\nline2" {
		t.Fatalf("expected private key to be preserved, got %q", updated.PrivateKey)
	}
	if updated.Passphrase != "old-passphrase" {
		t.Fatalf("expected passphrase to be preserved, got %q", updated.Passphrase)
	}
}

func TestEditBookmarkEditorDefaultsToMaskedSecretValues(t *testing.T) {
	AM = AppModel{}
	am := &AM
	_ = setupBookmarkTempDir(t)

	AM.BookmarkInfo.List = []BookmarkItem{{
		Title:      "prod",
		Host:       "10.0.0.8",
		Username:   "root",
		Port:       22,
		EnableSSH:  true,
		Password:   "plain-password",
		AuthType:   "private-key",
		PrivateKey: "PRIVATE KEY DATA",
		Passphrase: "plain-passphrase",
	}}
	AM.width = 100
	AM.height = 30
	createList()

	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	if AM.editor == nil {
		t.Fatalf("expected edit key to open bookmark editor")
	}

	if got := AM.editor.inputs[editorFieldPrivateKey].Value(); got != maskedSecretValue {
		t.Fatalf("expected private key to be masked by default, got %q", got)
	}
	if got := AM.editor.inputs[editorFieldPassphrase].Value(); got != maskedSecretValue {
		t.Fatalf("expected passphrase to be masked by default, got %q", got)
	}
}

func TestConfigEditorDefaultsToMaskedTokenValue(t *testing.T) {
	AM = AppModel{}
	am := &AM

	am.GistConfig = GistConfig{
		Platform: "github",
		Token:    "ghp_plain_token",
		GistID:   "gid-1",
		Locale:   "en",
	}
	AM.Platform = "github"
	AM.width = 100
	AM.height = 30
	createList()

	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	if AM.configEditor == nil {
		t.Fatalf("expected config key to open config editor")
	}

	if got := AM.configEditor.inputs[configFieldToken].Value(); got != maskedSecretValue {
		t.Fatalf("expected token to be masked by default, got %q", got)
	}
}

func TestConfigEditorCanToggleTokenVisibilityWithV(t *testing.T) {
	AM = AppModel{}
	am := &AM

	am.GistConfig = GistConfig{
		Platform: "github",
		Token:    "ghp_plain_token",
		GistID:   "gid-1",
		Locale:   "en",
	}
	AM.Platform = "github"
	AM.width = 100
	AM.height = 30
	createList()

	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	if AM.configEditor == nil {
		t.Fatalf("expected config key to open config editor")
	}

	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	if got := AM.configEditor.inputs[configFieldToken].Value(); got != "ghp_plain_token" {
		t.Fatalf("expected token to be shown after first ctrl+r toggle, got %q", got)
	}

	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	if got := AM.configEditor.inputs[configFieldToken].Value(); got != maskedSecretValue {
		t.Fatalf("expected token to be masked after second ctrl+r toggle, got %q", got)
	}
}

func TestBookmarkEditorCanToggleSecretVisibilityWithV(t *testing.T) {
	AM = AppModel{}
	am := &AM
	_ = setupBookmarkTempDir(t)

	AM.BookmarkInfo.List = []BookmarkItem{{
		Title:      "prod",
		Host:       "10.0.0.8",
		Username:   "root",
		Port:       22,
		EnableSSH:  true,
		AuthType:   "private-key",
		PrivateKey: "-----BEGIN OPENSSH PRIVATE KEY-----\nline-1\nline-2\n-----END OPENSSH PRIVATE KEY-----",
		Passphrase: "plain-passphrase",
	}}
	AM.width = 100
	AM.height = 30
	createList()

	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	if AM.editor == nil {
		t.Fatalf("expected edit key to open bookmark editor")
	}

	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	if got := AM.editor.inputs[editorFieldPrivateKey].Value(); !strings.Contains(got, "BEGIN OPENSSH PRIVATE KEY") || !strings.Contains(got, "line-2") || !strings.Contains(got, "END OPENSSH PRIVATE KEY") {
		t.Fatalf("expected full private key markers after first ctrl+r toggle, got %q", got)
	}
	if got := AM.editor.inputs[editorFieldPassphrase].Value(); got != "plain-passphrase" {
		t.Fatalf("expected passphrase to be shown after first ctrl+r toggle, got %q", got)
	}

	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	if got := AM.editor.inputs[editorFieldPrivateKey].Value(); got != maskedSecretValue {
		t.Fatalf("expected private key masked after second ctrl+r toggle, got %q", got)
	}
	if got := AM.editor.inputs[editorFieldPassphrase].Value(); got != maskedSecretValue {
		t.Fatalf("expected passphrase masked after second ctrl+r toggle, got %q", got)
	}

	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	if got := AM.editor.inputs[editorFieldPrivateKey].Value(); !strings.Contains(got, "BEGIN OPENSSH PRIVATE KEY") || !strings.Contains(got, "line-2") || !strings.Contains(got, "END OPENSSH PRIVATE KEY") {
		t.Fatalf("expected full private key markers restored after third ctrl+r toggle, got %q", got)
	}
}

func TestBookmarkEditorSaveAfterToggleKeepsFullPrivateKey(t *testing.T) {
	AM = AppModel{}
	am := &AM
	_ = setupBookmarkTempDir(t)

	originalKey := "-----BEGIN OPENSSH PRIVATE KEY-----\nAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA\nTAIL-MARKER-LINE\n-----END OPENSSH PRIVATE KEY-----"
	AM.BookmarkInfo.List = []BookmarkItem{{
		Title:      "prod",
		Host:       "10.0.0.8",
		Username:   "root",
		Port:       22,
		EnableSSH:  true,
		AuthType:   "private-key",
		PrivateKey: originalKey,
		Passphrase: "plain-passphrase",
	}}
	AM.width = 100
	AM.height = 30
	createList()

	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyCtrlS})

	if got := AM.BookmarkInfo.List[0].PrivateKey; got != originalKey {
		t.Fatalf("expected private key kept intact after toggle+save, got %q", got)
	}
}

func TestBookmarkEditorShowModeRendersFullPrivateKeyPreview(t *testing.T) {
	AM = AppModel{}
	am := &AM
	_ = setupBookmarkTempDir(t)

	originalKey := "-----BEGIN OPENSSH PRIVATE KEY-----\nline-1\nline-2\n-----END OPENSSH PRIVATE KEY-----"
	AM.BookmarkInfo.List = []BookmarkItem{{
		Title:      "prod",
		Host:       "10.0.0.8",
		Username:   "root",
		Port:       22,
		EnableSSH:  true,
		AuthType:   "private-key",
		PrivateKey: originalKey,
		Passphrase: "plain-passphrase",
	}}
	AM.width = 100
	AM.height = 30
	createList()

	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyCtrlR})

	view := am.View()
	if !strings.Contains(view, "line-1\\nline-2") || !strings.Contains(view, "END OPENSSH PRIVATE KEY") {
		t.Fatalf("expected private key preview with escaped newlines in view, got %q", view)
	}
}

func TestPrivateKeyPreviewKeepsMultilineAfterEnterInShowMode(t *testing.T) {
	AM = AppModel{}
	am := &AM
	_ = setupBookmarkTempDir(t)

	originalKey := "-----BEGIN OPENSSH PRIVATE KEY-----\nline-1\nline-2\n-----END OPENSSH PRIVATE KEY-----"
	AM.BookmarkInfo.List = []BookmarkItem{{
		Title:      "prod",
		Host:       "10.0.0.8",
		Username:   "root",
		Port:       22,
		EnableSSH:  true,
		AuthType:   "private-key",
		PrivateKey: originalKey,
		Passphrase: "plain-passphrase",
	}}
	AM.width = 100
	AM.height = 30
	createList()

	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyCtrlR})

	for i := 0; i < 6; i++ {
		_, _ = am.Update(tea.KeyMsg{Type: tea.KeyTab})
	}
	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyEnter})

	view := am.View()
	if strings.Contains(view, "line-1 line-2") {
		t.Fatalf("expected multiline private key preview after enter, got folded single-line view: %q", view)
	}
	if !strings.Contains(view, "line-1") || !strings.Contains(view, "line-2") {
		t.Fatalf("expected multiline private key preview after enter, got %q", view)
	}
}

func TestBookmarkEditorCtrlYCopiesFullPrivateKey(t *testing.T) {
	AM = AppModel{}
	am := &AM
	_ = setupBookmarkTempDir(t)

	originalWrite := clipboardWriteAll
	defer func() { clipboardWriteAll = originalWrite }()
	copied := ""
	clipboardWriteAll = func(text string) error {
		copied = text
		return nil
	}

	originalKey := "-----BEGIN OPENSSH PRIVATE KEY-----\nline-1\nline-2\n-----END OPENSSH PRIVATE KEY-----"
	AM.BookmarkInfo.List = []BookmarkItem{{
		Title:      "prod",
		Host:       "10.0.0.8",
		Username:   "root",
		Port:       22,
		EnableSSH:  true,
		AuthType:   "private-key",
		PrivateKey: originalKey,
	}}
	AM.width = 100
	AM.height = 30
	createList()

	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	for i := 0; i < 6; i++ {
		_, _ = am.Update(tea.KeyMsg{Type: tea.KeyTab})
	}
	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyCtrlY})

	if copied != originalKey {
		t.Fatalf("expected ctrl+y copy full private key, got %q", copied)
	}
	if !strings.Contains(AM.TipString, "chars") {
		t.Fatalf("expected copy tip to include character count, got %q", AM.TipString)
	}
}

func TestBookmarkEditorCtrlYCopiesPrivateKeyAfterEnter(t *testing.T) {
	AM = AppModel{}
	am := &AM
	_ = setupBookmarkTempDir(t)

	originalWrite := clipboardWriteAll
	defer func() { clipboardWriteAll = originalWrite }()
	copied := ""
	clipboardWriteAll = func(text string) error {
		copied = text
		return nil
	}

	originalKey := "-----BEGIN OPENSSH PRIVATE KEY-----\nline-1\nline-2\n-----END OPENSSH PRIVATE KEY-----"
	AM.BookmarkInfo.List = []BookmarkItem{{
		Title:      "prod",
		Host:       "10.0.0.8",
		Username:   "root",
		Port:       22,
		EnableSSH:  true,
		AuthType:   "private-key",
		PrivateKey: originalKey,
	}}
	AM.width = 100
	AM.height = 30
	createList()

	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	for i := 0; i < 6; i++ {
		_, _ = am.Update(tea.KeyMsg{Type: tea.KeyTab})
	}
	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyEnter})
	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyCtrlY})

	if copied != originalKey+"\n" {
		t.Fatalf("expected ctrl+y copy appends newline after enter in private key field, got %q", copied)
	}
}

func TestBookmarkEditorCtrlVPastesPrivateKeyWithNewlines(t *testing.T) {
	AM = AppModel{}
	am := &AM
	_ = setupBookmarkTempDir(t)

	originalRead := clipboardReadAll
	defer func() { clipboardReadAll = originalRead }()
	clipboardReadAll = func() (string, error) {
		return "-----BEGIN OPENSSH PRIVATE KEY-----\nline-1\nline-2\n-----END OPENSSH PRIVATE KEY-----", nil
	}

	originalWrite := clipboardWriteAll
	defer func() { clipboardWriteAll = originalWrite }()
	copied := ""
	clipboardWriteAll = func(text string) error {
		copied = text
		return nil
	}

	AM.BookmarkInfo.List = []BookmarkItem{{
		Title:      "prod",
		Host:       "10.0.0.8",
		Username:   "root",
		Port:       22,
		EnableSSH:  true,
		AuthType:   "private-key",
		PrivateKey: "",
	}}
	AM.width = 100
	AM.height = 30
	createList()

	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	for i := 0; i < 6; i++ {
		_, _ = am.Update(tea.KeyMsg{Type: tea.KeyTab})
	}
	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyCtrlV})
	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyCtrlY})

	expected := "-----BEGIN OPENSSH PRIVATE KEY-----\nline-1\nline-2\n-----END OPENSSH PRIVATE KEY-----"
	if copied != expected {
		t.Fatalf("expected ctrl+v paste to preserve newlines for private key, got %q", copied)
	}
}

func TestBookmarkEditorPasteMultilinePrivateKeyKeepsNewlines(t *testing.T) {
	AM = AppModel{}
	am := &AM
	_ = setupBookmarkTempDir(t)

	originalWrite := clipboardWriteAll
	defer func() { clipboardWriteAll = originalWrite }()
	copied := ""
	clipboardWriteAll = func(text string) error {
		copied = text
		return nil
	}

	AM.BookmarkInfo.List = []BookmarkItem{{
		Title:      "prod",
		Host:       "10.0.0.8",
		Username:   "root",
		Port:       22,
		EnableSSH:  true,
		AuthType:   "private-key",
		PrivateKey: "",
	}}
	AM.width = 100
	AM.height = 30
	createList()

	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	for i := 0; i < 6; i++ {
		_, _ = am.Update(tea.KeyMsg{Type: tea.KeyTab})
	}

	pasted := "-----BEGIN OPENSSH PRIVATE KEY-----\nline-1\nline-2\n-----END OPENSSH PRIVATE KEY-----"
	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(pasted)})
	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyCtrlY})

	if copied != pasted {
		t.Fatalf("expected pasted multiline private key to preserve newlines, got %q", copied)
	}
}

func TestBookmarkEditorPasteSequenceKeepsNewlinesInPrivateKey(t *testing.T) {
	AM = AppModel{}
	am := &AM
	_ = setupBookmarkTempDir(t)

	originalWrite := clipboardWriteAll
	defer func() { clipboardWriteAll = originalWrite }()
	copied := ""
	clipboardWriteAll = func(text string) error {
		copied = text
		return nil
	}

	AM.BookmarkInfo.List = []BookmarkItem{{
		Title:      "prod",
		Host:       "10.0.0.8",
		Username:   "root",
		Port:       22,
		EnableSSH:  true,
		AuthType:   "private-key",
		PrivateKey: "",
	}}
	AM.width = 100
	AM.height = 30
	createList()

	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	for i := 0; i < 6; i++ {
		_, _ = am.Update(tea.KeyMsg{Type: tea.KeyTab})
	}

	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("line-1"), Paste: true})
	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyEnter, Paste: true})
	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("line-2"), Paste: true})
	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyCtrlY})

	if copied != "line-1\nline-2" {
		t.Fatalf("expected pasted key sequence to preserve newline in private key, got %q", copied)
	}
}

func TestBookmarkEditorCollapsedPEMInputKeepsSpaces(t *testing.T) {
	AM = AppModel{}
	am := &AM
	_ = setupBookmarkTempDir(t)

	originalWrite := clipboardWriteAll
	defer func() { clipboardWriteAll = originalWrite }()
	copied := ""
	clipboardWriteAll = func(text string) error {
		copied = text
		return nil
	}

	AM.BookmarkInfo.List = []BookmarkItem{{
		Title:      "prod",
		Host:       "10.0.0.8",
		Username:   "root",
		Port:       22,
		EnableSSH:  true,
		AuthType:   "private-key",
		PrivateKey: "",
	}}
	AM.width = 100
	AM.height = 30
	createList()

	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	for i := 0; i < 6; i++ {
		_, _ = am.Update(tea.KeyMsg{Type: tea.KeyTab})
	}

	collapsed := "-----BEGIN OPENSSH PRIVATE KEY----- AAAAB3NzaC1yc2EAAAADAQABAAABAQC11111111111111111111 BBBB3NzaC1yc2EAAAADAQABAAABAQC22222222222222222222 -----END OPENSSH PRIVATE KEY-----"
	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(collapsed)})
	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyCtrlY})

	if copied != collapsed {
		t.Fatalf("expected collapsed pem input to keep spaces unchanged, got %q", copied)
	}
}

func TestBookmarkEditorNewlineRuneSequenceKeepsNewlines(t *testing.T) {
	AM = AppModel{}
	am := &AM
	_ = setupBookmarkTempDir(t)

	originalWrite := clipboardWriteAll
	defer func() { clipboardWriteAll = originalWrite }()
	copied := ""
	clipboardWriteAll = func(text string) error {
		copied = text
		return nil
	}

	AM.BookmarkInfo.List = []BookmarkItem{{
		Title:      "prod",
		Host:       "10.0.0.8",
		Username:   "root",
		Port:       22,
		EnableSSH:  true,
		AuthType:   "private-key",
		PrivateKey: "",
	}}
	AM.width = 100
	AM.height = 30
	createList()

	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	for i := 0; i < 6; i++ {
		_, _ = am.Update(tea.KeyMsg{Type: tea.KeyTab})
	}

	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("line-1")})
	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'\n'}})
	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("line-2")})
	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyCtrlY})

	if copied != "line-1\nline-2" {
		t.Fatalf("expected newline-rune sequence to preserve newline, got %q", copied)
	}
}

func TestBookmarkEditorEnterSequenceKeepsNewlines(t *testing.T) {
	AM = AppModel{}
	am := &AM
	_ = setupBookmarkTempDir(t)

	originalWrite := clipboardWriteAll
	defer func() { clipboardWriteAll = originalWrite }()
	copied := ""
	clipboardWriteAll = func(text string) error {
		copied = text
		return nil
	}

	AM.BookmarkInfo.List = []BookmarkItem{{
		Title:      "prod",
		Host:       "10.0.0.8",
		Username:   "root",
		Port:       22,
		EnableSSH:  true,
		AuthType:   "private-key",
		PrivateKey: "",
	}}
	AM.width = 100
	AM.height = 30
	createList()

	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	for i := 0; i < 6; i++ {
		_, _ = am.Update(tea.KeyMsg{Type: tea.KeyTab})
	}

	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("line-1")})
	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyEnter})
	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("line-2")})
	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyCtrlY})

	if copied != "line-1\nline-2" {
		t.Fatalf("expected enter sequence to preserve newline, got %q", copied)
	}
}

func TestBookmarkEditorPrivateKeyFocusedShowsEscapedNewlineAndCursor(t *testing.T) {
	AM = AppModel{}
	am := &AM
	_ = setupBookmarkTempDir(t)

	AM.BookmarkInfo.List = []BookmarkItem{{
		Title:      "prod",
		Host:       "10.0.0.8",
		Username:   "root",
		Port:       22,
		EnableSSH:  true,
		AuthType:   "private-key",
		PrivateKey: "line-1\nline-2",
	}}
	AM.width = 100
	AM.height = 30
	createList()

	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	for i := 0; i < 6; i++ {
		_, _ = am.Update(tea.KeyMsg{Type: tea.KeyTab})
	}

	view := am.View()
	if !strings.Contains(view, "line-1\\nline-2") {
		t.Fatalf("expected focused private key to display escaped newline form, got %q", view)
	}
}

func TestBookmarkEditorPrivateKeyFieldShowsPathHint(t *testing.T) {
	AM = AppModel{}
	am := &AM
	_ = setupBookmarkTempDir(t)

	AM.BookmarkInfo.List = []BookmarkItem{{
		Title:      "prod",
		Host:       "10.0.0.8",
		Username:   "root",
		Port:       22,
		EnableSSH:  true,
		AuthType:   "private-key",
		PrivateKey: "",
	}}
	AM.width = 100
	AM.height = 30
	createList()

	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	view := am.View()
	if !strings.Contains(view, "file path") {
		t.Fatalf("expected private key field hint to mention file path, got %q", view)
	}
}

func TestBookmarkEditorPrivateKeyBackspaceRespectsCursorPosition(t *testing.T) {
	AM = AppModel{}
	am := &AM
	_ = setupBookmarkTempDir(t)

	originalWrite := clipboardWriteAll
	defer func() { clipboardWriteAll = originalWrite }()
	copied := ""
	clipboardWriteAll = func(text string) error {
		copied = text
		return nil
	}

	AM.BookmarkInfo.List = []BookmarkItem{{
		Title:      "prod",
		Host:       "10.0.0.8",
		Username:   "root",
		Port:       22,
		EnableSSH:  true,
		AuthType:   "private-key",
		PrivateKey: "abcdef",
	}}
	AM.width = 100
	AM.height = 30
	createList()

	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	for i := 0; i < 6; i++ {
		_, _ = am.Update(tea.KeyMsg{Type: tea.KeyTab})
	}

	AM.editor.inputs[editorFieldPrivateKey].SetCursor(3)
	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyCtrlY})

	if copied != "abdef" {
		t.Fatalf("expected backspace to delete relative to cursor, got %q", copied)
	}
}

func TestBookmarkEditorSaveSyncsPrivateKeyFromInput(t *testing.T) {
	AM = AppModel{}
	am := &AM
	_ = setupBookmarkTempDir(t)

	AM.BookmarkInfo.List = []BookmarkItem{{
		Title:      "prod",
		Host:       "10.0.0.8",
		Username:   "root",
		Port:       22,
		EnableSSH:  true,
		AuthType:   "private-key",
		PrivateKey: "old-key",
	}}
	AM.width = 100
	AM.height = 30
	createList()

	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	for i := 0; i < 6; i++ {
		_, _ = am.Update(tea.KeyMsg{Type: tea.KeyTab})
	}

	AM.editor.inputs[editorFieldPrivateKey].SetValue("-----BEGIN OPENSSH PRIVATE KEY-----\\nNEWLINE-1\\nNEWLINE-2\\n-----END OPENSSH PRIVATE KEY-----")
	AM.editor.secretValues[editorFieldPrivateKey] = "old-key"

	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyCtrlS})

	expected := "-----BEGIN OPENSSH PRIVATE KEY-----\nNEWLINE-1\nNEWLINE-2\n-----END OPENSSH PRIVATE KEY-----"
	if got := AM.BookmarkInfo.List[0].PrivateKey; got != expected {
		t.Fatalf("expected save to sync private key from input, got %q", got)
	}
}

func TestBookmarkEditorSavePrefersMultilineSecretOverSpaceFlattenedInput(t *testing.T) {
	AM = AppModel{}
	am := &AM
	_ = setupBookmarkTempDir(t)

	AM.BookmarkInfo.List = []BookmarkItem{{
		Title:      "prod",
		Host:       "10.0.0.8",
		Username:   "root",
		Port:       22,
		EnableSSH:  true,
		AuthType:   "private-key",
		PrivateKey: "",
	}}
	AM.width = 100
	AM.height = 30
	createList()

	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	for i := 0; i < 6; i++ {
		_, _ = am.Update(tea.KeyMsg{Type: tea.KeyTab})
	}

	multiline := "-----BEGIN OPENSSH PRIVATE KEY-----\nline-1\nline-2\n-----END OPENSSH PRIVATE KEY-----"
	AM.editor.secretValues[editorFieldPrivateKey] = multiline
	AM.editor.inputs[editorFieldPrivateKey].SetValue("-----BEGIN OPENSSH PRIVATE KEY----- line-1 line-2 -----END OPENSSH PRIVATE KEY-----")

	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyCtrlS})

	if got := AM.BookmarkInfo.List[0].PrivateKey; got != multiline {
		t.Fatalf("expected save to preserve multiline private key instead of flattened spaces, got %q", got)
	}
}

func TestBookmarkEditorSaveLoadsPrivateKeyFromFilePath(t *testing.T) {
	AM = AppModel{}
	am := &AM
	dir := setupBookmarkTempDir(t)

	keyPath := filepath.Join(dir, "id_test_key")
	keyContent := "-----BEGIN OPENSSH PRIVATE KEY-----\r\nline-1\r\nline-2\r\n-----END OPENSSH PRIVATE KEY-----\r\n"
	if err := os.WriteFile(keyPath, []byte(keyContent), 0600); err != nil {
		t.Fatalf("failed writing test key file: %v", err)
	}

	AM.BookmarkInfo.List = []BookmarkItem{{
		Title:      "prod",
		Host:       "10.0.0.8",
		Username:   "root",
		Port:       22,
		EnableSSH:  true,
		AuthType:   "private-key",
		PrivateKey: "old",
	}}
	AM.width = 100
	AM.height = 30
	createList()

	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	for i := 0; i < 6; i++ {
		_, _ = am.Update(tea.KeyMsg{Type: tea.KeyTab})
	}
	AM.editor.inputs[editorFieldPrivateKey].SetValue(keyPath)
	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyCtrlS})

	expected := "-----BEGIN OPENSSH PRIVATE KEY-----\nline-1\nline-2\n-----END OPENSSH PRIVATE KEY-----\n"
	if got := AM.BookmarkInfo.List[0].PrivateKey; got != expected {
		t.Fatalf("expected private key loaded from file, got %q", got)
	}
	if !strings.Contains(AM.TipString, "loaded from file path") {
		t.Fatalf("expected tip to mention file-path load, got %q", AM.TipString)
	}
}

func TestBookmarkEditorSaveUnreadablePrivateKeyPathKeepsText(t *testing.T) {
	AM = AppModel{}
	am := &AM
	dir := setupBookmarkTempDir(t)

	missingPath := filepath.Join(dir, "no_such_private_key_file")

	AM.BookmarkInfo.List = []BookmarkItem{{
		Title:      "prod",
		Host:       "10.0.0.8",
		Username:   "root",
		Port:       22,
		EnableSSH:  true,
		AuthType:   "private-key",
		PrivateKey: "old",
	}}
	AM.width = 100
	AM.height = 30
	createList()

	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	for i := 0; i < 6; i++ {
		_, _ = am.Update(tea.KeyMsg{Type: tea.KeyTab})
	}
	AM.editor.inputs[editorFieldPrivateKey].SetValue(missingPath)
	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyCtrlS})

	if got := AM.BookmarkInfo.List[0].PrivateKey; got != missingPath {
		t.Fatalf("expected unreadable path text kept as private key, got %q", got)
	}
	if !strings.Contains(AM.TipString, "path unreadable") {
		t.Fatalf("expected tip to mention unreadable path, got %q", AM.TipString)
	}
}

func TestBookmarkEditorSaveTooLargePrivateKeyFileKeepsPathText(t *testing.T) {
	AM = AppModel{}
	am := &AM
	dir := setupBookmarkTempDir(t)

	keyPath := filepath.Join(dir, "id_large_key")
	large := strings.Repeat("A", int(maxPrivateKeyFileBytes)+1)
	if err := os.WriteFile(keyPath, []byte(large), 0600); err != nil {
		t.Fatalf("failed writing large test key file: %v", err)
	}

	AM.BookmarkInfo.List = []BookmarkItem{{
		Title:      "prod",
		Host:       "10.0.0.8",
		Username:   "root",
		Port:       22,
		EnableSSH:  true,
		AuthType:   "private-key",
		PrivateKey: "old",
	}}
	AM.width = 100
	AM.height = 30
	createList()

	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	for i := 0; i < 6; i++ {
		_, _ = am.Update(tea.KeyMsg{Type: tea.KeyTab})
	}
	AM.editor.inputs[editorFieldPrivateKey].SetValue(keyPath)
	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyCtrlS})

	if got := AM.BookmarkInfo.List[0].PrivateKey; got != keyPath {
		t.Fatalf("expected oversize file path text kept as private key, got %q", got)
	}
	if !strings.Contains(AM.TipString, "file is too large") {
		t.Fatalf("expected tip to mention oversize file, got %q", AM.TipString)
	}
}

func TestBookmarkEditorPasteThenSaveKeepsPrivateKeyNewlines(t *testing.T) {
	AM = AppModel{}
	am := &AM
	_ = setupBookmarkTempDir(t)

	AM.BookmarkInfo.List = []BookmarkItem{{
		Title:      "prod",
		Host:       "10.0.0.8",
		Username:   "root",
		Port:       22,
		EnableSSH:  true,
		AuthType:   "private-key",
		PrivateKey: "",
	}}
	AM.width = 100
	AM.height = 30
	createList()

	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	for i := 0; i < 6; i++ {
		_, _ = am.Update(tea.KeyMsg{Type: tea.KeyTab})
	}

	pasted := "-----BEGIN OPENSSH PRIVATE KEY-----\nline-1\nline-2\n-----END OPENSSH PRIVATE KEY-----"
	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(pasted)})
	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyCtrlS})

	if got := AM.BookmarkInfo.List[0].PrivateKey; got != pasted {
		t.Fatalf("expected pasted private key newlines kept after direct save, got %q", got)
	}
}

func TestBookmarkEditorPasteSequenceThenSaveKeepsPrivateKeyNewlines(t *testing.T) {
	AM = AppModel{}
	am := &AM
	_ = setupBookmarkTempDir(t)

	AM.BookmarkInfo.List = []BookmarkItem{{
		Title:      "prod",
		Host:       "10.0.0.8",
		Username:   "root",
		Port:       22,
		EnableSSH:  true,
		AuthType:   "private-key",
		PrivateKey: "",
	}}
	AM.width = 100
	AM.height = 30
	createList()

	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	for i := 0; i < 6; i++ {
		_, _ = am.Update(tea.KeyMsg{Type: tea.KeyTab})
	}

	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("-----BEGIN OPENSSH PRIVATE KEY-----"), Paste: true})
	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyEnter, Paste: true})
	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("line-1"), Paste: true})
	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyEnter, Paste: true})
	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("line-2"), Paste: true})
	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyEnter, Paste: true})
	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("-----END OPENSSH PRIVATE KEY-----"), Paste: true})
	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyCtrlS})

	expected := "-----BEGIN OPENSSH PRIVATE KEY-----\nline-1\nline-2\n-----END OPENSSH PRIVATE KEY-----"
	if got := AM.BookmarkInfo.List[0].PrivateKey; got != expected {
		t.Fatalf("expected pasted sequence private key newlines kept after direct save, got %q", got)
	}
}

func TestBookmarkEditorPasteMultipleNewlinesKeepsExactTextOnCopyAndSave(t *testing.T) {
	AM = AppModel{}
	am := &AM
	_ = setupBookmarkTempDir(t)

	originalWrite := clipboardWriteAll
	defer func() { clipboardWriteAll = originalWrite }()
	copied := ""
	clipboardWriteAll = func(text string) error {
		copied = text
		return nil
	}

	AM.BookmarkInfo.List = []BookmarkItem{{
		Title:      "prod",
		Host:       "10.0.0.8",
		Username:   "root",
		Port:       22,
		EnableSSH:  true,
		AuthType:   "private-key",
		PrivateKey: "",
	}}
	AM.width = 100
	AM.height = 30
	createList()

	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	for i := 0; i < 6; i++ {
		_, _ = am.Update(tea.KeyMsg{Type: tea.KeyTab})
	}

	pasted := "-----BEGIN OPENSSH PRIVATE KEY-----\nline-1\n\nline-2\n\n\nline-3\n-----END OPENSSH PRIVATE KEY-----\n"
	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(pasted)})
	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyCtrlY})

	if copied != pasted {
		t.Fatalf("expected ctrl+y to keep exact multiple-newline private key, got %q", copied)
	}

	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	if got := AM.BookmarkInfo.List[0].PrivateKey; got != pasted {
		t.Fatalf("expected save to keep exact multiple-newline private key, got %q", got)
	}
}

func TestBookmarkEditorSaveNormalizesEscapedPrivateKeyNewlines(t *testing.T) {
	AM = AppModel{}
	am := &AM
	_ = setupBookmarkTempDir(t)

	AM.BookmarkInfo.List = []BookmarkItem{{
		Title:      "prod",
		Host:       "10.0.0.8",
		Username:   "root",
		Port:       22,
		EnableSSH:  true,
		AuthType:   "private-key",
		PrivateKey: "",
	}}
	AM.width = 100
	AM.height = 30
	createList()

	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	for i := 0; i < 6; i++ {
		_, _ = am.Update(tea.KeyMsg{Type: tea.KeyTab})
	}

	AM.editor.secretValues[editorFieldPrivateKey] = "-----BEGIN OPENSSH PRIVATE KEY-----\\nline-1\\nline-2\\n-----END OPENSSH PRIVATE KEY-----"
	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyCtrlS})

	expected := "-----BEGIN OPENSSH PRIVATE KEY-----\nline-1\nline-2\n-----END OPENSSH PRIVATE KEY-----"
	if got := AM.BookmarkInfo.List[0].PrivateKey; got != expected {
		t.Fatalf("expected save to normalize escaped private key newlines, got %q", got)
	}
}

func TestBookmarkEditorCollapsedPEMKeepsSpaces(t *testing.T) {
	AM = AppModel{}
	am := &AM
	_ = setupBookmarkTempDir(t)

	AM.BookmarkInfo.List = []BookmarkItem{{
		Title:      "prod",
		Host:       "10.0.0.8",
		Username:   "root",
		Port:       22,
		EnableSSH:  true,
		AuthType:   "private-key",
		PrivateKey: "",
	}}
	AM.width = 100
	AM.height = 30
	createList()

	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	for i := 0; i < 6; i++ {
		_, _ = am.Update(tea.KeyMsg{Type: tea.KeyTab})
	}

	collapsed := "-----BEGIN OPENSSH PRIVATE KEY----- AAAAB3NzaC1yc2EAAAADAQABAAABAQC11111111111111111111  BBBB3NzaC1yc2EAAAADAQABAAABAQC22222222222222222222   CCCC3NzaC1yc2EAAAADAQABAAABAQC33333333333333333333 -----END OPENSSH PRIVATE KEY-----"
	AM.editor.inputs[editorFieldPrivateKey].SetValue(collapsed)
	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyCtrlS})

	expected := collapsed
	if got := AM.BookmarkInfo.List[0].PrivateKey; got != expected {
		t.Fatalf("expected collapsed pem to keep spaces unchanged, got %q", got)
	}
}

func TestBookmarkEditorCollapsedTextWithSpacesKeepsSpaces(t *testing.T) {
	AM = AppModel{}
	am := &AM
	_ = setupBookmarkTempDir(t)

	AM.BookmarkInfo.List = []BookmarkItem{{
		Title:      "prod",
		Host:       "10.0.0.8",
		Username:   "root",
		Port:       22,
		EnableSSH:  true,
		AuthType:   "private-key",
		PrivateKey: "",
	}}
	AM.width = 100
	AM.height = 30
	createList()

	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	for i := 0; i < 6; i++ {
		_, _ = am.Update(tea.KeyMsg{Type: tea.KeyTab})
	}

	plainText := "word1 word2  word3"
	AM.editor.inputs[editorFieldPrivateKey].SetValue(plainText)
	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyCtrlS})

	if got := AM.BookmarkInfo.List[0].PrivateKey; got != plainText {
		t.Fatalf("expected spaces to remain unchanged, got %q", got)
	}
}

func TestBookmarkEditorCollapsedPEMWithShortChunksKeepsSpaces(t *testing.T) {
	AM = AppModel{}
	am := &AM
	_ = setupBookmarkTempDir(t)

	AM.BookmarkInfo.List = []BookmarkItem{{
		Title:      "prod",
		Host:       "10.0.0.8",
		Username:   "root",
		Port:       22,
		EnableSSH:  true,
		AuthType:   "private-key",
		PrivateKey: "",
	}}
	AM.width = 100
	AM.height = 30
	createList()

	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	for i := 0; i < 6; i++ {
		_, _ = am.Update(tea.KeyMsg{Type: tea.KeyTab})
	}

	collapsed := "-----BEGIN OPENSSH PRIVATE KEY----- AAAA BBBB CCCC DDDD EEEE FFFF GGGG HHHH IIII JJJJ KKKK LLLL MMMM NNNN OOOO PPPP QQQQ RRRR SSSS TTTT -----END OPENSSH PRIVATE KEY-----"
	AM.editor.inputs[editorFieldPrivateKey].SetValue(collapsed)
	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyCtrlS})

	expected := collapsed
	if got := AM.BookmarkInfo.List[0].PrivateKey; got != expected {
		t.Fatalf("expected collapsed pem with short chunks to keep spaces unchanged, got %q", got)
	}
}

func TestBookmarkEditorCtrlUClearsFocusedField(t *testing.T) {
	AM = AppModel{}
	am := &AM
	_ = setupBookmarkTempDir(t)

	AM.BookmarkInfo.List = []BookmarkItem{{
		Title:      "prod",
		Host:       "10.0.0.8",
		Username:   "root",
		Port:       22,
		EnableSSH:  true,
		AuthType:   "private-key",
		PrivateKey: "line-1\nline-2",
	}}
	AM.width = 100
	AM.height = 30
	createList()

	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	for i := 0; i < 6; i++ {
		_, _ = am.Update(tea.KeyMsg{Type: tea.KeyTab})
	}
	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyCtrlU})

	if got := AM.editor.fieldValueForCopy(editorFieldPrivateKey); got != "" {
		t.Fatalf("expected ctrl+u to clear focused private key field, got %q", got)
	}
}

func TestBookmarkEditorCtrlUStringPathClearsFocusedField(t *testing.T) {
	AM = AppModel{}
	am := &AM
	_ = setupBookmarkTempDir(t)

	AM.BookmarkInfo.List = []BookmarkItem{{
		Title:      "prod",
		Host:       "10.0.0.8",
		Username:   "root",
		Port:       22,
		EnableSSH:  true,
		AuthType:   "private-key",
		PrivateKey: "line-1\nline-2",
	}}
	AM.width = 100
	AM.height = 30
	createList()

	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	for i := 0; i < 6; i++ {
		_, _ = am.Update(tea.KeyMsg{Type: tea.KeyTab})
	}
	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{0x15}})

	if got := AM.editor.fieldValueForCopy(editorFieldPrivateKey); got != "" {
		t.Fatalf("expected ctrl+u rune path to clear focused private key field, got %q", got)
	}
}

func TestConfigEditorCtrlUClearsFocusedField(t *testing.T) {
	AM = AppModel{}
	am := &AM

	am.GistConfig = GistConfig{Platform: "github", Token: "ghp_plain_token", GistID: "gid-1", Locale: "en"}
	AM.Platform = "github"
	AM.width = 100
	AM.height = 30
	createList()

	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyCtrlU})

	if got := AM.configEditor.fieldValueForCopy(configFieldToken); got != "" {
		t.Fatalf("expected ctrl+u to clear focused token field, got %q", got)
	}
}

func TestConfigEditorCtrlUStringPathClearsFocusedField(t *testing.T) {
	AM = AppModel{}
	am := &AM

	am.GistConfig = GistConfig{Platform: "github", Token: "ghp_plain_token", GistID: "gid-1", Locale: "en"}
	AM.Platform = "github"
	AM.width = 100
	AM.height = 30
	createList()

	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{0x15}})

	if got := AM.configEditor.fieldValueForCopy(configFieldToken); got != "" {
		t.Fatalf("expected ctrl+u rune path to clear focused token field, got %q", got)
	}
}

func TestBookmarkEditorCtrlURawRuneClearsFocusedField(t *testing.T) {
	AM = AppModel{}
	am := &AM
	_ = setupBookmarkTempDir(t)

	AM.BookmarkInfo.List = []BookmarkItem{{
		Title:      "prod",
		Host:       "10.0.0.8",
		Username:   "root",
		Port:       22,
		EnableSSH:  true,
		AuthType:   "private-key",
		PrivateKey: "line-1\nline-2",
	}}
	AM.width = 100
	AM.height = 30
	createList()

	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	for i := 0; i < 6; i++ {
		_, _ = am.Update(tea.KeyMsg{Type: tea.KeyTab})
	}
	_, _ = am.Update(tea.KeyMsg{Runes: []rune{0x15}})

	if got := AM.editor.fieldValueForCopy(editorFieldPrivateKey); got != "" {
		t.Fatalf("expected raw ctrl+u rune path to clear focused private key field, got %q", got)
	}
}

func TestConfigEditorCtrlURawRuneClearsFocusedField(t *testing.T) {
	AM = AppModel{}
	am := &AM

	am.GistConfig = GistConfig{Platform: "github", Token: "ghp_plain_token", GistID: "gid-1", Locale: "en"}
	AM.Platform = "github"
	AM.width = 100
	AM.height = 30
	createList()

	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	_, _ = am.Update(tea.KeyMsg{Runes: []rune{0x15}})

	if got := AM.configEditor.fieldValueForCopy(configFieldToken); got != "" {
		t.Fatalf("expected raw ctrl+u rune path to clear focused token field, got %q", got)
	}
}

func TestBookmarkEditorCtrlUHiddenModeDoesNotRestoreSecret(t *testing.T) {
	AM = AppModel{}
	am := &AM
	_ = setupBookmarkTempDir(t)

	AM.BookmarkInfo.List = []BookmarkItem{{
		Title:      "prod",
		Host:       "10.0.0.8",
		Username:   "root",
		Port:       22,
		EnableSSH:  true,
		AuthType:   "private-key",
		PrivateKey: "line-1\nline-2",
	}}
	AM.width = 100
	AM.height = 30
	createList()

	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	for i := 0; i < 6; i++ {
		_, _ = am.Update(tea.KeyMsg{Type: tea.KeyTab})
	}
	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{0x15}})
	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'A'}})

	if !AM.editor.showSecrets {
		t.Fatalf("expected typing after ctrl+u to auto reveal empty private key field")
	}
	if got := AM.editor.inputs[editorFieldPrivateKey].Value(); got != "A" {
		t.Fatalf("expected cleared private key field not to restore old value, got %q", got)
	}
}

func TestConfigEditorCtrlUHiddenModeDoesNotRestoreToken(t *testing.T) {
	AM = AppModel{}
	am := &AM

	am.GistConfig = GistConfig{Platform: "github", Token: "ghp_plain_token", GistID: "gid-1", Locale: "en"}
	AM.Platform = "github"
	AM.width = 100
	AM.height = 30
	createList()

	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{0x15}})
	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})

	if !AM.configEditor.showSecrets {
		t.Fatalf("expected typing after ctrl+u to auto reveal empty token field")
	}
	if got := AM.configEditor.inputs[configFieldToken].Value(); got != "a" {
		t.Fatalf("expected cleared token field not to restore old value, got %q", got)
	}
}

func TestBookmarkEditorCtrlUHiddenThenRevealStaysEmpty(t *testing.T) {
	AM = AppModel{}
	am := &AM
	_ = setupBookmarkTempDir(t)

	AM.BookmarkInfo.List = []BookmarkItem{{
		Title:      "prod",
		Host:       "10.0.0.8",
		Username:   "root",
		Port:       22,
		EnableSSH:  true,
		AuthType:   "private-key",
		PrivateKey: "line-1\nline-2",
	}}
	AM.width = 100
	AM.height = 30
	createList()

	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	for i := 0; i < 6; i++ {
		_, _ = am.Update(tea.KeyMsg{Type: tea.KeyTab})
	}
	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{0x15}})
	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyCtrlR})

	if got := AM.editor.inputs[editorFieldPrivateKey].Value(); got != "" {
		t.Fatalf("expected private key to stay empty after hidden ctrl+u then reveal, got %q", got)
	}
}

func TestConfigEditorCtrlUHiddenThenRevealStaysEmpty(t *testing.T) {
	AM = AppModel{}
	am := &AM

	am.GistConfig = GistConfig{Platform: "github", Token: "ghp_plain_token", GistID: "gid-1", Locale: "en"}
	AM.Platform = "github"
	AM.width = 100
	AM.height = 30
	createList()

	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{0x15}})
	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyCtrlR})

	if got := AM.configEditor.inputs[configFieldToken].Value(); got != "" {
		t.Fatalf("expected token to stay empty after hidden ctrl+u then reveal, got %q", got)
	}
}

func TestConfigEditorCtrlYCopiesToken(t *testing.T) {
	AM = AppModel{}
	am := &AM

	originalWrite := clipboardWriteAll
	defer func() { clipboardWriteAll = originalWrite }()
	copied := ""
	clipboardWriteAll = func(text string) error {
		copied = text
		return nil
	}

	am.GistConfig = GistConfig{Platform: "github", Token: "ghp_plain_token", GistID: "gid-1", Locale: "en"}
	AM.Platform = "github"
	AM.width = 100
	AM.height = 30
	createList()

	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyCtrlY})

	if copied != "ghp_plain_token" {
		t.Fatalf("expected ctrl+y copy token, got %q", copied)
	}
	if !strings.Contains(AM.TipString, "chars") {
		t.Fatalf("expected copy tip to include character count, got %q", AM.TipString)
	}
}

func TestPlainVInputDoesNotToggleConfigSecretVisibility(t *testing.T) {
	AM = AppModel{}
	am := &AM

	am.GistConfig = GistConfig{Platform: "github", Token: "ghp_plain_token", GistID: "gid-1", Locale: "en"}
	AM.Platform = "github"
	AM.width = 100
	AM.height = 30
	createList()

	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	if AM.configEditor == nil {
		t.Fatalf("expected config editor open")
	}

	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})
	if got := AM.configEditor.inputs[configFieldToken].Value(); got != maskedSecretValue {
		t.Fatalf("expected plain v not to toggle visibility, got %q", got)
	}
}

func TestEditorHelpShowsCaretRShortcut(t *testing.T) {
	AM = AppModel{}
	am := &AM
	_ = setupBookmarkTempDir(t)

	am.GistConfig = GistConfig{Platform: "github", Token: "ghp_plain_token", GistID: "gid-1", Locale: "en"}
	AM.Platform = "github"
	AM.BookmarkInfo.List = []BookmarkItem{{Title: "dev", Host: "10.0.0.1", Username: "root", Port: 22, EnableSSH: true}}
	AM.width = 100
	AM.height = 30
	createList()

	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	configView := am.View()
	if !strings.Contains(configView, "^R") {
		t.Fatalf("expected config editor help to show ^R")
	}
	if !strings.Contains(configView, "^Y") {
		t.Fatalf("expected config editor help to show ^Y")
	}
	if !strings.Contains(configView, "^U") {
		t.Fatalf("expected config editor help to show ^U")
	}

	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyEsc})
	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	bookmarkView := am.View()
	if !strings.Contains(bookmarkView, "^R") {
		t.Fatalf("expected bookmark editor help to show ^R")
	}
	if !strings.Contains(bookmarkView, "^Y") {
		t.Fatalf("expected bookmark editor help to show ^Y")
	}
	if !strings.Contains(bookmarkView, "^U") {
		t.Fatalf("expected bookmark editor help to show ^U")
	}
}

func TestEditorHelpShowsCurrentRevealMode(t *testing.T) {
	AM = AppModel{}
	am := &AM
	_ = setupBookmarkTempDir(t)

	am.GistConfig = GistConfig{Platform: "github", Token: "ghp_plain_token", GistID: "gid-1", Locale: "en"}
	AM.Platform = "github"
	AM.BookmarkInfo.List = []BookmarkItem{{
		Title:      "dev",
		Host:       "10.0.0.1",
		Username:   "root",
		Port:       22,
		EnableSSH:  true,
		AuthType:   "private-key",
		PrivateKey: "KEY",
	}}
	AM.width = 100
	AM.height = 30
	createList()

	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	if !strings.Contains(am.View(), "^R reveal/hide") || !strings.Contains(strings.ToLower(am.View()), "hidden") {
		t.Fatalf("expected config editor to show inline reveal mode in help")
	}
	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	if !strings.Contains(am.View(), "^R reveal/hide") || !strings.Contains(strings.ToLower(am.View()), "shown") {
		t.Fatalf("expected config editor to show inline shown mode after ^R")
	}

	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyEsc})
	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	if AM.editor == nil || AM.editor.showSecrets {
		t.Fatalf("expected bookmark editor default hidden mode")
	}
	if !strings.Contains(am.View(), "^R reveal/hide") || !strings.Contains(strings.ToLower(am.View()), "hidden") {
		t.Fatalf("expected bookmark editor to show inline reveal mode in help")
	}
	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	if !AM.editor.showSecrets {
		t.Fatalf("expected bookmark editor switched to shown mode after ^R")
	}
	if !strings.Contains(am.View(), "^R reveal/hide") || !strings.Contains(strings.ToLower(am.View()), "shown") {
		t.Fatalf("expected bookmark editor to show inline shown mode after ^R")
	}
}

func TestModeHintUsesColorStyling(t *testing.T) {
	AM = AppModel{}
	am := &AM

	ce := newConfigEditor(GistConfig{Platform: "github", Token: "x", GistID: "gid", Locale: "en"})
	ce.showSecrets = false
	hidden := ce.modeHint(am)
	ce.showSecrets = true
	shown := ce.modeHint(am)
	if !strings.Contains(strings.ToLower(hidden), "hidden") || !strings.Contains(strings.ToLower(shown), "shown") {
		t.Fatalf("expected hidden/shown mode hints, hidden=%q shown=%q", hidden, shown)
	}
	if hidden == shown {
		t.Fatalf("expected hidden and shown mode hints to have distinct styling")
	}

	be := newBookmarkEditor(editorModeAdd, 0, BookmarkItem{AuthType: "private-key"})
	be.showSecrets = false
	bHidden := be.modeHint(am)
	be.showSecrets = true
	bShown := be.modeHint(am)
	if !strings.Contains(strings.ToLower(bHidden), "hidden") || !strings.Contains(strings.ToLower(bShown), "shown") {
		t.Fatalf("expected hidden/shown bookmark hints, hidden=%q shown=%q", bHidden, bShown)
	}
	if bHidden == bShown {
		t.Fatalf("expected bookmark hidden and shown hints to have distinct styling")
	}
}

func TestConfigEditorEmptyTokenAutoRevealsOnType(t *testing.T) {
	AM = AppModel{}
	am := &AM

	am.GistConfig = GistConfig{Platform: "github", Token: "", GistID: "gid-1", Locale: "en"}
	AM.Platform = "github"
	AM.width = 100
	AM.height = 30
	createList()

	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})

	if !AM.configEditor.showSecrets {
		t.Fatalf("expected empty token field typing to auto reveal mode")
	}
	if got := AM.configEditor.inputs[configFieldToken].Value(); got != "a" {
		t.Fatalf("expected typing to work on empty token without manual ^R, got %q", got)
	}
}

func TestBookmarkEditorEmptyPrivateKeyAutoRevealsOnType(t *testing.T) {
	AM = AppModel{}
	am := &AM
	_ = setupBookmarkTempDir(t)

	AM.BookmarkInfo.List = []BookmarkItem{{
		Title:      "prod",
		Host:       "10.0.0.8",
		Username:   "root",
		Port:       22,
		EnableSSH:  true,
		AuthType:   "private-key",
		PrivateKey: "",
		Passphrase: "",
	}}
	AM.width = 100
	AM.height = 30
	createList()

	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	for i := 0; i < 6; i++ {
		_, _ = am.Update(tea.KeyMsg{Type: tea.KeyTab})
	}
	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'A'}})

	if !AM.editor.showSecrets {
		t.Fatalf("expected empty private key typing to auto reveal mode")
	}
	if got := AM.editor.inputs[editorFieldPrivateKey].Value(); got != "A" {
		t.Fatalf("expected typing to work on empty private key without manual ^R, got %q", got)
	}
}

func TestDeleteBookmarkConfirmationRemovesSelectedItem(t *testing.T) {
	AM = AppModel{}
	am := &AM
	_ = setupBookmarkTempDir(t)

	AM.BookmarkInfo.List = []BookmarkItem{
		{Title: "dev", Host: "10.0.0.1", Username: "root", Port: 22, EnableSSH: true},
		{Title: "staging", Host: "10.0.0.2", Username: "root", Port: 22, EnableSSH: true},
	}
	AM.width = 100
	AM.height = 30
	createList()
	AM.list.Select(1)

	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	if AM.pendingDelete == nil {
		t.Fatalf("expected delete key to open confirmation modal")
	}

	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})

	if AM.pendingDelete != nil {
		t.Fatalf("expected delete confirmation to close after success")
	}
	if len(AM.BookmarkInfo.List) != 1 {
		t.Fatalf("expected one bookmark after deletion, got %d", len(AM.BookmarkInfo.List))
	}
	if AM.BookmarkInfo.List[0].Host != "10.0.0.1" {
		t.Fatalf("expected remaining bookmark to be first item, got %+v", AM.BookmarkInfo.List[0])
	}
}

func TestAddBookmarkRejectsInvalidPortAndKeepsEditorOpen(t *testing.T) {
	AM = AppModel{}
	am := &AM
	_ = setupBookmarkTempDir(t)

	AM.BookmarkInfo.List = []BookmarkItem{}
	AM.width = 100
	AM.height = 30
	createList()

	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	if AM.editor == nil {
		t.Fatalf("expected add key to open editor")
	}

	AM.editor.inputs[editorFieldHost].SetValue("10.0.0.3")
	AM.editor.inputs[editorFieldUsername].SetValue("root")
	AM.editor.inputs[editorFieldPort].SetValue("abc")

	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyCtrlS})

	if AM.editor == nil {
		t.Fatalf("expected editor to stay open when validation fails")
	}
	if len(AM.BookmarkInfo.List) != 0 {
		t.Fatalf("expected no bookmark added on invalid port, got %d", len(AM.BookmarkInfo.List))
	}
	if !strings.Contains(strings.ToLower(AM.TipString), "port") {
		t.Fatalf("expected validation tip to mention port, got %q", AM.TipString)
	}
}

func TestEditorAuthModeSwitchShowsOnlyRelevantFields(t *testing.T) {
	AM = AppModel{}
	am := &AM
	_ = setupBookmarkTempDir(t)

	AM.BookmarkInfo.List = []BookmarkItem{{
		Title:      "prod",
		Host:       "10.0.0.8",
		Username:   "root",
		Port:       22,
		EnableSSH:  true,
		Password:   "secret",
		AuthType:   "password",
		PrivateKey: "",
	}}
	AM.width = 90
	AM.height = 30
	createList()

	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	view := am.View()
	if !strings.Contains(view, "Password") {
		t.Fatalf("expected password field in password mode")
	}
	if strings.Contains(view, "Private Key") {
		t.Fatalf("expected private key field hidden in password mode")
	}

	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyCtrlM})
	view = am.View()
	if !strings.Contains(view, "Private Key") || !strings.Contains(view, "Passphrase") {
		t.Fatalf("expected key fields in private-key mode")
	}
	if strings.Contains(view, "Password") {
		t.Fatalf("expected password field hidden in private-key mode")
	}

	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyCtrlM})
	view = am.View()
	if strings.Contains(view, "Password") || strings.Contains(view, "Private Key") || strings.Contains(view, "Passphrase") {
		t.Fatalf("expected secret fields hidden in keyboard-interactive mode")
	}
	if !strings.Contains(strings.ToLower(view), "keyboard-interactive") {
		t.Fatalf("expected keyboard-interactive indicator in editor view")
	}
}

func TestEditorKeyboardModeSaveClearsSecretFields(t *testing.T) {
	AM = AppModel{}
	am := &AM
	_ = setupBookmarkTempDir(t)

	AM.BookmarkInfo.List = []BookmarkItem{{
		Title:      "prod",
		Host:       "10.0.0.8",
		Username:   "root",
		Port:       22,
		EnableSSH:  true,
		Password:   "secret",
		AuthType:   "password",
		PrivateKey: "PRIVATE KEY DATA",
		Passphrase: "pp",
	}}
	AM.width = 90
	AM.height = 24
	createList()

	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyCtrlM})
	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyCtrlM})
	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyCtrlS})

	updated := AM.BookmarkInfo.List[0]
	if updated.AuthType != "keyboard-interactive" {
		t.Fatalf("expected auth type keyboard-interactive, got %q", updated.AuthType)
	}
	if updated.Password != "" || updated.PrivateKey != "" || updated.Passphrase != "" {
		t.Fatalf("expected secrets cleared in keyboard-interactive mode, got %+v", updated)
	}
}

func TestEditorSmallWindowUsesScrollableStableLayout(t *testing.T) {
	AM = AppModel{}
	am := &AM
	_ = setupBookmarkTempDir(t)

	AM.BookmarkInfo.List = []BookmarkItem{{
		Title:     "dev",
		Host:      "10.0.0.1",
		Username:  "root",
		Port:      22,
		EnableSSH: true,
	}}
	AM.width = 52
	AM.height = 12
	createList()

	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyCtrlM})

	_ = am.View()
	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyHome})

	view := am.View()
	lines := strings.Split(view, "\n")
	if len(lines) == 0 {
		t.Fatalf("expected non-empty editor view")
	}
	ref := ansi.StringWidth(lines[0])
	for i, line := range lines {
		if w := ansi.StringWidth(line); w != ref {
			t.Fatalf("expected stable line widths in small editor view, line=%d width=%d ref=%d", i, w, ref)
		}
	}
	if !strings.Contains(strings.ToLower(view), "switch") && !strings.Contains(view, "切换") {
		t.Fatalf("expected auth switch hint to be visible in small window")
	}
	if !strings.Contains(strings.ToLower(view), "save") && !strings.Contains(view, "保存") {
		t.Fatalf("expected editor help to remain visible in small window")
	}
}

func TestEditorTabScrollsToLastFieldInSmallWindow(t *testing.T) {
	AM = AppModel{}
	am := &AM
	_ = setupBookmarkTempDir(t)

	AM.BookmarkInfo.List = []BookmarkItem{{
		Title:      "dev",
		Host:       "10.0.0.1",
		Username:   "root",
		Port:       22,
		EnableSSH:  true,
		Password:   "secret",
		AuthType:   "password",
		PrivateKey: "KEY",
		Passphrase: "pp",
	}}
	AM.width = 60
	AM.height = 10
	createList()

	// Open editor in private-key mode (most fields: title, host, username, port, authType, privateKey, passphrase)
	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	if AM.editor == nil {
		t.Fatalf("expected editor to open")
	}
	// Switch to private-key auth
	AM.editor.authType = authTypePrivateKey
	AM.editor.authDirty = true

	// Tab through all fields to the last one
	fields := AM.editor.activeFields()
	for i := 0; i < len(fields)-1; i++ {
		_, _ = am.Update(tea.KeyMsg{Type: tea.KeyTab})
	}

	lastField := fields[len(fields)-1]
	if AM.editor.focusedField() != lastField {
		t.Fatalf("expected focus on last field %d, got %d", lastField, AM.editor.focusedField())
	}

	view := am.View()
	lastLabel := am.editorFieldLabel(lastField)
	if !strings.Contains(view, lastLabel) {
		t.Fatalf("expected last field label %q visible after tabbing in small window, view:\n%s", lastLabel, view)
	}
}

func TestConnectEnterRepeated(t *testing.T) {
	base := time.Now()
	if connectEnterRepeated(base, time.Time{}) {
		t.Fatalf("expected first enter to be allowed")
	}
	if !connectEnterRepeated(base.Add(100*time.Millisecond), base) {
		t.Fatalf("expected enter repeat within debounce window to be blocked")
	}
	if connectEnterRepeated(base.Add(600*time.Millisecond), base) {
		t.Fatalf("expected enter after debounce window to be allowed")
	}
}

func TestAuthModeTextShowsInteractiveLabelForKeyboardInteractive(t *testing.T) {
	AM = AppModel{}

	AM.locale = localeEN
	en := authModeText(BookmarkItem{AuthType: "keyboard-interactive"})
	if !strings.Contains(strings.ToLower(en), "interactive login") {
		t.Fatalf("expected english auth mode text to mention interactive login, got %q", en)
	}

	AM.locale = localeZH
	zh := authModeText(BookmarkItem{AuthType: "keyboard-interactive"})
	if !strings.Contains(zh, "交互登录") {
		t.Fatalf("expected chinese auth mode text to mention 交互登录, got %q", zh)
	}
}

func TestEditorViewUsesPageStyleWithoutInverseOrBoxArtifacts(t *testing.T) {
	AM = AppModel{}
	am := &AM
	_ = setupBookmarkTempDir(t)

	AM.BookmarkInfo.List = []BookmarkItem{{
		Title:     "dev",
		Host:      "10.0.0.1",
		Username:  "root",
		Port:      22,
		EnableSSH: true,
	}}
	AM.width = 90
	AM.height = 20
	createList()

	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	view := am.View()

	if strings.Contains(view, "\x1b[7m") {
		t.Fatalf("expected editor view not to use inverse video cursor blocks")
	}
	if strings.Contains(view, "\x1b[48;") {
		t.Fatalf("expected editor view not to force background color fills")
	}
	if strings.ContainsAny(view, "╭╮╰╯│") {
		t.Fatalf("expected editor page style without modal box border artifacts")
	}
}

func TestCursorWrapsFromFirstToLast(t *testing.T) {
	AM = AppModel{}
	am := &AM

	AM.BookmarkInfo.List = []BookmarkItem{
		{Title: "a", Host: "10.0.0.1", Port: 22, Username: "root", EnableSSH: true},
		{Title: "b", Host: "10.0.0.2", Port: 22, Username: "root", EnableSSH: true},
		{Title: "c", Host: "10.0.0.3", Port: 22, Username: "root", EnableSSH: true},
	}
	AM.width = 80
	AM.height = 24
	createList()
	AM.list.Select(0)

	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyUp})

	if got := AM.list.GlobalIndex(); got != 2 {
		t.Fatalf("expected cursor to wrap to last item (2), got %d", got)
	}
}

func TestCursorWrapsFromLastToFirst(t *testing.T) {
	AM = AppModel{}
	am := &AM

	AM.BookmarkInfo.List = []BookmarkItem{
		{Title: "a", Host: "10.0.0.1", Port: 22, Username: "root", EnableSSH: true},
		{Title: "b", Host: "10.0.0.2", Port: 22, Username: "root", EnableSSH: true},
		{Title: "c", Host: "10.0.0.3", Port: 22, Username: "root", EnableSSH: true},
	}
	AM.width = 80
	AM.height = 24
	createList()
	AM.list.Select(2)

	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})

	if got := AM.list.GlobalIndex(); got != 0 {
		t.Fatalf("expected cursor to wrap to first item (0), got %d", got)
	}
}

func TestPageWrapsFromLastToFirst(t *testing.T) {
	AM = AppModel{}
	am := &AM

	AM.BookmarkInfo.List = make([]BookmarkItem, 40)
	for i := range AM.BookmarkInfo.List {
		AM.BookmarkInfo.List[i] = BookmarkItem{Title: "srv", Host: "10.0.0.1", Port: 22, Username: "root", EnableSSH: true}
	}
	AM.width = 80
	AM.height = 24
	createList()

	lastPage := AM.list.Paginator.TotalPages - 1
	if lastPage < 1 {
		t.Fatalf("expected multiple pages, got %d", AM.list.Paginator.TotalPages)
	}
	AM.list.Paginator.Page = lastPage

	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRight})

	if AM.list.Paginator.Page != 0 {
		t.Fatalf("expected page to wrap to 0, got %d", AM.list.Paginator.Page)
	}
	if got := AM.list.GlobalIndex(); got != 0 {
		t.Fatalf("expected cursor at 0 after page wrap, got %d", got)
	}
}

func TestPageWrapsFromFirstToLast(t *testing.T) {
	AM = AppModel{}
	am := &AM

	AM.BookmarkInfo.List = make([]BookmarkItem, 40)
	for i := range AM.BookmarkInfo.List {
		AM.BookmarkInfo.List[i] = BookmarkItem{Title: "srv", Host: "10.0.0.1", Port: 22, Username: "root", EnableSSH: true}
	}
	AM.width = 80
	AM.height = 24
	createList()

	lastPage := AM.list.Paginator.TotalPages - 1
	if lastPage < 1 {
		t.Fatalf("expected multiple pages, got %d", AM.list.Paginator.TotalPages)
	}
	AM.list.Paginator.Page = 0

	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyLeft})

	if AM.list.Paginator.Page != lastPage {
		t.Fatalf("expected page to wrap to %d, got %d", lastPage, AM.list.Paginator.Page)
	}
}

func TestSyncPushRequiresConfirmAndDefaultsToNoOnEnter(t *testing.T) {
	AM = AppModel{}
	am := &AM

	AM.Token = "token"
	AM.GistID = "gid"
	AM.BookmarkInfo.List = []BookmarkItem{
		{Title: "dev", Host: "10.0.0.1", Port: 22, Username: "root", EnableSSH: true},
	}
	AM.width = 100
	AM.height = 30
	createList()

	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	if AM.isSyncing {
		t.Fatalf("expected pressing s to open confirm first, not start syncing")
	}

	view := am.View()
	if !strings.Contains(view, "WARNING") {
		t.Fatalf("expected obvious warning in sync confirm, got: %q", view)
	}
	if !strings.Contains(view, "No") {
		t.Fatalf("expected sync confirm to render No option by default")
	}
	if !strings.Contains(view, "Upload local bookmarks to remote Gist") {
		t.Fatalf("expected clearer push explanation in sync confirm, got: %q", view)
	}

	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if AM.isSyncing {
		t.Fatalf("expected enter on default No to cancel syncing")
	}
	if !strings.Contains(strings.ToLower(AM.TipString), "cancel") {
		t.Fatalf("expected cancel tip after default-no enter, got %q", AM.TipString)
	}
}

func TestSyncPullCanConfirmWithY(t *testing.T) {
	AM = AppModel{}
	am := &AM

	AM.Token = "token"
	AM.GistID = "gid"
	AM.BookmarkInfo.List = []BookmarkItem{
		{Title: "dev", Host: "10.0.0.1", Port: 22, Username: "root", EnableSSH: true},
	}
	AM.width = 100
	AM.height = 30
	createList()

	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'S'}})
	if AM.isSyncing {
		t.Fatalf("expected pressing S to open confirm first, not start syncing")
	}
	if view := am.View(); !strings.Contains(view, "Download from remote Gist and overwrite local bookmarks") {
		t.Fatalf("expected clearer pull explanation in sync confirm, got: %q", view)
	}

	_, cmd := am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	if !AM.isSyncing {
		t.Fatalf("expected y to confirm sync and start syncing")
	}
	if cmd == nil {
		t.Fatalf("expected confirmed sync to return async command")
	}
}

func TestSyncPushAllowsEmptyGistIDForFirstCreate(t *testing.T) {
	AM = AppModel{}
	am := &AM

	AM.Token = "token"
	AM.GistID = ""
	AM.BookmarkInfo.List = []BookmarkItem{{Title: "dev", Host: "10.0.0.1", Port: 22, Username: "root", EnableSSH: true}}
	AM.width = 100
	AM.height = 30
	createList()

	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	if AM.pendingSync == nil {
		t.Fatalf("expected push to open confirm even when gist_id is empty")
	}
	if strings.Contains(strings.ToLower(AM.TipString), "gist_id") {
		t.Fatalf("expected no gist_id warning for push-first-create flow, got %q", AM.TipString)
	}
}

func TestSyncPullRequiresGistID(t *testing.T) {
	AM = AppModel{}
	am := &AM

	AM.Token = "token"
	AM.GistID = ""
	AM.BookmarkInfo.List = []BookmarkItem{{Title: "dev", Host: "10.0.0.1", Port: 22, Username: "root", EnableSSH: true}}
	AM.width = 100
	AM.height = 30
	createList()

	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'S'}})
	if AM.pendingSync != nil {
		t.Fatalf("expected pull not to open confirm when gist_id is empty")
	}
	if !strings.Contains(strings.ToLower(AM.TipString), "gist_id") {
		t.Fatalf("expected gist_id warning for pull without gist_id, got %q", AM.TipString)
	}
}

func TestSyncConfirmViewUsesOptionBackgroundHighlight(t *testing.T) {
	AM = AppModel{}
	am := &AM

	AM.Token = "token"
	AM.GistID = "gid"
	AM.BookmarkInfo.List = []BookmarkItem{
		{Title: "dev", Host: "10.0.0.1", Port: 22, Username: "root", EnableSSH: true},
	}
	AM.width = 100
	AM.height = 30
	createList()

	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	view := am.View()

	if !strings.Contains(view, "◉") {
		t.Fatalf("expected sync confirm selected option indicator, got %q", view)
	}
}

func TestSyncConfirmOverlayUsesOptionBackgroundHighlight(t *testing.T) {
	AM = AppModel{}
	am := &AM
	AM.pendingSync = &syncConfirmState{action: syncActionPush, selectYes: false}

	overlay := am.buildSyncConfirmOverlay(100)
	if !strings.Contains(overlay, "◉") {
		t.Fatalf("expected sync confirm overlay to include option selection indicator, got %q", overlay)
	}
}

func TestDeleteConfirmDefaultsToNoOnEnter(t *testing.T) {
	AM = AppModel{}
	am := &AM
	_ = setupBookmarkTempDir(t)

	AM.BookmarkInfo.List = []BookmarkItem{{
		Title:     "dev",
		Host:      "10.0.0.1",
		Username:  "root",
		Port:      22,
		EnableSSH: true,
	}}
	AM.width = 100
	AM.height = 30
	createList()

	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	if AM.pendingDelete == nil {
		t.Fatalf("expected delete key to open confirmation modal")
	}

	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if len(AM.BookmarkInfo.List) != 1 {
		t.Fatalf("expected default-no enter to keep bookmark, got %d", len(AM.BookmarkInfo.List))
	}
	if !strings.Contains(strings.ToLower(AM.TipString), "cancel") {
		t.Fatalf("expected cancel tip after default-no enter, got %q", AM.TipString)
	}
}

func TestDeleteConfirmOverlayUsesOptionBackgroundHighlight(t *testing.T) {
	AM = AppModel{}
	am := &AM
	AM.pendingDelete = &deleteConfirmState{index: 0, label: "dev (root@10.0.0.1:22)"}

	overlay := am.buildDeleteConfirmOverlay(100)
	if !strings.Contains(overlay, "◉") {
		t.Fatalf("expected delete confirm overlay to include option selection indicator, got %q", overlay)
	}
}

func TestSyncUploadMsgPersistsGistIDWhenAlreadySetInMemory(t *testing.T) {
	AM = AppModel{}
	am := &AM
	dir := t.TempDir()
	APP_DIR = dir
	ConfigFile = filepath.Join(dir, "config.json")

	am.GistConfig = GistConfig{
		Platform: "github",
		Token:    "token-1",
		GistID:   "",
		Locale:   "en",
	}
	if err := SaveConfig(am.GistConfig); err != nil {
		t.Fatalf("failed to seed config: %v", err)
	}

	AM.BookmarkInfo.List = []BookmarkItem{{Title: "dev", Host: "10.0.0.1", Username: "root", Port: 22, EnableSSH: true}}
	AM.GistID = "new-gist-id"

	_, _ = am.Update(syncUploadMsg{Err: nil, GistID: "new-gist-id"})

	data, err := os.ReadFile(ConfigFile)
	if err != nil {
		t.Fatalf("failed to read config file: %v", err)
	}
	var cfg GistConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("failed to parse config file: %v", err)
	}
	if cfg.GistID != "new-gist-id" {
		t.Fatalf("expected persisted gist_id to be new-gist-id, got %q", cfg.GistID)
	}
}

func TestSyncUploadMsgReturnsErrorTipWhenConfigPersistFails(t *testing.T) {
	AM = AppModel{}
	am := &AM
	dir := t.TempDir()
	APP_DIR = dir
	ConfigFile = filepath.Join(dir, "missing", "config.json")

	am.GistConfig = GistConfig{
		Platform: "github",
		Token:    "token-1",
		GistID:   "old-gist-id",
		Locale:   "en",
	}
	AM.BookmarkInfo.List = []BookmarkItem{{Title: "dev", Host: "10.0.0.1", Username: "root", Port: 22, EnableSSH: true}}

	_, _ = am.Update(syncUploadMsg{Err: nil, GistID: "new-gist-id"})

	if !strings.Contains(AM.TipString, "write config") {
		t.Fatalf("expected config persist error tip, got %q", AM.TipString)
	}
}

func TestTmuxScrollCallbackShellsMapping(t *testing.T) {
	if got := tmuxScrollCallbackShells("on"); len(got) != 1 || got[0].Cmd != "tmux set -g mouse on" || got[0].Delay != 300 {
		t.Fatalf("unexpected on mapping: %+v", got)
	}
	if got := tmuxScrollCallbackShells("off"); len(got) != 1 || got[0].Cmd != "tmux set -g mouse off" || got[0].Delay != 300 {
		t.Fatalf("unexpected off mapping: %+v", got)
	}
	if got := tmuxScrollCallbackShells(""); got != nil {
		t.Fatalf("expected nil callbacks for empty tmuxScroll, got %+v", got)
	}
	if got := tmuxScrollCallbackShells("bogus"); got != nil {
		t.Fatalf("expected nil callbacks for unknown tmuxScroll, got %+v", got)
	}
	if got := tmuxScrollCallbackShells(" on "); len(got) != 1 || got[0].Cmd != "tmux set -g mouse on" {
		t.Fatalf("expected trimmed on mapping: %+v", got)
	}
}

func TestBuildSSHClientWiresTmuxScrollCallbacks(t *testing.T) {
	client, err := buildSSHClient(BookmarkItem{Host: "h", Username: "u", TmuxScroll: "on"}, 22)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(client.node.CallbackShells) != 1 || client.node.CallbackShells[0].Cmd != "tmux set -g mouse on" {
		t.Fatalf("expected tmux on callback wired, got %+v", client.node.CallbackShells)
	}

	client2, err := buildSSHClient(BookmarkItem{Host: "h", Username: "u"}, 22)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client2.node.CallbackShells != nil {
		t.Fatalf("expected nil callbacks for empty tmuxScroll, got %+v", client2.node.CallbackShells)
	}
}

func TestBookmarkEditorTmuxScrollCyclesAndSaves(t *testing.T) {
	AM = AppModel{}
	am := &AM
	bookmarkDir := setupBookmarkTempDir(t)

	AM.BookmarkInfo.List = []BookmarkItem{{
		Title:     "dev",
		Host:      "10.0.0.1",
		Username:  "root",
		Port:      22,
		EnableSSH: true,
	}}
	AM.width = 100
	AM.height = 30
	createList()

	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	if AM.editor == nil {
		t.Fatalf("expected editor to open")
	}
	// Default is no-op (empty)
	if got := AM.editor.tmuxScroll; got != "" {
		t.Fatalf("expected default tmuxScroll empty, got %q", got)
	}
	if got := am.editorFieldLabel(editorFieldTmuxScroll); got != "Tmux Scroll" {
		t.Fatalf("expected EN label, got %q", got)
	}

	// Tab to the Tmux Scroll field (title, host, username, port, authType, tmuxScroll)
	for i := 0; i < 5; i++ {
		_, _ = am.Update(tea.KeyMsg{Type: tea.KeyTab})
	}
	if AM.editor.focusedField() != editorFieldTmuxScroll {
		t.Fatalf("expected focus on tmux scroll field, got %d", AM.editor.focusedField())
	}

	// Right cycles "" -> "on" -> "off" -> ""
	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRight})
	if got := AM.editor.tmuxScroll; got != "on" {
		t.Fatalf("expected tmuxScroll on after right, got %q", got)
	}
	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRight})
	if got := AM.editor.tmuxScroll; got != "off" {
		t.Fatalf("expected tmuxScroll off after second right, got %q", got)
	}
	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRight})
	if got := AM.editor.tmuxScroll; got != "" {
		t.Fatalf("expected tmuxScroll wrap to empty, got %q", got)
	}
	// Enter also cycles when focused on the field ("" -> "on")
	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if got := AM.editor.tmuxScroll; got != "on" {
		t.Fatalf("expected tmuxScroll on after enter, got %q", got)
	}
	// Cycle "on" -> "off" -> "" -> "on" for save
	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRight})
	if got := AM.editor.tmuxScroll; got != "off" {
		t.Fatalf("expected tmuxScroll off, got %q", got)
	}
	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRight})
	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRight})
	if got := AM.editor.tmuxScroll; got != "on" {
		t.Fatalf("expected tmuxScroll on, got %q", got)
	}

	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	if AM.editor != nil {
		t.Fatalf("expected editor closed after save")
	}
	if got := AM.BookmarkInfo.List[0].TmuxScroll; got != "on" {
		t.Fatalf("expected saved tmuxScroll on, got %q", got)
	}
	data, err := os.ReadFile(filepath.Join(bookmarkDir, "bookmarks.json"))
	if err != nil {
		t.Fatalf("expected bookmarks file to be written: %v", err)
	}
	if !strings.Contains(string(data), "\"tmuxScroll\":\"on\"") {
		t.Fatalf("expected persisted json to contain tmuxScroll, got %s", string(data))
	}
}

func TestBookmarkEditorTmuxScrollLabelI18nAndEditBackfill(t *testing.T) {
	AM = AppModel{}
	am := &AM
	_ = setupBookmarkTempDir(t)

	AM.BookmarkInfo.List = []BookmarkItem{{
		Title:      "dev",
		Host:       "10.0.0.1",
		Username:   "root",
		Port:       22,
		EnableSSH:  true,
		TmuxScroll: "off",
	}}
	AM.width = 100
	AM.height = 30
	AM.locale = localeZH
	createList()

	if got := am.editorFieldLabel(editorFieldTmuxScroll); got != "Tmux 滚动" {
		t.Fatalf("expected ZH label, got %q", got)
	}

	_, _ = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	if AM.editor == nil {
		t.Fatalf("expected editor to open")
	}
	// Edit mode backfills the existing value
	if got := AM.editor.tmuxScroll; got != "off" {
		t.Fatalf("expected tmuxScroll backfilled from bookmark, got %q", got)
	}
}
