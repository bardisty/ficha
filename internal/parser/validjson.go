package parser

import "bytes"

// maxJSONDepth is how deep encoding/json lets arrays and objects nest before
// it calls the input invalid. The scan has to agree, or a line nested deeper
// would stop counting as malformed.
const maxJSONDepth = 10000

// droppable reports whether line is valid JSON that the parse would decode
// and then drop: an object whose type is neither "assistant" nor "ai-title".
// Most of a transcript's bytes are such lines (tool results, file snapshots,
// pasted images), and one pass here costs a fraction of json.Unmarshal, which
// validates the line and then walks it again to step over every field.
//
// A false answer promises nothing. The caller then decodes the line, so an
// invalid line still counts as malformed and a line this scan can't judge
// still gets encoding/json's verdict.
func droppable(line []byte) bool {
	valid, mayKeep := scanLine(line)
	return valid && !mayKeep
}

// scanLine reports whether line is valid JSON by encoding/json's rules, and
// whether decoding it could yield a record the parse keeps. mayKeep means
// anything only when valid is true.
func scanLine(line []byte) (valid, mayKeep bool) {
	s := lineScanner{b: line}
	s.space()
	if s.peek() == '{' {
		valid = s.object(true)
	} else {
		// Not an object: decoding it into a record either fails or, for
		// null, does nothing. Either way it is encoding/json's call.
		s.mayKeep = true
		valid = s.value()
	}
	if valid {
		s.space()
		valid = s.i == len(line)
	}
	return valid, s.mayKeep
}

// lineScanner walks one line of JSON. It validates as encoding/json does,
// which is looser than the JSON spec in one way that matters here: bytes that
// aren't valid UTF-8 are fine inside a string.
type lineScanner struct {
	b     []byte
	i     int
	depth int
	// mayKeep is set once the line could decode to a kept record, or once
	// ruling that out would take encoding/json's own key matching.
	mayKeep bool
	sawType bool
}

// peek returns the next byte, or 0 at the end of the line. A zero byte is
// valid nowhere outside a string, so callers need no separate end check.
func (s *lineScanner) peek() byte {
	if s.i < len(s.b) {
		return s.b[s.i]
	}
	return 0
}

func (s *lineScanner) space() {
	for s.i < len(s.b) {
		switch s.b[s.i] {
		case ' ', '\t', '\r', '\n':
			s.i++
		default:
			return
		}
	}
}

func (s *lineScanner) value() bool {
	switch c := s.peek(); {
	case c == '"':
		_, _, ok := s.str()
		return ok
	case c == '{':
		return s.object(false)
	case c == '[':
		return s.array()
	case c == '-' || (c >= '0' && c <= '9'):
		return s.number()
	case c == 't':
		return s.literal("true")
	case c == 'f':
		return s.literal("false")
	case c == 'n':
		return s.literal("null")
	}
	return false
}

func (s *lineScanner) literal(lit string) bool {
	if !bytes.HasPrefix(s.b[s.i:], []byte(lit)) {
		return false
	}
	s.i += len(lit)
	return true
}

func (s *lineScanner) digits() bool {
	start := s.i
	for c := s.peek(); c >= '0' && c <= '9'; c = s.peek() {
		s.i++
	}
	return s.i > start
}

func (s *lineScanner) number() bool {
	if s.peek() == '-' {
		s.i++
	}
	// A leading zero stands alone: "01" ends the number at the 0, and the 1
	// then fails whatever the caller expects next.
	if s.peek() == '0' {
		s.i++
	} else if !s.digits() {
		return false
	}
	if s.peek() == '.' {
		s.i++
		if !s.digits() {
			return false
		}
	}
	if c := s.peek(); c == 'e' || c == 'E' {
		s.i++
		if c := s.peek(); c == '+' || c == '-' {
			s.i++
		}
		return s.digits()
	}
	return true
}

// str scans a string from its opening quote and returns its bytes between
// the quotes, and whether any of them is a backslash escape.
func (s *lineScanner) str() (raw []byte, escaped, ok bool) {
	b := s.b
	start := s.i + 1
	for i := start; i < len(b); i++ {
		c := b[i]
		if plainStringByte[c] {
			continue
		}
		switch {
		case c == '"':
			s.i = i + 1
			return b[start:i], escaped, true
		case c == '\\':
			escaped = true
			i++
			if i == len(b) {
				return nil, false, false
			}
			switch b[i] {
			case '"', '\\', '/', 'b', 'f', 'n', 'r', 't':
			case 'u':
				if i+4 >= len(b) || !isHex(b[i+1]) || !isHex(b[i+2]) || !isHex(b[i+3]) || !isHex(b[i+4]) {
					return nil, false, false
				}
				i += 4
			default:
				return nil, false, false
			}
		case c < 0x20:
			return nil, false, false
		}
	}
	return nil, false, false
}

// plainStringByte marks the bytes a string holds as they are: everything but
// the quote, the backslash and the control characters. Long tool results are
// nearly all such bytes, so the scan's speed is how fast it steps over them.
var plainStringByte = func() (plain [256]bool) {
	for c := 0x20; c < 256; c++ {
		plain[c] = c != '"' && c != '\\'
	}
	return plain
}()

func isHex(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

// enter steps into an array or object, past its opening bracket.
func (s *lineScanner) enter() bool {
	s.i++
	s.depth++
	return s.depth <= maxJSONDepth
}

func (s *lineScanner) array() bool {
	if !s.enter() {
		return false
	}
	s.space()
	if s.peek() == ']' {
		s.i++
		s.depth--
		return true
	}
	for {
		s.space()
		if !s.value() {
			return false
		}
		s.space()
		switch s.peek() {
		case ',':
			s.i++
		case ']':
			s.i++
			s.depth--
			return true
		default:
			return false
		}
	}
}

// object scans an object. For the line's own object, top is true and each
// key is checked for being the record's type: only that one decides whether
// the parse keeps the line. A "type" nested in message or content never does.
func (s *lineScanner) object(top bool) bool {
	if !s.enter() {
		return false
	}
	s.space()
	if s.peek() == '}' {
		s.i++
		s.depth--
		return true
	}
	for {
		s.space()
		if s.peek() != '"' {
			return false
		}
		key, escaped, ok := s.str()
		if !ok {
			return false
		}
		s.space()
		if s.peek() != ':' {
			return false
		}
		s.i++
		s.space()
		if top {
			ok = s.topValue(key, escaped)
		} else {
			ok = s.value()
		}
		if !ok {
			return false
		}
		s.space()
		switch s.peek() {
		case ',':
			s.i++
		case '}':
			s.i++
			s.depth--
			return true
		default:
			return false
		}
	}
}

// topValue scans the value of one of the line's own keys, and sets mayKeep
// unless the key and value leave the record's type certain.
//
// encoding/json matches keys without regard to case, with Unicode folding
// beyond ASCII, so "TYPE" is the type key and a key with an escape or a
// non-ASCII byte might be. When a key repeats, the last value wins, except
// that a null leaves the earlier one in place. A second type key is left to
// the decoder for that reason, as is any value but a string spelled without
// escapes.
func (s *lineScanner) topValue(key []byte, escaped bool) bool {
	if escaped || !isASCII(key) {
		s.mayKeep = true
		return s.value()
	}
	if !bytes.EqualFold(key, []byte("type")) {
		return s.value()
	}
	if s.sawType || s.peek() != '"' {
		s.mayKeep = true
		return s.value()
	}
	s.sawType = true
	typ, escaped, ok := s.str()
	if escaped || string(typ) == "assistant" || string(typ) == "ai-title" {
		s.mayKeep = true
	}
	return ok
}

func isASCII(b []byte) bool {
	for _, c := range b {
		if c >= 0x80 {
			return false
		}
	}
	return true
}
