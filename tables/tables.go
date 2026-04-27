package tables

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	schemads "github.com/grafana/schemads"
)

// TableRef is a parameterised reference to a table. The zero value encodes
// as the empty string; callers should typically construct one via [Parse]
// or by populating Table (and optionally TableParams) directly.
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

// String returns the canonical encoded form of the reference, suitable for
// round-tripping through [Parse]. The output has no outer delimiters:
// callers that need to embed the reference in a larger grammar (for
// example, wrapping in backticks) must add those delimiters themselves.
//
// Parameters are emitted in sorted key order with no surrounding
// whitespace; reserved characters in the table name, keys, and values are
// backslash-escaped.
//
// String never returns a malformed reference for any [TableRef] value.
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
// An empty input decodes to the zero [TableRef] (an empty table name with
// no parameters); semantic checks such as table existence are deferred to
// [Validate].
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

// ParseLegacy is a best-effort decoder for the deprecated underscore
// separated table reference form:
//
//	<table>_<param1Value>_<param2Value>_...
//
// where parameter values are positional and appear in the order declared by
// [schemads.Table.TableParameters]. For example, with a table "issues" whose
// declared parameters are ("organization", "repository"), the legacy string
// "issues_grafana_loki" decodes to {Table:"issues", TableParams:{"organization":
// "grafana", "repository":"loki"}}.
//
// The legacy form is fundamentally ambiguous: table names and parameter
// values may both contain underscores, and parameter names are not encoded
// in the string. ParseLegacy resolves this with a schema-aware best-effort
// algorithm:
//
//  1. Find every table whose name is a "_"-delimited prefix of s and which
//     can accept the number of remaining "_"-separated fields, accounting
//     for trailing optional parameters that may have been omitted by the
//     producer.
//  2. Among those, pick the candidate with the longest matching table name.
//  3. Bind the trailing fields positionally to the first len(values)
//     parameters in [Table.TableParameters] order; any unbound parameters
//     (always trailing optionals) are absent from the resulting
//     [TableRef.TableParams] map.
//
// A candidate is rejected when:
//   - The number of values is less than the table's required-parameter count
//     (so at least one required parameter would be unbound).
//   - The number of values exceeds the table's total parameter count (so
//     there are extra fields with nothing to bind to).
//   - Any parameter that would be skipped (i.e. not bound because the input
//     ran out of values) is marked Required.
//
// If no candidate matches, ParseLegacy returns [ErrSyntax]. Use this only
// to migrate callers off the legacy form: new code should round-trip through
// [Parse] and [TableRef.String].
//
// Limitations:
//   - Parameter values containing "_" cannot be recovered: each "_" is
//     treated as a separator.
//   - When a table name is itself a prefix of another and both have a
//     parameter count consistent with the input, the longer name wins.
//   - Only trailing optional parameters can be omitted. An optional
//     parameter declared in the middle (or beginning) of
//     [Table.TableParameters], followed by a required one, cannot be
//     omitted because the parser cannot tell where the gap is.
func ParseLegacy(s string, schema *schemads.Schema) (TableRef, error) {
	if schema == nil {
		return TableRef{}, errors.New("tables: schema is nil")
	}
	if s == "" {
		return TableRef{}, fmt.Errorf("tables: empty input: %w", ErrSyntax)
	}

	type candidate struct {
		table  *schemads.Table
		values []string
	}
	var best *candidate
	bestPrefixLen := -1

	for i := range schema.Tables {
		t := &schema.Tables[i]
		name := t.Name
		if name == "" || !strings.HasPrefix(s, name) {
			continue
		}
		var remainder string
		switch {
		case len(s) == len(name):
			remainder = ""
		case s[len(name)] == '_':
			remainder = s[len(name)+1:]
		default:
			// Matched name is a prefix of a longer identifier (e.g. table
			// "issue" against input "issuess..."); not a real match.
			continue
		}

		var values []string
		if remainder != "" {
			values = strings.Split(remainder, "_")
		}

		requiredCount := 0
		for _, p := range t.TableParameters {
			if p.Required {
				requiredCount++
			}
		}
		if len(values) < requiredCount || len(values) > len(t.TableParameters) {
			continue
		}
		// The trailing parameters we'd skip must all be optional; we cannot
		// omit a required parameter because positional binding gives us no
		// way to indicate where in the sequence it would have appeared.
		skippedHasRequired := false
		for _, p := range t.TableParameters[len(values):] {
			if p.Required {
				skippedHasRequired = true
				break
			}
		}
		if skippedHasRequired {
			continue
		}

		if len(name) > bestPrefixLen {
			bestPrefixLen = len(name)
			best = &candidate{table: t, values: values}
		}
	}

	if best == nil {
		return TableRef{}, fmt.Errorf("tables: %w: %q does not match any table in schema", ErrSyntax, s)
	}

	ref := TableRef{Table: best.table.Name}
	if len(best.values) > 0 {
		ref.TableParams = make(map[string]string, len(best.values))
		for i, v := range best.values {
			ref.TableParams[best.table.TableParameters[i].Name] = v
		}
	}
	return ref, nil
}

// ParseWithFallback decodes a table reference in either the canonical form
// (see [Parse]) or the legacy underscore-separated form (see [ParseLegacy]),
// dispatching based on whether the input contains an unescaped opening
// parenthesis.
//
// Dispatch:
//
//   - If the input contains an unescaped "(", it is canonical syntax with
//     a parameter list. ParseWithFallback calls [Parse] only and returns
//     its result; ParseLegacy is never consulted because the legacy form
//     cannot contain "(".
//   - Otherwise the input has no parenthesised parameter list and is
//     ambiguous between a 0-parameter canonical reference and the legacy
//     underscore form. ParseWithFallback tries [ParseLegacy] first (the
//     more specific interpretation) and falls back to [Parse] if
//     ParseLegacy reports no match. When both fail, the returned error
//     joins the two via [errors.Join] so callers can inspect both reasons;
//     either component is matchable with [errors.Is].
//
// The dispatch heuristic is purely syntactic: it does not consult the
// schema for the canonical case. Callers that know an input came from a
// specific producer should call [Parse] or [ParseLegacy] directly rather
// than rely on this routing.
func ParseWithFallback(s string, schema *schemads.Schema) (TableRef, error) {
	if hasUnescapedParen(s) {
		return Parse(s)
	}
	legacyRef, legacyErr := ParseLegacy(s, schema)
	if legacyErr == nil {
		return legacyRef, nil
	}
	ref, parseErr := Parse(s)
	if parseErr == nil {
		return ref, nil
	}
	return TableRef{}, errors.Join(legacyErr, parseErr)
}

// hasUnescapedParen reports whether s contains a "(" that is not consumed
// by a preceding "\" escape. It mirrors the canonical escape grammar by
// stepping over any "\X" pair without inspecting X, which handles escaped
// parens ("\(") and escaped backslashes ("\\(") correctly without needing
// to know which characters are reserved.
func hasUnescapedParen(s string) bool {
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++ // consume the escaped byte; bounds checked by the loop
		case '(':
			return true
		}
	}
	return false
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
