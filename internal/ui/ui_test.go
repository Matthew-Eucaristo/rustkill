package ui

import (
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Matthew-Eucaristo/rustkill/internal/clean"
	"github.com/Matthew-Eucaristo/rustkill/internal/scan"
	"github.com/Matthew-Eucaristo/rustkill/internal/testfix"
)

func model(t *testing.T) *Model {
	t.Helper()
	root := testfix.Real(t, t.TempDir())
	testfix.Write(t, filepath.Join(root, "shop"), testfix.Target(testfix.Crate("shop"), "target"))
	testfix.Repo(t, filepath.Join(root, "shop"), testfix.Old)
	testfix.Write(t, filepath.Join(root, "api"), testfix.Target(testfix.Crate("api"), "target"))
	testfix.Repo(t, filepath.Join(root, "api"), testfix.Old)
	desk := testfix.Target(testfix.Crate("desk"), "target")
	desk["target/release/bundle/dmg/Desk.dmg"] = "installer"
	testfix.Write(t, filepath.Join(root, "desk"), desk)
	testfix.Repo(t, filepath.Join(root, "desk"), testfix.Old)
	opts := scan.Options{Roots: []string{root}, Home: root, Env: scan.DefaultEnv()}
	res, err := scan.Scan(opts)
	if err != nil {
		t.Fatal(err)
	}
	res.MeasureAll()
	m := New(opts, clean.Options{Mode: clean.DryRun, Scan: opts}, 0)
	m.Update(tea.WindowSizeMsg{Width: 140, Height: 32})
	m.res = res
	m.cursorKey = firstItem(m.rows())
	return m
}

func press(m *Model, keys ...string) {
	for _, k := range keys {
		msg := tea.KeyPressMsg{Text: k}
		switch k {
		case "space":
			msg = tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
		case "enter":
			msg = tea.KeyPressMsg{Code: tea.KeyEnter}
		case "esc":
			msg = tea.KeyPressMsg{Code: tea.KeyEscape}
		case "tab":
			msg = tea.KeyPressMsg{Code: tea.KeyTab}
		default:
			msg.Code = []rune(k)[0]
		}
		m.Update(msg)
	}
}

func screen(m *Model) string { return ansi.Strip(m.render()) }

func TestRenderAndSelect(t *testing.T) {
	m := model(t)
	s := screen(m)
	for _, want := range []string{"rustkill", "dry run", "target", "safe", "review", "deployable", "edition 2024 · axum"} {
		if !strings.Contains(s, want) {
			t.Errorf("screen lacks %q:\n%s", want, s)
		}
	}
	press(m, "s")
	if len(m.selected) != 2 {
		t.Fatalf("s selected %d, want the 2 safe items", len(m.selected))
	}
	press(m, "enter")
	if s := screen(m); !strings.Contains(s, "Dry run: nothing will change") || !strings.Contains(s, "Move to the "+clean.TrashName) {
		t.Fatalf("confirm screen:\n%s", s)
	}
	press(m, "esc")
	if m.confirm != nil || len(m.selected) != 2 {
		t.Fatal("esc should cancel the confirmation and keep the selection")
	}
}

func TestGroupingFilterLegend(t *testing.T) {
	m := model(t)
	press(m, "tab")
	if !strings.Contains(screen(m), "by framework") || !strings.Contains(screen(m), "axum") {
		t.Error("framework grouping")
	}
	press(m, "tab")
	if !strings.Contains(screen(m), "idle 90+ days") {
		t.Errorf("age grouping:\n%s", screen(m))
	}
	press(m, "/", "z", "z", "z", "enter")
	if !strings.Contains(screen(m), "Nothing matches") {
		t.Error("filter")
	}
	press(m, "esc", "?")
	if !strings.Contains(screen(m), "Safety") {
		t.Error("legend")
	}
}

func TestFitsEveryWidth(t *testing.T) {
	m := model(t)
	m.res.Warnings = []string{"2 folder(s) could not be read (permission denied), e.g. ~/x; projects inside them are not listed"}
	for _, w := range []int{60, 80, 100, 140} {
		for _, h := range []int{16, 24, 40} {
			m.Update(tea.WindowSizeMsg{Width: w, Height: h})
			for _, mode := range []string{"list", "legend", "confirm"} {
				m.legend, m.confirm = mode == "legend", nil
				if mode == "confirm" {
					m.confirm = []string{m.res.Items[0].ID}
				}
				lines := strings.Split(screen(m), "\n")
				if len(lines) > h {
					t.Errorf("%dx%d %s: %d lines", w, h, mode, len(lines))
				}
				for _, l := range lines {
					if ansi.StringWidth(l) > w {
						t.Errorf("%dx%d %s: line wider than the terminal: %q", w, h, mode, l)
					}
				}
			}
			m.legend, m.confirm = false, nil
			s := screen(m)
			for _, want := range []string{"rustkill", "? legend", "q quit", "⚠"} {
				if !strings.Contains(s, want) {
					t.Errorf("%dx%d lacks %q:\n%s", w, h, want, s)
				}
			}
		}
	}
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 30})
	if s := screen(m); !strings.Contains(s, "target") || !strings.Contains(s, "safe") {
		t.Errorf("80 columns must still show the item and its level:\n%s", s)
	}
}

func TestCursorStaysOnTopWhileMeasuring(t *testing.T) {
	m := model(t)
	rows := m.rows()
	last := rows[len(rows)-1]
	m.cursorKey = last.key()
	m.Update(measuredMsg(scan.Measure(m.res.Items[0])))
	if m.cursorKey != firstItem(m.rows()) {
		t.Error("cursor should return to the first item until the user moves")
	}
	press(m, "j")
	k := m.cursorKey
	m.Update(measuredMsg(scan.Measure(m.res.Items[0])))
	if m.cursorKey != k {
		t.Error("cursor must stay where the user put it")
	}
}

func TestNothingToCleanSaysSo(t *testing.T) {
	m := model(t)
	for _, it := range m.res.Items {
		it.Safety = scan.Safety{Level: scan.Protected}
	}
	if s := screen(m); !strings.Contains(s, "0 B freeable") {
		t.Errorf("protected items are not freeable:\n%s", s)
	}
	m.confirm = []string{m.res.Items[0].ID}
	if s := screen(m); !strings.Contains(s, "Nothing here can be cleaned") || strings.Contains(s, "Proceed?") {
		t.Errorf("confirm with nothing to run:\n%s", s)
	}
}

func TestBulkSkipsEmptyItems(t *testing.T) {
	m := model(t)
	for _, it := range m.res.Items {
		if it.Safety.Level == scan.Safe {
			it.Size.Allocated = 0
		}
	}
	press(m, "a")
	if len(m.selected) != 0 {
		t.Errorf("a selected %d empty items", len(m.selected))
	}
}
