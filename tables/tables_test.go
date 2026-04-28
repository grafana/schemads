package tables_test

import (
	"errors"
	"testing"
	"testing/quick"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	schemads "github.com/grafana/schemads"
	"github.com/grafana/schemads/tables"
)

func TestParse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want tables.TableRef
	}{
		{
			name: "table only",
			in:   "events",
			want: tables.TableRef{Table: "events"},
		},
		{
			name: "empty parameter list",
			in:   "events()",
			want: tables.TableRef{Table: "events", TableParams: map[string]string{}},
		},
		{
			name: "single parameter",
			in:   "events(env=prod)",
			want: tables.TableRef{Table: "events", TableParams: map[string]string{"env": "prod"}},
		},
		{
			name: "multiple parameters",
			in:   "events(env=prod,service=tempo)",
			want: tables.TableRef{Table: "events", TableParams: map[string]string{"env": "prod", "service": "tempo"}},
		},
		{
			name: "tolerates whitespace around separators",
			in:   "events( env = prod , service = tempo )",
			want: tables.TableRef{Table: "events", TableParams: map[string]string{"env": "prod", "service": "tempo"}},
		},
		{
			name: "empty value",
			in:   "events(env=)",
			want: tables.TableRef{Table: "events", TableParams: map[string]string{"env": ""}},
		},
		{
			name: "empty value before another parameter",
			in:   "events(env=,foo=bar)",
			want: tables.TableRef{Table: "events", TableParams: map[string]string{"env": "", "foo": "bar"}},
		},
		{
			name: "value with escaped parens",
			in:   "tags(name=Promo \\(2024\\))",
			want: tables.TableRef{Table: "tags", TableParams: map[string]string{"name": "Promo (2024)"}},
		},
		{
			name: "value with escaped comma",
			in:   "t(k=a\\,b)",
			want: tables.TableRef{Table: "t", TableParams: map[string]string{"k": "a,b"}},
		},
		{
			name: "value with escaped equals",
			in:   "t(k=a\\=b)",
			want: tables.TableRef{Table: "t", TableParams: map[string]string{"k": "a=b"}},
		},
		{
			name: "value with escaped backslash",
			in:   "t(k=a\\\\b)",
			want: tables.TableRef{Table: "t", TableParams: map[string]string{"k": `a\b`}},
		},
		{
			name: "value with literal backtick is unescaped",
			in:   "t(k=a`b)",
			want: tables.TableRef{Table: "t", TableParams: map[string]string{"k": "a`b"}},
		},
		{
			name: "value preserves internal whitespace",
			in:   "t(k=foo bar)",
			want: tables.TableRef{Table: "t", TableParams: map[string]string{"k": "foo bar"}},
		},
		{
			name: "value preserves multibyte runes",
			in:   "metrics(unit=µs,note=π)",
			want: tables.TableRef{Table: "metrics", TableParams: map[string]string{"unit": "µs", "note": "π"}},
		},
		{
			name: "table name with escaped reserved char",
			in:   "weird\\(name(a=1)",
			want: tables.TableRef{Table: "weird(name", TableParams: map[string]string{"a": "1"}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := tables.Parse(tc.in)
			require.NoError(t, err)
			assert.Equal(t, tc.want.Table, got.Table)
			assert.Equal(t, tc.want.TableParams, got.TableParams)
		})
	}
}

func TestParseErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		in      string
		wantErr error
	}{
		{name: "empty input", in: "", wantErr: tables.ErrSyntax},
		{name: "empty table with params", in: "(a=1)", wantErr: tables.ErrSyntax},
		{name: "empty table with empty params", in: "()", wantErr: tables.ErrSyntax},
		{name: "unterminated parameter list", in: "events(a=1", wantErr: tables.ErrSyntax},
		{name: "trailing data after closing paren", in: "events(a=1)x", wantErr: tables.ErrSyntax},
		{name: "duplicate parameter key", in: "t(a=1,a=2)", wantErr: tables.ErrDuplicateKey},
		{name: "empty key", in: "t(=v)", wantErr: tables.ErrSyntax},
		{name: "missing equals", in: "t(a)", wantErr: tables.ErrSyntax},
		{name: "invalid escape sequence in value", in: "t(k=a\\xb)", wantErr: tables.ErrSyntax},
		{name: "dangling backslash in value", in: "t(k=a\\)", wantErr: tables.ErrSyntax},
		{name: "unescaped equals in table name", in: "weird=name(a=1)", wantErr: tables.ErrSyntax},
		{name: "unescaped paren in value", in: "t(k=a(b)", wantErr: tables.ErrSyntax},
		{name: "unexpected text after table name without paren", in: "events,extra", wantErr: tables.ErrSyntax},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := tables.Parse(tc.in)
			require.Error(t, err)
			assert.ErrorIs(t, err, tc.wantErr, "got error: %v", err)
		})
	}
}

func TestTableRefString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   tables.TableRef
		want string
	}{
		{
			name: "empty ref",
			in:   tables.TableRef{},
			want: "",
		},
		{
			name: "table only",
			in:   tables.TableRef{Table: "events"},
			want: "events",
		},
		{
			name: "empty params map collapses",
			in:   tables.TableRef{Table: "events", TableParams: map[string]string{}},
			want: "events",
		},
		{
			name: "single param",
			in:   tables.TableRef{Table: "events", TableParams: map[string]string{"env": "prod"}},
			want: "events(env=prod)",
		},
		{
			name: "params sorted alphabetically",
			in:   tables.TableRef{Table: "events", TableParams: map[string]string{"service": "tempo", "env": "prod"}},
			want: "events(env=prod,service=tempo)",
		},
		{
			name: "escapes reserved chars in value",
			in:   tables.TableRef{Table: "tags", TableParams: map[string]string{"name": "Promo (2024)"}},
			want: "tags(name=Promo \\(2024\\))",
		},
		{
			name: "escapes reserved chars in table and key",
			in:   tables.TableRef{Table: "weird(name", TableParams: map[string]string{"a=b": "v"}},
			want: "weird\\(name(a\\=b=v)",
		},
		{
			name: "escapes backslash but leaves backtick literal",
			in:   tables.TableRef{Table: "t", TableParams: map[string]string{"k": "a\\b`c"}},
			want: "t(k=a\\\\b`c)",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, tc.in.String())
		})
	}
}

func TestRoundTrip(t *testing.T) {
	t.Parallel()

	// hasEdgeWS reports whether s starts or ends with ASCII whitespace.
	// Such values are not round-trippable: the lenient parser strips
	// leading/trailing whitespace as separator padding (documented).
	hasEdgeWS := func(s string) bool {
		if s == "" {
			return false
		}
		first, last := s[0], s[len(s)-1]
		return first == ' ' || first == '\t' || last == ' ' || last == '\t'
	}

	// Property: for any TableRef with a non-empty table, non-empty keys, and no
	// leading/trailing ASCII whitespace in any name or value,
	// Parse(ref.String()) == ref.
	property := func(table string, paramKeys []string, paramVals []string) bool {
		if table == "" || hasEdgeWS(table) {
			return true
		}
		if len(paramKeys) > 8 {
			paramKeys = paramKeys[:8]
		}
		if len(paramVals) < len(paramKeys) {
			return true // shrink to a representable shape
		}
		params := make(map[string]string, len(paramKeys))
		for i, k := range paramKeys {
			if k == "" || hasEdgeWS(k) || hasEdgeWS(paramVals[i]) {
				return true
			}
			params[k] = paramVals[i]
		}

		ref := tables.TableRef{Table: table}
		if len(params) > 0 {
			ref.TableParams = params
		}
		out := ref.String()
		got, err := tables.Parse(out)
		if err != nil {
			t.Logf("Parse(%q) returned error: %v", out, err)
			return false
		}
		if got.Table != ref.Table {
			t.Logf("table mismatch: got %q want %q (encoded %q)", got.Table, ref.Table, out)
			return false
		}
		if len(got.TableParams) != len(ref.TableParams) {
			t.Logf("params length mismatch: got %v want %v (encoded %q)", got.TableParams, ref.TableParams, out)
			return false
		}
		for k, v := range ref.TableParams {
			gv, ok := got.TableParams[k]
			if !ok || gv != v {
				t.Logf("param %q mismatch: got %q want %q (encoded %q)", k, gv, v, out)
				return false
			}
		}
		return true
	}

	if err := quick.Check(property, &quick.Config{MaxCount: 1000}); err != nil {
		t.Error(err)
	}
}

func TestEdgeWhitespaceLimitation(t *testing.T) {
	t.Parallel()

	// Document the round-trip limitation explicitly: leading/trailing ASCII
	// whitespace inside a value is treated as separator padding by the
	// lenient parser and is not preserved. Internal whitespace is preserved.
	in := tables.TableRef{Table: "t", TableParams: map[string]string{"k": "  v  "}}
	out := in.String()
	got, err := tables.Parse(out)
	require.NoError(t, err)
	assert.Equal(t, "v", got.TableParams["k"], "leading/trailing whitespace is stripped")

	in = tables.TableRef{Table: "t", TableParams: map[string]string{"k": "a  b"}}
	got, err = tables.Parse(in.String())
	require.NoError(t, err)
	assert.Equal(t, "a  b", got.TableParams["k"], "internal whitespace is preserved")
}

func TestRoundTripFixedCases(t *testing.T) {
	t.Parallel()

	// A handful of explicitly-chosen tricky inputs in addition to the
	// random property test.
	cases := []tables.TableRef{
		{Table: "t"},
		{Table: "t", TableParams: map[string]string{"k": ""}},
		{Table: "t", TableParams: map[string]string{"": ""}}, // pathological key, should still encode but Parse rejects
		{Table: "tab,le", TableParams: map[string]string{"k(=)": "v\\`,()=v"}},
		{Table: "µ", TableParams: map[string]string{"π": "3.14159"}},
		{Table: "t", TableParams: map[string]string{"a": "1", "b": "2", "c": "3"}},
	}
	for i, ref := range cases {
		out := ref.String()
		got, err := tables.Parse(out)
		// TableRefs with an empty parameter key are not parseable (empty key is
		// rejected). Skip the round-trip assertion in that case but make
		// sure the parser does reject as expected.
		if _, hasEmpty := ref.TableParams[""]; hasEmpty {
			require.ErrorIs(t, err, tables.ErrSyntax, "case %d: %#v -> %q", i, ref, out)
			continue
		}
		require.NoError(t, err, "case %d: %#v -> %q", i, ref, out)
		assert.Equal(t, ref.Table, got.Table, "case %d", i)
		// Allow nil vs empty-map equivalence.
		if len(ref.TableParams) == 0 {
			assert.Empty(t, got.TableParams, "case %d", i)
			continue
		}
		assert.Equal(t, ref.TableParams, got.TableParams, "case %d", i)
	}
}

func TestValidate(t *testing.T) {
	t.Parallel()

	schema := &schemads.Schema{
		Tables: []schemads.Table{
			{
				Name: "events",
				TableParameters: []schemads.TableParameter{
					{Name: "service", Root: true, Required: true},
					{Name: "env", Root: true},
				},
			},
		},
	}

	tests := []struct {
		name    string
		ref     tables.TableRef
		wantErr error
	}{
		{
			name: "valid with required only",
			ref:  tables.TableRef{Table: "events", TableParams: map[string]string{"service": "tempo"}},
		},
		{
			name: "valid with required and optional",
			ref:  tables.TableRef{Table: "events", TableParams: map[string]string{"service": "tempo", "env": "prod"}},
		},
		{
			name:    "unknown table",
			ref:     tables.TableRef{Table: "missing", TableParams: map[string]string{"service": "tempo"}},
			wantErr: tables.ErrUnknownTable,
		},
		{
			name:    "missing required",
			ref:     tables.TableRef{Table: "events", TableParams: map[string]string{"env": "prod"}},
			wantErr: tables.ErrMissingRequired,
		},
		{
			name:    "unknown parameter",
			ref:     tables.TableRef{Table: "events", TableParams: map[string]string{"service": "tempo", "bogus": "x"}},
			wantErr: tables.ErrUnknownParameter,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := tables.Validate(tc.ref, schema)
			if tc.wantErr == nil {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.True(t, errors.Is(err, tc.wantErr), "want %v, got %v", tc.wantErr, err)
		})
	}
}

func TestValidateNilSchema(t *testing.T) {
	t.Parallel()
	err := tables.Validate(tables.TableRef{Table: "x"}, nil)
	require.Error(t, err)
}
