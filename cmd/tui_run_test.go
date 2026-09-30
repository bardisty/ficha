package cmd

import (
	"errors"
	"io"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// interruptModel sends itself tea.Interrupt, the message Bubble Tea turns a
// SIGINT into.
type interruptModel struct{}

func (interruptModel) Init() tea.Cmd                         { return tea.Interrupt }
func (m interruptModel) Update(tea.Msg) (tea.Model, tea.Cmd) { return m, nil }
func (interruptModel) View() string                          { return "" }

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
