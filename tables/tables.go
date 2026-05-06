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
// stripping them on the way in before calling [Parse]. See
// [WrapInBackticks] and [UnwrapFromBackticks] for the standard recipe.
//
// TableRef is not safe for concurrent mutation: callers must finish
// populating Table and TableParams before sharing a value across
// goroutines. Concurrent reads of an immutable TableRef are safe.
type TableRef struct {
	Table       string
	TableParams map[string]string
}

// Errors returned by [Parse] and [Validate]. Use [errors.Is] to
// match.
var (
	ErrSyntax            = errors.New("tables: syntax error")
	ErrUnknownTable      = errors.New("tables: unknown table")
	ErrUnknownParameter  = errors.New("tables: unknown parameter")
	ErrMissingRequired   = errors.New("tables: missing required parameter")
	ErrMissingDependency = errors.New("tables: missing parameter dependency")
	ErrDuplicateKey      = errors.New("tables: duplicate parameter key")
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
// those before calling Parse. Outer whitespace (any Unicode whitespace) is
// trimmed before parsing. See the package documentation for the full
// grammar.
//
// The table name must be non-empty after trimming. An empty parameter list
// (for example "events()") is normalised to no parameters, so the returned
// [TableRef.TableParams] is nil — identical to a reference with no parameter
// list at all.
//
// Parse performs only syntactic validation: it does not check whether the
// table or its parameters exist in any schema. Use [Validate] for that.
func Parse(s string) (TableRef, error) {
	s = strings.TrimSpace(s)
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

		if ref.TableParams == nil {
			ref.TableParams = make(map[string]string)
		}
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
//   - every required parameter declared on the table must be present;
//   - for every present parameter, every entry in its
//     [schemads.TableParameter.DependsOn] list must also be present.
//
// Validate aggregates issues: if multiple checks fail, the returned error
// joins them via [errors.Join] so callers can see every problem in one
// pass. Each component is matchable with [errors.Is]. The unknown-table
// check is fatal and short-circuits the rest of the checks.
//
// Validate does not check that values are members of
// [schemads.Schema.TableParameterValues]; callers that need value-set
// enforcement should perform that check themselves.
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

	// Iterate the present keys in sorted order so error ordering is
	// deterministic across runs and platforms.
	presentKeys := make([]string, 0, len(ref.TableParams))
	for k := range ref.TableParams {
		presentKeys = append(presentKeys, k)
	}
	sort.Strings(presentKeys)

	var errs []error
	for _, k := range presentKeys {
		if _, ok := declared[k]; !ok {
			errs = append(errs, fmt.Errorf("tables: %w: %q on table %q", ErrUnknownParameter, k, ref.Table))
		}
	}
	for _, p := range table.TableParameters {
		if p.Required {
			if _, ok := ref.TableParams[p.Name]; !ok {
				errs = append(errs, fmt.Errorf("tables: %w: %q on table %q", ErrMissingRequired, p.Name, ref.Table))
			}
		}
	}
	for _, k := range presentKeys {
		decl, ok := declared[k]
		if !ok {
			continue // already reported as unknown
		}
		for _, dep := range decl.DependsOn {
			if _, present := ref.TableParams[dep]; !present {
				errs = append(errs, fmt.Errorf("tables: %w: %q requires %q on table %q",
					ErrMissingDependency, k, dep, ref.Table))
			}
		}
	}
	return errors.Join(errs...)
}

// Canonicalize parses s and re-encodes it via [TableRef.String], returning
// the canonical form. It is shorthand for `Parse(s).String()` and is the
// recommended way to normalise a reference before using it as a map key,
// cache key, or identity comparison value: the parser is intentionally
// lenient (whitespace around separators, empty parameter lists, etc.)
// while the encoder is strict, so two semantically-equal raw inputs may
// not be byte-equal until canonicalised.
//
// Canonicalize returns the same errors as [Parse].
func Canonicalize(s string) (string, error) {
	ref, err := Parse(s)
	if err != nil {
		return "", err
	}
	return ref.String(), nil
}

// WrapInBackticks returns s wrapped in a pair of backtick delimiters,
// with any backticks in s doubled (`` `` ``) so that the result can be
// unambiguously [UnwrapFromBackticks]'d back into the original string.
//
// Use this when embedding a [TableRef.String] output (or any other
// payload) inside a wider grammar that uses backticks as identifier
// delimiters. The doubling rule is the same one used by SQL identifier
// quoting (e.g. ANSI double quotes, MySQL backticks).
func WrapInBackticks(s string) string {
	var sb strings.Builder
	sb.Grow(len(s) + 2)
	sb.WriteByte('`')
	for i := 0; i < len(s); i++ {
		if s[i] == '`' {
			sb.WriteByte('`')
		}
		sb.WriteByte(s[i])
	}
	sb.WriteByte('`')
	return sb.String()
}

// UnwrapFromBackticks reverses [WrapInBackticks]: it strips the outer
// backtick delimiters and un-doubles any escaped backticks inside.
//
// It returns [ErrSyntax] if s is not wrapped in backticks or contains an
// unescaped backtick inside the wrapped content.
func UnwrapFromBackticks(s string) (string, error) {
	if len(s) < 2 || s[0] != '`' || s[len(s)-1] != '`' {
		return "", fmt.Errorf("tables: %w: input is not wrapped in backticks", ErrSyntax)
	}
	inner := s[1 : len(s)-1]
	var sb strings.Builder
	sb.Grow(len(inner))
	for i := 0; i < len(inner); i++ {
		if inner[i] != '`' {
			sb.WriteByte(inner[i])
			continue
		}
		if i+1 >= len(inner) || inner[i+1] != '`' {
			return "", fmt.Errorf("tables: %w: unescaped backtick at byte %d in wrapped content", ErrSyntax, i+1)
		}
		sb.WriteByte('`')
		i++ // consume the doubled backtick
	}
	return sb.String(), nil
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
