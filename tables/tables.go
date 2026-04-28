package tables

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	schemads "github.com/grafana/schemads"
)

// TableRef is a parameterised reference to a table. The zero value encodes
// as the empty string, but that string is not accepted by [Parse]; callers
// should typically construct one via [Parse] or by populating Table (and
// optionally TableParams) directly.
//
// The encoded form does not include any outer delimiters. If the surrounding
// system wraps references in delimiters (for example, backticks in a query
// language), callers are responsible for adding them on the way out and
// stripping them on the way in before calling [Parse].
type TableRef struct {
	Table       string
	TableParams map[string]string
}

// Errors returned by [Parse] and [Validate]. Use [errors.Is] to
// match.
var (
	ErrSyntax           = errors.New("tables: syntax error")
	ErrUnknownTable     = errors.New("tables: unknown table")
	ErrUnknownParameter = errors.New("tables: unknown parameter")
	ErrMissingRequired  = errors.New("tables: missing required parameter")
	ErrDuplicateKey     = errors.New("tables: duplicate parameter key")
)

// String returns the canonical encoded form of the reference. References with
// a non-empty table name and non-empty parameter keys are suitable for
// round-tripping through [Parse]. The output has no outer delimiters:
// callers that need to embed the reference in a larger grammar (for
// example, wrapping in backticks) must add those delimiters themselves.
//
// Parameters are emitted in sorted key order with no surrounding
// whitespace; reserved characters in the table name, keys, and values are
// backslash-escaped.
//
// String does not validate TableRef; an empty table name or empty parameter
// key will produce output that [Parse] rejects.
func (r TableRef) String() string {
	var sb strings.Builder
	writeEscaped(&sb, r.Table)
	if len(r.TableParams) > 0 {
		sb.WriteByte('(')
		keys := make([]string, 0, len(r.TableParams))
		for k := range r.TableParams {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for i, k := range keys {
			if i > 0 {
				sb.WriteByte(',')
			}
			writeEscaped(&sb, k)
			sb.WriteByte('=')
			writeEscaped(&sb, r.TableParams[k])
		}
		sb.WriteByte(')')
	}
	return sb.String()
}

// Parse decodes the canonical string form of a table reference. The input
// must NOT include any surrounding delimiters such as backticks; callers
// embedding the reference in a wider grammar are responsible for stripping
// those before calling Parse. See the package documentation for the full
// grammar.
//
// The table name must be non-empty. Semantic checks such as table existence
// are deferred to [Validate].
//
// Parse performs only syntactic validation: it does not check whether the
// table or its parameters exist in any schema. Use [Validate] for that.
func Parse(s string) (TableRef, error) {
	p := parser{src: s}

	table, err := p.readChars(true)
	if err != nil {
		return TableRef{}, err
	}
	table = strings.TrimRight(table, " \t")
	if table == "" {
		return TableRef{}, p.errf("empty table name")
	}
	ref := TableRef{Table: table}

	if p.pos == len(p.src) {
		return ref, nil
	}
	if p.peek() != '(' {
		return TableRef{}, p.errf("unexpected %q after table name", p.peek())
	}
	p.advance()
	p.skipWS()

	ref.TableParams = make(map[string]string)
	if p.peek() == ')' {
		p.advance()
		if p.pos != len(p.src) {
			return TableRef{}, p.errf("trailing data after closing %q", byte(')'))
		}
		return ref, nil
	}
	for {
		p.skipWS()
		key, err := p.readChars(false)
		if err != nil {
			return TableRef{}, err
		}
		key = strings.TrimRight(key, " \t")
		if key == "" {
			return TableRef{}, p.errf("empty parameter key")
		}
		if p.peek() != '=' {
			return TableRef{}, p.errf("expected %q after parameter key, got %q", byte('='), p.peek())
		}
		p.advance()
		p.skipWS()
		value, err := p.readChars(false)
		if err != nil {
			return TableRef{}, err
		}
		value = strings.TrimRight(value, " \t")

		if _, dup := ref.TableParams[key]; dup {
			return TableRef{}, fmt.Errorf("tables: %w: %q", ErrDuplicateKey, key)
		}
		ref.TableParams[key] = value

		switch p.peek() {
		case ',':
			p.advance()
			continue
		case ')':
			p.advance()
			if p.pos != len(p.src) {
				return TableRef{}, p.errf("trailing data after closing %q", byte(')'))
			}
			return ref, nil
		case 0:
			return TableRef{}, p.errf("unterminated parameter list")
		default:
			return TableRef{}, p.errf("unexpected %q in parameter list", p.peek())
		}
	}
}

// Validate checks a decoded reference against a [schemads.Schema]:
//
//   - the table must exist in the schema;
//   - every key in [TableRef.TableParams] must be a declared parameter on that table;
//   - every required parameter declared on the table must be present.
//
// Validate does not check that values are members of
// [schemads.Schema.TableParameterValues] or that the dependency chain
// between non-root parameters is satisfied; callers should perform those
// checks themselves if needed.
func Validate(ref TableRef, schema *schemads.Schema) error {
	if schema == nil {
		return errors.New("tables: schema is nil")
	}
	var table *schemads.Table
	for i := range schema.Tables {
		if schema.Tables[i].Name == ref.Table {
			table = &schema.Tables[i]
			break
		}
	}
	if table == nil {
		return fmt.Errorf("tables: %w: %q", ErrUnknownTable, ref.Table)
	}

	declared := make(map[string]schemads.TableParameter, len(table.TableParameters))
	for _, p := range table.TableParameters {
		declared[p.Name] = p
	}
	for k := range ref.TableParams {
		if _, ok := declared[k]; !ok {
			return fmt.Errorf("tables: %w: %q on table %q", ErrUnknownParameter, k, ref.Table)
		}
	}
	for _, p := range table.TableParameters {
		if !p.Required {
			continue
		}
		if _, ok := ref.TableParams[p.Name]; !ok {
			return fmt.Errorf("tables: %w: %q on table %q", ErrMissingRequired, p.Name, ref.Table)
		}
	}
	return nil
}

func isReserved(b byte) bool {
	switch b {
	case '(', ')', ',', '=', '\\':
		return true
	}
	return false
}

func writeEscaped(sb *strings.Builder, s string) {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if isReserved(c) {
			sb.WriteByte('\\')
		}
		sb.WriteByte(c)
	}
}

type parser struct {
	src string
	pos int
}

func (p *parser) peek() byte {
	if p.pos >= len(p.src) {
		return 0
	}
	return p.src[p.pos]
}

func (p *parser) advance() { p.pos++ }

func (p *parser) skipWS() {
	for p.pos < len(p.src) && (p.src[p.pos] == ' ' || p.src[p.pos] == '\t') {
		p.pos++
	}
}

func (p *parser) errf(format string, args ...any) error {
	return fmt.Errorf("tables: at byte %d: "+format+": %w",
		append([]any{p.pos}, append(args, ErrSyntax)...)...)
}

// readChars reads characters until it encounters an unescaped reserved
// character that terminates the current production (or end-of-input).
//
// At the table-name level (topLevel=true) only "(" terminates; "," "=" ")"
// inside a table name must be escaped.
//
// Inside a parameter (topLevel=false), "," "=" ")" terminate; "(" must be
// escaped.
func (p *parser) readChars(topLevel bool) (string, error) {
	var sb strings.Builder
	for p.pos < len(p.src) {
		c := p.src[p.pos]
		if c == '\\' {
			if p.pos+1 >= len(p.src) {
				return "", p.errf("dangling backslash at end of input")
			}
			esc := p.src[p.pos+1]
			if !isReserved(esc) {
				p.pos++ // point error at the bad escape char
				return "", p.errf(`invalid escape "\%c"`, esc)
			}
			sb.WriteByte(esc)
			p.pos += 2
			continue
		}
		if topLevel {
			if c == '(' {
				return sb.String(), nil
			}
			if c == ')' || c == ',' || c == '=' {
				return "", p.errf("unescaped %q in table name", c)
			}
		} else {
			if c == ',' || c == '=' || c == ')' {
				return sb.String(), nil
			}
			if c == '(' {
				return "", p.errf("unescaped %q in parameter", c)
			}
		}
		sb.WriteByte(c)
		p.pos++
	}
	return sb.String(), nil
}
