package model

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func TestTimeInputTabFromPresetFocusesCustomRange(t *testing.T) {
	now := time.Now()
	m := NewTimeInputModel(now.Add(-time.Hour), now)

	var cmd tea.Cmd
	m, cmd = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if cmd != nil {
		t.Fatalf("tab from preset returned unexpected command")
	}
	if got := m.currentFocus(); got != "start" {
		t.Fatalf("focus = %q, want start", got)
	}
	if got := m.start.CursorPosition(); got != 0 {
		t.Fatalf("start cursor = %d, want first segment", got)
	}
}

func TestTimeInputTabReachesDisplayTime(t *testing.T) {
	now := time.Now()
	m := NewTimeInputModel(now.Add(-time.Hour), now)

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})

	if got := m.currentFocus(); got != "display" {
		t.Fatalf("focus = %q, want display", got)
	}
}

func TestTimeInputTabFromPresetFocusesInstantEvaluationTime(t *testing.T) {
	now := time.Now()
	m := NewTimeInputModel(now.Add(-time.Hour), now)
	m.SetInstant(true)

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if got := m.currentFocus(); got != "end" {
		t.Fatalf("focus = %q, want end", got)
	}
	if got := m.end.CursorPosition(); got != 0 {
		t.Fatalf("evaluation cursor = %d, want first segment", got)
	}
}

func TestTimeInputInstantTabReachesDisplayTime(t *testing.T) {
	now := time.Now()
	m := NewTimeInputModel(now.Add(-time.Hour), now)
	m.SetInstant(true)

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})

	if got := m.currentFocus(); got != "display" {
		t.Fatalf("focus = %q, want display", got)
	}
}

func TestTimeInputRangeEndCannotMoveBeforeStart(t *testing.T) {
	now := time.Now()
	m := NewTimeInputModel(now, now)
	m.FocusEnd()

	before := m.end.Time()
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})

	if !m.end.Time().Equal(before) {
		t.Fatalf("end changed to %s, want unchanged %s", m.end.Time(), before)
	}
}

func TestTimeInputRangeEndCannotMoveIntoFuture(t *testing.T) {
	now := time.Now()
	m := NewTimeInputModel(now.Add(-time.Hour), now)
	m.FocusEnd()

	before := m.end.Time()
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})

	if !m.end.Time().Equal(before) {
		t.Fatalf("end changed to %s, want unchanged %s", m.end.Time(), before)
	}
	if m.end.Time().After(time.Now()) {
		t.Fatalf("end moved into future: %s", m.end.Time())
	}
}

func TestTimeInputInstantEndCannotMoveIntoFuture(t *testing.T) {
	now := time.Now()
	m := NewTimeInputModel(now.Add(-time.Hour), now)
	m.SetInstant(true)
	m.FocusEnd()

	before := m.end.Time()
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})

	if !m.end.Time().Equal(before) {
		t.Fatalf("instant end changed to %s, want unchanged %s", m.end.Time(), before)
	}
	if m.end.Time().After(time.Now()) {
		t.Fatalf("instant end moved into future: %s", m.end.Time())
	}
}

func TestTimeInputInstantPresetKeepsSecondsStable(t *testing.T) {
	now := time.Date(2026, 6, 3, 12, 34, 42, 0, time.Local)
	m := NewTimeInputModel(now.Add(-time.Hour), now.Add(-time.Hour))
	m.SetInstant(true)

	before := m.end.Time()
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})

	if got := m.end.Time().Second(); got != before.Second() {
		t.Fatalf("seconds = %d, want %d", got, before.Second())
	}
	if diff := before.Sub(m.end.Time()); diff != 4*time.Hour {
		t.Fatalf("preset jump = %s, want 4h from 1 Hour to 5 Hours", diff)
	}
}

func TestTimeInputPresetIgnoresLeftRight(t *testing.T) {
	now := time.Date(2026, 6, 3, 12, 34, 42, 0, time.Local)
	m := NewTimeInputModel(now.Add(-time.Hour), now.Add(-time.Hour))
	m.SetInstant(true)

	before := m.end.Time()
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyLeft})

	if !m.end.Time().Equal(before) {
		t.Fatalf("evaluation time changed to %s, want unchanged %s", m.end.Time(), before)
	}
}

func TestTimeInputInstantEvaluationLeftRightMovesSegment(t *testing.T) {
	now := time.Now()
	m := NewTimeInputModel(now.Add(-time.Hour), now.Add(-time.Hour))
	m.SetInstant(true)
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRight})
	if got := m.end.CursorPosition(); got != 5 {
		t.Fatalf("cursor after right = %d, want month segment", got)
	}

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	if got := m.end.CursorPosition(); got != 0 {
		t.Fatalf("cursor after left = %d, want year segment", got)
	}
}

func TestTimeInputDisplayModeToggles(t *testing.T) {
	now := time.Now()
	m := NewTimeInputModel(now.Add(-time.Hour), now)
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})

	if got := m.DisplayMode(); got != TimeDisplayLocal {
		t.Fatalf("display mode = %q, want local", got)
	}

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRight})
	if got := m.DisplayMode(); got != TimeDisplayUTC {
		t.Fatalf("display mode = %q, want utc", got)
	}

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	if got := m.DisplayMode(); got != TimeDisplayLocal {
		t.Fatalf("display mode = %q, want local", got)
	}
}

func TestTimeInputViewLabelsResultDisplayMode(t *testing.T) {
	now := time.Now()
	m := NewTimeInputModel(now.Add(-time.Hour), now)

	view := m.View()
	if !strings.Contains(view, "QUERY TIME") {
		t.Fatalf("view should label query time picker, got:\n%s", view)
	}
	if !strings.Contains(view, "DISPLAY RESULTS AS") {
		t.Fatalf("view should label result display mode, got:\n%s", view)
	}
}
