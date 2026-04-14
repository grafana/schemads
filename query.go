package schemas

import (
	"fmt"

	"github.com/huandu/go-sqlbuilder"
)

// SQLDialect identifies the SQL dialect used for query interpolation.
type SQLDialect int

const (
	DialectMySQL SQLDialect = iota
	DialectPostgreSQL
	DialectSQLite
	DialectSQLServer
	DialectClickHouse
	DialectSnowflake
	DialectOracle
)

// toFlavor maps a SQLDialect to the corresponding go-sqlbuilder Flavor.
func (d SQLDialect) toFlavor() sqlbuilder.Flavor {
	switch d {
	case DialectPostgreSQL:
		return sqlbuilder.PostgreSQL
	case DialectSQLite:
		return sqlbuilder.SQLite
	case DialectSQLServer:
		return sqlbuilder.SQLServer
	case DialectClickHouse:
		return sqlbuilder.ClickHouse
	case DialectSnowflake:
		return sqlbuilder.MySQL
	case DialectOracle:
		return sqlbuilder.Oracle
	default:
		return sqlbuilder.MySQL
	}
}

// ToSQL builds a fully interpolated SQL SELECT query from a
// Query using the specified SQL dialect. All filter conditions
// are ANDed together. Returns an error if a filter uses an unsupported
// operator.
//
// When Columns is non-empty the SELECT list is set to those columns;
// otherwise SELECT * is used. OrderBy and Limit are appended when set.
func (q Query) ToSQL(dialect SQLDialect) (string, error) {
	flavor := dialect.toFlavor()
	sb := flavor.NewSelectBuilder()

	if len(q.Columns) > 0 {
		sb.Select(q.Columns...).From(q.Table)
	} else {
		sb.Select("*").From(q.Table)
	}

	for _, filter := range q.Filters {
		for _, cond := range filter.Conditions {
			expr, err := conditionExpr(sb, filter.Name, cond)
			if err != nil {
				return "", err
			}
			sb.Where(expr)
		}
	}

	for _, ob := range q.OrderBy {
		if ob.Desc {
			sb.OrderByDesc(ob.Name)
		} else {
			sb.OrderByAsc(ob.Name)
		}
	}

	if q.Limit != nil && *q.Limit >= 0 {
		sb.Limit(int(*q.Limit))
	}

	sql, args := sb.Build()
	interpolated, err := flavor.Interpolate(sql, args)
	if err != nil {
		return "", fmt.Errorf("failed to interpolate SQL: %w", err)
	}
	return interpolated, nil
}

func conditionExpr(sb *sqlbuilder.SelectBuilder, column string, cond FilterCondition) (string, error) {
	switch cond.Operator {
	case OperatorEquals:
		return sb.Equal(column, cond.Value), nil
	case OperatorNotEquals:
		return sb.NotEqual(column, cond.Value), nil
	case OperatorGreaterThan:
		return sb.GreaterThan(column, cond.Value), nil
	case OperatorGreaterThanOrEqual:
		return sb.GreaterEqualThan(column, cond.Value), nil
	case OperatorLessThan:
		return sb.LessThan(column, cond.Value), nil
	case OperatorLessThanOrEqual:
		return sb.LessEqualThan(column, cond.Value), nil
	case OperatorLike:
		return sb.Like(column, cond.Value), nil
	case OperatorIn:
		values := make([]any, len(cond.Values))
		for i, v := range cond.Values {
			values[i] = v
		}
		return sb.In(column, values...), nil
	default:
		return "", fmt.Errorf("unsupported operator: %s", cond.Operator)
	}
}
