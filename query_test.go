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
