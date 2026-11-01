package parser

import (
	"encoding/json"
	"errors"
	"io"
	"os"

	"github.com/bardisty/ficha/internal/models"
)

// FirstReplyID returns the message.id of the first reply in a transcript,
// reading only as far as that reply, or "" when no reply carries one. A fork
// copies its original from the start, so every session in a fork family
// holds the first reply of the session the family began with.
func FirstReplyID(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	reader := newLineReader(f)
	for {
		line, oversized, err := reader.next(maxLineBytes)
		if errors.Is(err, io.EOF) {
			return "", nil
		}
		if err != nil {
			return "", err
		}
		if oversized || len(line) == 0 || droppable(line) {
			continue
		}
		var msg models.JSONLMessage
		if json.Unmarshal(line, &msg) != nil {
			continue
		}
		if msg.Type == "assistant" && msg.Message != nil && msg.Message.ID != "" {
			return msg.Message.ID, nil
		}
	}
}
