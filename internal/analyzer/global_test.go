package analyzer

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bardisty/ficha/internal/models"
)

// --- 1A: TestSortProjectsBy ---

func TestSortProjectsBy(t *testing.T) {
	t1 := time.Date(2024, 1, 10, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2024, 1, 20, 0, 0, 0, 0, time.UTC)
	t3 := time.Date(2024, 1, 30, 0, 0, 0, 0, time.UTC)

	makeProjects := func() []models.ProjectAnalysis {
		return []models.ProjectAnalysis{
			{
				ProjectInfo:  models.ProjectInfo{DisplayName: "bravo"},
				TotalCost:    models.CostBreakdown{TotalCost: 5.0},
				SessionCount: 3,
				LastActive:   t2,
			},
			{
				ProjectInfo:  models.ProjectInfo{DisplayName: "alpha"},
				TotalCost:    models.CostBreakdown{TotalCost: 10.0},
				SessionCount: 1,
				LastActive:   t3,
			},
			{
				ProjectInfo:  models.ProjectInfo{DisplayName: "charlie"},
				TotalCost:    models.CostBreakdown{TotalCost: 2.0},
				SessionCount: 7,
				LastActive:   t1,
			},
		}
	}

	tests := []struct {
		name          string
		sortBy        string
		expectedOrder []string // DisplayName order after sort
	}{
		{
			name:          "cost descending (default)",
			sortBy:        "cost",
			expectedOrder: []string{"alpha", "bravo", "charlie"},
		},
		{
			name:          "sessions descending",
			sortBy:        "sessions",
			expectedOrder: []string{"charlie", "bravo", "alpha"},
		},
		{
			name:          "name ascending",
			sortBy:        "name",
			expectedOrder: []string{"alpha", "bravo", "charlie"},
		},
		{
			name:          "activity descending",
			sortBy:        "activity",
			expectedOrder: []string{"alpha", "bravo", "charlie"},
		},
		{
			name:          "unknown defaults to cost",
			sortBy:        "unknown",
			expectedOrder: []string{"alpha", "bravo", "charlie"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			projects := makeProjects()
			SortProjectsBy(projects, tt.sortBy)

			for i, expected := range tt.expectedOrder {
				if projects[i].DisplayName != expected {
					t.Errorf("position %d: got %s, want %s", i, projects[i].DisplayName, expected)
				}
			}
		})
	}
}

// --- 1B: TestAnalyzeAllProjects_Empty ---

func TestAnalyzeAllProjects_Empty(t *testing.T) {
	tests := []struct {
		name     string
		projects []models.ProjectInfo
	}{
		{name: "nil input", projects: nil},
		{name: "empty slice", projects: []models.ProjectInfo{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := AnalyzeAllProjects(tt.projects)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result == nil {
				t.Fatal("result should not be nil")
			}
			if result.ProjectCount != 0 {
				t.Errorf("ProjectCount: got %d, want 0", result.ProjectCount)
			}
			if result.SessionCount != 0 {
				t.Errorf("SessionCount: got %d, want 0", result.SessionCount)
			}
			if result.MessageCount != 0 {
				t.Errorf("MessageCount: got %d, want 0", result.MessageCount)
			}
			if result.TotalCost.TotalCost != 0 {
				t.Errorf("TotalCost: got %f, want 0", result.TotalCost.TotalCost)
			}
			if result.CostByModel == nil {
				t.Error("CostByModel should be initialized (non-nil)")
			}
			if len(result.CostByModel) != 0 {
				t.Errorf("CostByModel should be empty, got %d entries", len(result.CostByModel))
			}
		})
	}
}

// --- 1C: TestAnalyzeAllProjects_WithProjects ---

// writeJSONLFile is a test helper that writes JSONL lines to a file.
func writeJSONLFile(t *testing.T, path string, lines []string) {
	t.Helper()
	content := ""
	for i, line := range lines {
		if i > 0 {
			content += "\n"
		}
		content += line
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write JSONL file %s: %v", path, err)
	}
}

func TestAnalyzeAllProjects_WithProjects(t *testing.T) {
	// Create two project directories with JSONL files.
	// Project 1: 1 session with 2 messages
	// Project 2: 1 session with 1 message
	tmpDir := t.TempDir()

	proj1Dir := filepath.Join(tmpDir, "project-1")
	proj2Dir := filepath.Join(tmpDir, "project-2")
	if err := os.MkdirAll(proj1Dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(proj2Dir, 0755); err != nil {
		t.Fatal(err)
	}

	// Project 1: session with 2 assistant messages (1000 input + 500 output each)
	// Expected cost per message: (1000/1e6)*3 + (500/1e6)*15 = 0.003 + 0.0075 = 0.0105
	// Total for project 1: 0.0105 * 2 = 0.021
	writeJSONLFile(t, filepath.Join(proj1Dir, "sess-aaa.jsonl"), []string{
		`{"type":"assistant","timestamp":"2024-01-15T10:00:00Z","message":{"model":"claude-sonnet-4-5","usage":{"input_tokens":1000,"output_tokens":500}}}`,
		`{"type":"assistant","timestamp":"2024-01-15T10:05:00Z","message":{"model":"claude-sonnet-4-5","usage":{"input_tokens":1000,"output_tokens":500}}}`,
	})

	// Project 2: session with 1 assistant message (2000 input + 1000 output)
	// Expected cost: (2000/1e6)*3 + (1000/1e6)*15 = 0.006 + 0.015 = 0.021
	writeJSONLFile(t, filepath.Join(proj2Dir, "sess-bbb.jsonl"), []string{
		`{"type":"assistant","timestamp":"2024-01-16T12:00:00Z","message":{"model":"claude-sonnet-4-5","usage":{"input_tokens":2000,"output_tokens":1000}}}`,
	})

	projects := []models.ProjectInfo{
		{
			EncodedPath:  "project-1",
			FullPath:     proj1Dir,
			OriginalPath: "/home/test/project-1",
			DisplayName:  "project-1",
		},
		{
			EncodedPath:  "project-2",
			FullPath:     proj2Dir,
			OriginalPath: "/home/test/project-2",
			DisplayName:  "project-2",
		},
	}

	result, err := AnalyzeAllProjects(projects)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Both projects have sessions
	if result.ProjectCount != 2 {
		t.Errorf("ProjectCount: got %d, want 2", result.ProjectCount)
	}
	if result.SessionCount != 2 {
		t.Errorf("SessionCount: got %d, want 2", result.SessionCount)
	}

	// MessageCount: 2 + 1 = 3
	if result.MessageCount != 3 {
		t.Errorf("MessageCount: got %d, want 3", result.MessageCount)
	}

	// Total cost: 0.021 + 0.021 = 0.042
	expectedTotal := 0.042
	if !almostEqual(result.TotalCost.TotalCost, expectedTotal, 0.001) {
		t.Errorf("TotalCost: got %f, want %f", result.TotalCost.TotalCost, expectedTotal)
	}

	// CostByModel should have claude-sonnet-4-5
	if _, ok := result.CostByModel["claude-sonnet-4-5"]; !ok {
		t.Error("CostByModel missing claude-sonnet-4-5")
	}

	// Projects should be sorted by cost descending. Both have same cost (0.021),
	// so order may vary -- just verify both are present and sorted.
	if len(result.Projects) != 2 {
		t.Errorf("Projects length: got %d, want 2", len(result.Projects))
	}
	for i := 1; i < len(result.Projects); i++ {
		if result.Projects[i].TotalCost.TotalCost > result.Projects[i-1].TotalCost.TotalCost {
			t.Errorf("projects not sorted by cost descending: [%d]=%f > [%d]=%f",
				i, result.Projects[i].TotalCost.TotalCost,
				i-1, result.Projects[i-1].TotalCost.TotalCost)
		}
	}

	if result.SkippedProjects != 0 {
		t.Errorf("SkippedProjects: got %d, want 0", result.SkippedProjects)
	}
}

// --- 1D: TestAnalyzeAllProjects_ErrorProject ---

func TestAnalyzeAllProjects_ErrorProject(t *testing.T) {
	tmpDir := t.TempDir()

	// One valid project
	validDir := filepath.Join(tmpDir, "valid-project")
	if err := os.MkdirAll(validDir, 0755); err != nil {
		t.Fatal(err)
	}
	writeJSONLFile(t, filepath.Join(validDir, "sess-ok.jsonl"), []string{
		`{"type":"assistant","timestamp":"2024-01-15T10:00:00Z","message":{"model":"claude-sonnet-4-5","usage":{"input_tokens":1000,"output_tokens":500}}}`,
	})

	projects := []models.ProjectInfo{
		{
			EncodedPath:  "valid-project",
			FullPath:     validDir,
			OriginalPath: "/home/test/valid-project",
			DisplayName:  "valid-project",
		},
		{
			EncodedPath:  "bad-project",
			FullPath:     filepath.Join(tmpDir, "nonexistent-dir"),
			OriginalPath: "/home/test/bad-project",
			DisplayName:  "bad-project",
		},
	}

	result, err := AnalyzeAllProjects(projects)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The invalid project should be skipped
	if result.SkippedProjects != 1 {
		t.Errorf("SkippedProjects: got %d, want 1", result.SkippedProjects)
	}

	// The valid project should still succeed
	if result.ProjectCount != 1 {
		t.Errorf("ProjectCount: got %d, want 1", result.ProjectCount)
	}
	if result.TotalCost.TotalCost <= 0 {
		t.Error("TotalCost should be > 0 from the valid project")
	}
	if result.SessionCount != 1 {
		t.Errorf("SessionCount: got %d, want 1", result.SessionCount)
	}
}
