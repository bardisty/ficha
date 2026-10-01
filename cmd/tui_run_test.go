package cmd

import (
	"bytes"
	"errors"
	"io"
	"runtime"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/bardisty/ficha/internal/styles"
)

// interruptModel sends itself tea.Interrupt, the message Bubble Tea turns a
// SIGINT into.
type interruptModel struct{}

func (interruptModel) Init() tea.Cmd                         { return tea.Interrupt }
func (m interruptModel) Update(tea.Msg) (tea.Model, tea.Cmd) { return m, nil }
func (interruptModel) View() tea.View                        { return tea.NewView("") }

func TestInterruptedTUIExits130Quietly(t *testing.T) {
	_, runErr := tea.NewProgram(interruptModel{}, tea.WithInput(nil), tea.WithOutput(io.Discard)).Run()
	if !errors.Is(runErr, tea.ErrInterrupted) {
		t.Fatalf("Run() = %v, want tea.ErrInterrupted", runErr)
	}
	err := tuiError(runErr)
	if got := exitCode(err); got != 130 {
		t.Errorf("exit status %d, want 130", got)
	}
	if err.Error() != "" {
		t.Errorf("an interrupt should print nothing, got %q", err.Error())
	}
}

func TestTUIErrorOtherwise(t *testing.T) {
	if err := tuiError(nil); err != nil {
		t.Errorf("a clean quit should be no error, got %v", err)
	}
	err := tuiError(tea.ErrProgramKilled)
	if got := exitCode(err); got != 1 {
		t.Errorf("a killed program should exit 1, got %d", got)
	}
	if err.Error() != "running TUI: program was killed" {
		t.Errorf("error text changed: %q", err.Error())
	}
}

// A view copies its spinner's and chart's colors when it's built, so the
// palette is picked before the view is, never after.
func TestLiveViewIsBuiltWithThePickedPalette(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("termenv reads no background on Windows")
	}
	lightTerminal(t)
	var out bytes.Buffer
	cfg := &config{format: "table", stdout: &out}
	root := newRootCmd()
	root.SetArgs(nil)
	resolveColor(root, cfg)

	darkAtBuild := true
	liveView(cfg, func() tea.Model {
		darkAtBuild = styles.Dark()
		return interruptModel{}
	})
	if darkAtBuild {
		t.Error("the view was built before the palette was picked")
	}
}

// A dimension the terminal reports as zero gets a size to draw at; a real
// one, and every other message, passes through.
func TestSizeOrFallback(t *testing.T) {
	for _, tt := range []struct{ in, want tea.WindowSizeMsg }{
		{tea.WindowSizeMsg{}, tea.WindowSizeMsg{Width: 80, Height: 24}},
		{tea.WindowSizeMsg{Width: 120}, tea.WindowSizeMsg{Width: 120, Height: 24}},
		{tea.WindowSizeMsg{Height: 50}, tea.WindowSizeMsg{Width: 80, Height: 50}},
		{tea.WindowSizeMsg{Width: 30, Height: 3}, tea.WindowSizeMsg{Width: 30, Height: 3}},
	} {
		if got := sizeOrFallback(nil, tt.in); got != tt.want {
			t.Errorf("sizeOrFallback(%+v) = %+v, want %+v", tt.in, got, tt.want)
		}
	}
	if got := sizeOrFallback(nil, tea.QuitMsg{}); got != (tea.QuitMsg{}) {
		t.Errorf("sizeOrFallback changed a %T", got)
	}
}
