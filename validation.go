package schemas

import (
	"fmt"
)

// ValidateSchema validates the structural integrity of a Schema's sub-table
// definitions. It is called automatically when a fullSchema response is
// returned and may also be called by consumers who construct schemas manually.
//
// For each table that defines sub-tables, ValidateSchema checks:
//   - Sub-table names are unique within the parent table.
//   - Every DependsOn entry references an existing sibling sub-table.
//   - The dependency graph is acyclic.
//   - At least one sub-table is marked Root, and root sub-tables have no
//     dependencies.
//   - Required sub-tables only depend on other required sub-tables (the
//     "required chain" invariant).
//
// At the schema level it also verifies that SubTableValues only references
// tables and root sub-tables that actually exist in the schema. Non-root
// sub-table values depend on ancestor selections and cannot be
// pre-populated; they must be fetched via SubTableValuesRequest.
//
// Returns nil when schema is nil or all invariants hold.
func ValidateSchema(schema *Schema) error {
	if schema == nil {
		return nil
	}

	tablesByName := make(map[string]*Table, len(schema.Tables))
	for i := range schema.Tables {
		if _, duplicate := tablesByName[schema.Tables[i].Name]; duplicate {
			return fmt.Errorf("duplicate table name %q", schema.Tables[i].Name)
		}
		tablesByName[schema.Tables[i].Name] = &schema.Tables[i]
	}

	for i := range schema.Tables {
		if err := validateTableSubTables(&schema.Tables[i]); err != nil {
			return fmt.Errorf("table %q: %w", schema.Tables[i].Name, err)
		}
	}

	if err := validateSubTableValues(schema.SubTableValues, tablesByName); err != nil {
		return err
	}

	return nil
}

// validateTableSubTables validates the sub-table definitions within a single
// table. It runs five checks in order:
//
//  1. Uniqueness  – no two sub-tables share the same name.
//  2. References  – every DependsOn entry names a sibling sub-table.
//  3. Acyclicity  – the dependency graph contains no cycles.
//  4. Root rules  – at least one sub-table is Root and root sub-tables
//     must not declare dependencies.
//  5. Required chain – a required sub-table cannot depend on an optional one,
//     because the consumer would be unable to resolve the required sub-table
//     without first resolving its optional ancestor.
//
// Skips validation (returns nil) when the table has no sub-tables.
func validateTableSubTables(table *Table) error {
	if len(table.SubTables) == 0 {
		return nil
	}

	knownNames := make(map[string]struct{}, len(table.SubTables))
	hasRoot := false
	for _, subTable := range table.SubTables {
		if _, duplicate := knownNames[subTable.Name]; duplicate {
			return fmt.Errorf("duplicate sub-table name %q", subTable.Name)
		}
		knownNames[subTable.Name] = struct{}{}
		if subTable.Root {
			hasRoot = true
			if len(subTable.DependsOn) > 0 {
				return fmt.Errorf("sub-table %q is marked as root but has dependencies", subTable.Name)
			}
		}
	}

	if !hasRoot {
		return fmt.Errorf("sub-tables defined but none are marked as root")
	}

	for _, subTable := range table.SubTables {
		for _, dependency := range subTable.DependsOn {
			if _, exists := knownNames[dependency]; !exists {
				return fmt.Errorf("sub-table %q depends on non-existent sub-table %q", subTable.Name, dependency)
			}
		}
	}

	if err := detectCycle(table.SubTables); err != nil {
		return err
	}

	if err := validateRequiredChain(table.SubTables); err != nil {
		return err
	}

	return nil
}

// detectCycle performs a depth-first search over the sub-table dependency
// graph to detect circular dependencies. It uses three-state colouring:
//
//   - unvisited: the node has not been reached yet.
//   - visiting:  the node is on the current DFS path (an ancestor in the
//     recursion stack). Reaching a "visiting" node means we have
//     found a back-edge, which implies a cycle.
//   - visited:   the node and all its descendants have been fully explored
//     without finding a cycle through this node.
//
// Every sub-table is used as a DFS entry point so that disconnected
// components are also checked.
func detectCycle(subTables []SubTable) error {
	const (
		unvisited = 0
		visiting  = 1
		visited   = 2
	)

	subTableByName := make(map[string]*SubTable, len(subTables))
	for i := range subTables {
		subTableByName[subTables[i].Name] = &subTables[i]
	}

	visitState := make(map[string]int, len(subTables))

	var visit func(name string) error
	visit = func(name string) error {
		switch visitState[name] {
		case visiting:
			return fmt.Errorf("circular dependency detected involving sub-table %q", name)
		case visited:
			return nil
		}

		visitState[name] = visiting
		for _, dependency := range subTableByName[name].DependsOn {
			if err := visit(dependency); err != nil {
				return err
			}
		}
		visitState[name] = visited
		return nil
	}

	for _, subTable := range subTables {
		if err := visit(subTable.Name); err != nil {
			return err
		}
	}
	return nil
}

// validateRequiredChain enforces that if a sub-table is marked Required,
// every sub-table it depends on must also be Required. Without this rule a
// consumer could encounter a required sub-table whose ancestor values are
// optional and therefore potentially unresolved, making it impossible to
// satisfy the required sub-table.
//
// Example violation: sub-table "repository" (required) depends on
// "organization" (optional). The consumer must select an organization
// before it can resolve repository, but organization is not required,
// so the dependency chain is broken.
func validateRequiredChain(subTables []SubTable) error {
	isRequired := make(map[string]bool, len(subTables))
	for _, subTable := range subTables {
		isRequired[subTable.Name] = subTable.Required
	}

	for _, subTable := range subTables {
		if !subTable.Required {
			continue
		}
		for _, dependency := range subTable.DependsOn {
			if !isRequired[dependency] {
				return fmt.Errorf(
					"required sub-table %q depends on non-required sub-table %q; "+
						"all dependencies of a required sub-table must also be required",
					subTable.Name, dependency,
				)
			}
		}
	}
	return nil
}

// validateSubTableValues checks that Schema.SubTableValues only references
// tables and sub-tables that exist in the schema, and that every referenced
// sub-table is a root. Non-root sub-tables have dependencies whose selected
// values determine the result set, so pre-populating them in a flat list is
// meaningless — consumers must use SubTableValuesRequest with
// DependencyValues for correlated fetching instead.
func validateSubTableValues(subTableValues map[string]map[string][]string, tablesByName map[string]*Table) error {
	for tableName, valuesBySubTable := range subTableValues {
		table, tableExists := tablesByName[tableName]
		if !tableExists {
			return fmt.Errorf("subTableValues references non-existent table %q", tableName)
		}

		subTableRoots := make(map[string]bool, len(table.SubTables))
		for i := range table.SubTables {
			subTableRoots[table.SubTables[i].Name] = table.SubTables[i].Root
		}

		for subTableName := range valuesBySubTable {
			root, exists := subTableRoots[subTableName]
			if !exists {
				return fmt.Errorf(
					"subTableValues references non-existent sub-table %q on table %q",
					subTableName, tableName,
				)
			}
			if !root {
				return fmt.Errorf(
					"subTableValues contains non-root sub-table %q on table %q; "+
						"only root sub-tables may have pre-populated values",
					subTableName, tableName,
				)
			}
		}
	}
	return nil
}
