package schemas

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateSchema(t *testing.T) {
	tests := []struct {
		name    string
		schema  *Schema
		wantErr string
	}{
		{
			name:   "nil schema",
			schema: nil,
		},
		{
			name: "valid schema without sub-tables",
			schema: &Schema{
				Tables: []Table{
					{Name: "issues", Columns: []Column{{Name: "id", Type: ColumnTypeInt64}}},
				},
			},
		},
		{
			name: "valid GitHub-style hierarchy",
			schema: &Schema{
				Tables: []Table{{
					Name: "issues",
					SubTables: []SubTable{
						{Name: "organization", Root: true, Required: true},
						{Name: "repository", DependsOn: []string{"organization"}, Required: true},
					},
					Columns: []Column{{Name: "title", Type: ColumnTypeString}},
				}},
				SubTableValues: map[string]map[string][]string{
					"issues": {
						"organization": {"grafana", "kubernetes"},
					},
				},
			},
		},
		{
			name: "valid three-level hierarchy",
			schema: &Schema{
				Tables: []Table{{
					Name: "pull_requests",
					SubTables: []SubTable{
						{Name: "organization", Root: true, Required: true},
						{Name: "repository", DependsOn: []string{"organization"}, Required: true},
						{Name: "branch", DependsOn: []string{"repository"}},
					},
				}},
			},
		},
		{
			name: "duplicate sub-table names",
			schema: &Schema{
				Tables: []Table{{
					Name: "issues",
					SubTables: []SubTable{
						{Name: "organization", Root: true},
						{Name: "organization", Root: true},
					},
				}},
			},
			wantErr: "duplicate sub-table name",
		},
		{
			name: "DependsOn references non-existent sub-table",
			schema: &Schema{
				Tables: []Table{{
					Name: "issues",
					SubTables: []SubTable{
						{Name: "organization", Root: true},
						{Name: "repository", DependsOn: []string{"nonexistent"}},
					},
				}},
			},
			wantErr: "depends on non-existent sub-table",
		},
		{
			name: "deep shared dependency graph",
			schema: &Schema{
				Tables: []Table{{
					Name: "issues",
					SubTables: []SubTable{
						{Name: "a", Root: true},
						{Name: "b", DependsOn: []string{"a"}},
						{Name: "c", DependsOn: []string{"b"}},
						{Name: "d", DependsOn: []string{"c", "b"}},
						{Name: "x", DependsOn: []string{"d"}},
						{Name: "y", DependsOn: []string{"x", "a"}},
						{Name: "cycle_start", DependsOn: []string{"y"}},
						{Name: "cycle_mid", DependsOn: []string{"cycle_start"}},
						{Name: "cycle_end", DependsOn: []string{"cycle_mid", "cycle_start"}},
					},
				}},
			},
		},
		{
			name: "self-referencing dependency",
			schema: &Schema{
				Tables: []Table{{
					Name: "issues",
					SubTables: []SubTable{
						{Name: "organization", Root: true},
						{Name: "self", DependsOn: []string{"self"}},
					},
				}},
			},
			wantErr: "circular dependency",
		},
		{
			name: "root with non-empty DependsOn",
			schema: &Schema{
				Tables: []Table{{
					Name: "issues",
					SubTables: []SubTable{
						{Name: "organization", Root: true, DependsOn: []string{"repository"}},
						{Name: "repository"},
					},
				}},
			},
			wantErr: "marked as root but has dependencies",
		},
		{
			name: "no root sub-table present",
			schema: &Schema{
				Tables: []Table{{
					Name: "issues",
					SubTables: []SubTable{
						{Name: "repository", DependsOn: []string{"organization"}},
						{Name: "organization"},
					},
				}},
			},
			wantErr: "none are marked as root",
		},
		{
			name: "required depends on non-required",
			schema: &Schema{
				Tables: []Table{{
					Name: "issues",
					SubTables: []SubTable{
						{Name: "organization", Root: true, Required: false},
						{Name: "repository", DependsOn: []string{"organization"}, Required: true},
					},
				}},
			},
			wantErr: "non-required sub-table",
		},
		{
			name: "SubTableValues references non-existent table",
			schema: &Schema{
				Tables: []Table{
					{Name: "issues", SubTables: []SubTable{{Name: "organization", Root: true}}},
				},
				SubTableValues: map[string]map[string][]string{
					"nonexistent": {"organization": {"val"}},
				},
			},
			wantErr: "non-existent table",
		},
		{
			name: "SubTableValues references non-existent sub-table",
			schema: &Schema{
				Tables: []Table{
					{Name: "issues", SubTables: []SubTable{{Name: "organization", Root: true}}},
				},
				SubTableValues: map[string]map[string][]string{
					"issues": {"nonexistent": {"val"}},
				},
			},
			wantErr: "non-existent sub-table",
		},
		{
			name: "valid SubTableValues for root sub-tables only",
			schema: &Schema{
				Tables: []Table{{
					Name: "issues",
					SubTables: []SubTable{
						{Name: "organization", Root: true, Required: true},
						{Name: "repository", DependsOn: []string{"organization"}, Required: true},
					},
				}},
				SubTableValues: map[string]map[string][]string{
					"issues": {
						"organization": {"grafana", "kubernetes"},
					},
				},
			},
		},
		{
			name: "SubTableValues rejects non-root sub-table",
			schema: &Schema{
				Tables: []Table{{
					Name: "issues",
					SubTables: []SubTable{
						{Name: "organization", Root: true, Required: true},
						{Name: "repository", DependsOn: []string{"organization"}, Required: true},
					},
				}},
				SubTableValues: map[string]map[string][]string{
					"issues": {
						"repository": {"grafana", "loki"},
					},
				},
			},
			wantErr: "non-root sub-table",
		},
		{
			name: "empty sub-tables list is valid",
			schema: &Schema{
				Tables: []Table{{Name: "issues", SubTables: []SubTable{}}},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateSchema(tc.schema)
			if tc.wantErr == "" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.wantErr)
			}
		})
	}
}
