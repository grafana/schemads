// Package tables encodes and decodes the canonical, human-writable string
// form of a parameterised table reference used by schemads consumers.
//
// # Format
//
// A table reference is an undelimited string of the form:
//
//	<table>(<key1>=<value1>,<key2>=<value2>,...)
//
// The table name is required. The parameter list is optional; both `events`
// and `events()` decode to a reference with zero parameters. Values are
// user-supplied free text; names (table and parameter keys) are typically
// constrained by the producing schema but the format itself imposes no
// character restrictions on them beyond requiring a non-empty table name.
//
// The format does not include any outer delimiters. If the surrounding
// system wraps a reference in delimiters (for example, backticks in a
// query language), callers must add them on the way out and strip them on
// the way in before calling [Parse].
//
// # Grammar
//
//	ref          = table [ "(" params ")" ]
//	table        = chars ; non-empty after trimming separator whitespace
//	params       = pair { "," [ ws ] pair }
//	pair         = key [ ws ] "=" [ ws ] value
//	key          = chars
//	value        = chars
//	chars        = { unescaped | escape }
//	unescaped    = any UTF-8 codepoint EXCEPT { "(", ")", ",", "=", "\" }
//	escape       = "\" ( "(" | ")" | "," | "=" | "\" )
//	ws           = { " " | "\t" }
//
// # Escaping
//
// The five characters "(", ")", ",", "=", and "\" are reserved. Inside a
// table name, parameter key, or value they must be backslash-escaped:
//
//	tags(name=Promo \(2024\))        // value contains "Promo (2024)"
//	t(k=a\,b)                        // value contains "a,b"
//	t(k=a\=b)                        // value contains "a=b"
//	t(k=a\\b)                        // value contains `a\b`
//
// A backslash followed by any character outside the reserved set is a parse
// error: the escape grammar is closed and there is no implicit pass-through.
//
// Characters that are not reserved (including backticks) are passed through
// verbatim and do not need to be escaped.
//
// # Whitespace
//
// Decoding tolerates optional whitespace (spaces and tabs) immediately
// around "(", ")", "=", and ",". Whitespace *inside* a value is preserved
// verbatim. The encoder always emits the no-whitespace form, so both
// `t(a=1, b=2)` and `t(a=1,b=2)` decode identically and round-trip to the
// latter.
//
// As a consequence, leading and trailing ASCII whitespace in a parameter
// value (or key, or table name) is not preserved across round-trips: the
// parser strips it, treating it as separator padding. Internal whitespace
// between non-whitespace characters is preserved.
//
// # Empty values
//
// `t(k=)` decodes to {"k": ""} (an empty string value). A key that is
// absent from the parameter list is "unset", which is distinct from an empty
// string.
//
// # Validation
//
// [Parse] performs only syntactic validation. Use [Validate] to check a
// decoded reference against a [github.com/grafana/schemads.Schema]: that the
// table exists, that all parameter keys are declared on that table, and that
// every required parameter is present.
package tables
