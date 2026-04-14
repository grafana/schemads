package schemas

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildSQLQuery(t *testing.T) {
	tests := []struct {
		name        string
		query       Query
		dialect     SQLDialect
		expectedSQL string
		expectErr   bool
	}{
		{
			name: "no filters",
			query: Query{
				Table: "events",
			},
			dialect:     DialectMySQL,
			expectedSQL: "SELECT * FROM events",
		},
		{
			name: "equals filter",
			query: Query{
				Table: "users",
				Filters: []ColumnFilter{
					{
						Name: "name",
						Conditions: []FilterCondition{
							{Operator: OperatorEquals, Value: "Alice"},
						},
					},
				},
			},
			dialect:     DialectMySQL,
			expectedSQL: "SELECT * FROM users WHERE name = 'Alice'",
		},
		{
			name: "not equals filter",
			query: Query{
				Table: "users",
				Filters: []ColumnFilter{
					{
						Name: "status",
						Conditions: []FilterCondition{
							{Operator: OperatorNotEquals, Value: "inactive"},
						},
					},
				},
			},
			dialect:     DialectMySQL,
			expectedSQL: "SELECT * FROM users WHERE status <> 'inactive'",
		},
		{
			name: "greater than filter",
			query: Query{
				Table: "orders",
				Filters: []ColumnFilter{
					{
						Name: "amount",
						Conditions: []FilterCondition{
							{Operator: OperatorGreaterThan, Value: 100},
						},
					},
				},
			},
			dialect:     DialectMySQL,
			expectedSQL: "SELECT * FROM orders WHERE amount > 100",
		},
		{
			name: "greater than or equal filter",
			query: Query{
				Table: "orders",
				Filters: []ColumnFilter{
					{
						Name: "amount",
						Conditions: []FilterCondition{
							{Operator: OperatorGreaterThanOrEqual, Value: 50},
						},
					},
				},
			},
			dialect:     DialectMySQL,
			expectedSQL: "SELECT * FROM orders WHERE amount >= 50",
		},
		{
			name: "less than filter",
			query: Query{
				Table: "orders",
				Filters: []ColumnFilter{
					{
						Name: "quantity",
						Conditions: []FilterCondition{
							{Operator: OperatorLessThan, Value: 10},
						},
					},
				},
			},
			dialect:     DialectMySQL,
			expectedSQL: "SELECT * FROM orders WHERE quantity < 10",
		},
		{
			name: "less than or equal filter",
			query: Query{
				Table: "orders",
				Filters: []ColumnFilter{
					{
						Name: "quantity",
						Conditions: []FilterCondition{
							{Operator: OperatorLessThanOrEqual, Value: 5},
						},
					},
				},
			},
			dialect:     DialectMySQL,
			expectedSQL: "SELECT * FROM orders WHERE quantity <= 5",
		},
		{
			name: "like filter",
			query: Query{
				Table: "users",
				Filters: []ColumnFilter{
					{
						Name: "name",
						Conditions: []FilterCondition{
							{Operator: OperatorLike, Value: "%Alice%"},
						},
					},
				},
			},
			dialect:     DialectMySQL,
			expectedSQL: "SELECT * FROM users WHERE name LIKE '%Alice%'",
		},
		{
			name: "in filter",
			query: Query{
				Table: "users",
				Filters: []ColumnFilter{
					{
						Name: "status",
						Conditions: []FilterCondition{
							{Operator: OperatorIn, Values: []any{"active", "pending"}},
						},
					},
				},
			},
			dialect:     DialectMySQL,
			expectedSQL: "SELECT * FROM users WHERE status IN ('active', 'pending')",
		},
		{
			name: "multiple filters AND",
			query: Query{
				Table: "users",
				Filters: []ColumnFilter{
					{
						Name: "name",
						Conditions: []FilterCondition{
							{Operator: OperatorEquals, Value: "Alice"},
						},
					},
					{
						Name: "age",
						Conditions: []FilterCondition{
							{Operator: OperatorGreaterThan, Value: 30},
						},
					},
				},
			},
			dialect:     DialectMySQL,
			expectedSQL: "SELECT * FROM users WHERE name = 'Alice' AND age > 30",
		},
		{
			name: "multiple conditions on same column",
			query: Query{
				Table: "orders",
				Filters: []ColumnFilter{
					{
						Name: "amount",
						Conditions: []FilterCondition{
							{Operator: OperatorGreaterThanOrEqual, Value: 10},
							{Operator: OperatorLessThan, Value: 100},
						},
					},
				},
			},
			dialect:     DialectMySQL,
			expectedSQL: "SELECT * FROM orders WHERE amount >= 10 AND amount < 100",
		},
		{
			name: "value with single quote escaping",
			query: Query{
				Table: "users",
				Filters: []ColumnFilter{
					{
						Name: "name",
						Conditions: []FilterCondition{
							{Operator: OperatorEquals, Value: "O'Reilly"},
						},
					},
				},
			},
			dialect:     DialectMySQL,
			expectedSQL: "SELECT * FROM users WHERE name = 'O\\'Reilly'",
		},
		{
			name: "unsupported operator error",
			query: Query{
				Table: "users",
				Filters: []ColumnFilter{
					{
						Name: "name",
						Conditions: []FilterCondition{
							{Operator: Operator("unsupported"), Value: "test"},
						},
					},
				},
			},
			dialect:   DialectMySQL,
			expectErr: true,
		},
		{
			name: "postgresql dialect",
			query: Query{
				Table: "users",
				Filters: []ColumnFilter{
					{
						Name: "name",
						Conditions: []FilterCondition{
							{Operator: OperatorEquals, Value: "Alice"},
						},
					},
				},
			},
			dialect:     DialectPostgreSQL,
			expectedSQL: "SELECT * FROM users WHERE name = E'Alice'",
		},
		{
			name: "column projection",
			query: Query{
				Table:   "events",
				Columns: []string{"id", "name", "created_at"},
			},
			dialect:     DialectMySQL,
			expectedSQL: "SELECT id, name, created_at FROM events",
		},
		{
			name: "column projection with filters",
			query: Query{
				Table:   "users",
				Columns: []string{"id", "name"},
				Filters: []ColumnFilter{
					{
						Name: "status",
						Conditions: []FilterCondition{
							{Operator: OperatorEquals, Value: "active"},
						},
					},
				},
			},
			dialect:     DialectMySQL,
			expectedSQL: "SELECT id, name FROM users WHERE status = 'active'",
		},
		{
			name: "order by single column asc",
			query: Query{
				Table:   "events",
				OrderBy: []OrderByColumn{{Name: "created_at"}},
			},
			dialect:     DialectMySQL,
			expectedSQL: "SELECT * FROM events ORDER BY created_at ASC",
		},
		{
			name: "order by single column desc",
			query: Query{
				Table:   "events",
				OrderBy: []OrderByColumn{{Name: "score", Desc: true}},
			},
			dialect:     DialectMySQL,
			expectedSQL: "SELECT * FROM events ORDER BY score DESC",
		},
		{
			name: "order by multiple columns mixed directions",
			query: Query{
				Table: "events",
				OrderBy: []OrderByColumn{
					{Name: "priority", Desc: true},
					{Name: "name"},
				},
			},
			dialect:     DialectMySQL,
			expectedSQL: "SELECT * FROM events ORDER BY priority DESC, name ASC",
		},
		{
			name: "limit only",
			query: Query{
				Table: "events",
				Limit: int64Ptr(10),
			},
			dialect:     DialectMySQL,
			expectedSQL: "SELECT * FROM events LIMIT 10",
		},
		{
			name: "limit zero",
			query: Query{
				Table: "events",
				Limit: int64Ptr(0),
			},
			dialect:     DialectMySQL,
			expectedSQL: "SELECT * FROM events LIMIT 0",
		},
		{
			name: "columns, filters, order by, and limit combined",
			query: Query{
				Table:   "orders",
				Columns: []string{"id", "total", "status"},
				Filters: []ColumnFilter{
					{
						Name: "status",
						Conditions: []FilterCondition{
							{Operator: OperatorEquals, Value: "shipped"},
						},
					},
				},
				OrderBy: []OrderByColumn{
					{Name: "total", Desc: true},
					{Name: "id"},
				},
				Limit: int64Ptr(25),
			},
			dialect:     DialectMySQL,
			expectedSQL: "SELECT id, total, status FROM orders WHERE status = 'shipped' ORDER BY total DESC, id ASC LIMIT 25",
		},
		{
			name: "nil limit is ignored",
			query: Query{
				Table: "events",
				Limit: nil,
			},
			dialect:     DialectMySQL,
			expectedSQL: "SELECT * FROM events",
		},
		{
			name: "empty columns slice uses SELECT star",
			query: Query{
				Table:   "events",
				Columns: []string{},
			},
			dialect:     DialectMySQL,
			expectedSQL: "SELECT * FROM events",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := tc.query.ToSQL(tc.dialect)
			if tc.expectErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.expectedSQL, result)
		})
	}
}

func int64Ptr(v int64) *int64 { return &v }
