package codex

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/cloudvane/litefactory/internal/agent"
)

// validateMCPConfig inspects TOML keys, not arbitrary text values. It is an
// isolation check, not a general TOML validator: the host still owns semantic
// validation. Ambiguous syntax is refused before launch. Both input and nesting
// are bounded, and comments/string values cannot introduce table declarations.
func validateMCPConfig(data []byte) error {
	if len(data) > 1<<20 || !utf8.Valid(data) {
		return fmt.Errorf("%w: invalid or oversized codex MCP config", agent.ErrInvalidRequest)
	}
	p := mcpConfigScanner{source: string(data)}
	if err := p.document(); err != nil {
		return fmt.Errorf("%w: inspect codex MCP config: %w", agent.ErrInvalidRequest, err)
	}
	return nil
}

type mcpConfigToken struct {
	kind byte // k: key/value text; q: quoted text; m: multiline text
	text string
}

type mcpConfigScanner struct {
	source string
	pos    int
	peeked *mcpConfigToken
}

func (p *mcpConfigScanner) next() (mcpConfigToken, error) {
	if p.peeked != nil {
		t := *p.peeked
		p.peeked = nil
		return t, nil
	}
	for p.pos < len(p.source) {
		c := p.source[p.pos]
		if c == ' ' || c == '\t' || c == '\r' {
			p.pos++
			continue
		}
		if c == '#' {
			for p.pos < len(p.source) && p.source[p.pos] != '\n' {
				p.pos++
			}
			continue
		}
		p.pos++
		if strings.ContainsRune("\n.=[]{} ,", rune(c)) {
			return mcpConfigToken{kind: c}, nil
		}
		if c == '"' || c == '\'' {
			start := p.pos
			multi := strings.HasPrefix(p.source[start:], strings.Repeat(string(c), 2))
			if multi {
				p.pos += 2
			}
			for p.pos < len(p.source) {
				if c == '"' && p.source[p.pos] == '\\' {
					p.pos += 2
					continue
				}
				if p.source[p.pos] == c {
					if multi {
						if !strings.HasPrefix(p.source[p.pos:], strings.Repeat(string(c), 3)) {
							p.pos++
							continue
						}
						p.pos += 3
						// TOML permits one or two extra closing quote characters.
						for n := 0; n < 2 && p.pos < len(p.source) && p.source[p.pos] == c; n++ {
							p.pos++
						}
						return mcpConfigToken{kind: 'm'}, nil
					}
					value := p.source[start:p.pos]
					p.pos++
					if c == '"' {
						decoded, err := strconv.Unquote("\"" + value + "\"")
						if err != nil {
							return mcpConfigToken{}, fmt.Errorf("invalid quoted config string")
						}
						value = decoded
					}
					return mcpConfigToken{kind: 'q', text: value}, nil
				}
				p.pos++
			}
			return mcpConfigToken{}, fmt.Errorf("unterminated config string")
		}
		start := p.pos - 1
		for p.pos < len(p.source) && !strings.ContainsRune(" \t\r\n.=[]{} ,#\"'", rune(p.source[p.pos])) {
			p.pos++
		}
		return mcpConfigToken{kind: 'k', text: p.source[start:p.pos]}, nil
	}
	return mcpConfigToken{}, nil
}

func (p *mcpConfigScanner) peek() (mcpConfigToken, error) {
	t, err := p.next()
	if err == nil {
		p.peeked = &t
	}
	return t, err
}

func (p *mcpConfigScanner) expect(kind byte) error {
	t, err := p.next()
	if err != nil {
		return err
	}
	if t.kind != kind {
		return fmt.Errorf("unexpected config syntax")
	}
	return nil
}

func (p *mcpConfigScanner) key() ([]string, error) {
	var path []string
	for {
		t, err := p.next()
		if err != nil {
			return nil, err
		}
		if t.kind != 'k' && t.kind != 'q' {
			return nil, fmt.Errorf("invalid config key")
		}
		path = append(path, t.text)
		t, err = p.peek()
		if err != nil || t.kind != '.' {
			return path, err
		}
		if err := p.expect('.'); err != nil {
			return nil, err
		}
	}
}

func reservedMCPPath(path []string) error {
	if len(path) >= 2 && path[0] == "mcp_servers" && path[1] == mcpServerName {
		return fmt.Errorf("config collides with the reserved %s MCP server", mcpServerName)
	}
	return nil
}

func (p *mcpConfigScanner) document() error {
	var table []string
	for {
		t, err := p.peek()
		if err != nil || t.kind == 0 {
			return err
		}
		if t.kind == '\n' {
			if err := p.expect('\n'); err != nil {
				return err
			}
			continue
		}
		if t.kind == '[' {
			if err := p.expect('['); err != nil {
				return err
			}
			t, err = p.peek()
			if err != nil {
				return err
			}
			array := t.kind == '['
			if array {
				if err := p.expect('['); err != nil {
					return err
				}
			}
			table, err = p.key()
			if err != nil {
				return err
			}
			if err := reservedMCPPath(table); err != nil {
				return err
			}
			if err := p.expect(']'); err != nil {
				return err
			}
			if array {
				if err := p.expect(']'); err != nil {
					return err
				}
			}
		} else if err := p.assignment(table, 0); err != nil {
			return err
		}
		t, err = p.peek()
		if err != nil {
			return err
		}
		if t.kind != 0 && t.kind != '\n' {
			return fmt.Errorf("unexpected trailing config syntax")
		}
	}
}

func (p *mcpConfigScanner) assignment(prefix []string, depth int) error {
	key, err := p.key()
	if err != nil {
		return err
	}
	path := append(append([]string(nil), prefix...), key...)
	if err := reservedMCPPath(path); err != nil {
		return err
	}
	if err := p.expect('='); err != nil {
		return err
	}
	return p.value(path, depth)
}

func (p *mcpConfigScanner) value(path []string, depth int) error {
	if depth > 64 {
		return fmt.Errorf("config nesting exceeds isolation check limit")
	}
	t, err := p.next()
	if err != nil {
		return err
	}
	if t.kind == '{' || t.kind == '[' {
		closing := byte('}')
		if t.kind == '[' {
			closing = ']'
		}
		for {
			next, err := p.peek()
			if err != nil {
				return err
			}
			if next.kind == closing {
				return p.expect(closing)
			}
			if next.kind == '\n' {
				if err := p.expect('\n'); err != nil {
					return err
				}
				continue
			}
			if t.kind == '{' {
				err = p.assignment(path, depth+1)
			} else {
				err = p.value(path, depth+1)
			}
			if err != nil {
				return err
			}
			next, err = p.peek()
			if err != nil {
				return err
			}
			if next.kind == ',' {
				if err := p.expect(','); err != nil {
					return err
				}
			} else if next.kind != closing && next.kind != '\n' {
				return fmt.Errorf("invalid compound config value")
			}
		}
	}
	if t.kind != 'k' && t.kind != 'q' && t.kind != 'm' {
		return fmt.Errorf("missing config value")
	}
	// Scalars include dotted floats and date/time values. Their contents are
	// irrelevant to server identity; delimiters remain for the enclosing parser.
	for {
		t, err := p.peek()
		if err != nil || t.kind == 0 || strings.ContainsRune("\n,]}", rune(t.kind)) {
			return err
		}
		if t.kind != 'k' && t.kind != '.' {
			return fmt.Errorf("invalid scalar config value")
		}
		if _, err := p.next(); err != nil {
			return err
		}
	}
}
