package schemas

import (
	"fmt"
)

// ValidateSchema validates the structural integrity of a Schema's table parameter
// definitions. It is called automatically when a fullSchema response is
// returned and may also be called by consumers who construct schemas manually.
//
// For each table that defines table parameters, ValidateSchema checks:
//   - Table parameter name is unique within the parent table.
//   - Every DependsOn entry references an existing sibling table parameter.
//   - The dependency graph is acyclic.
//   - At least one table parameter is marked Root, and root table parameters have no
//     dependencies.
//   - Required table parameters only depend on other required table parameters (the
//     "required chain" invariant).
//
// At the schema level it also verifies that TableParameterValues only references
// tables and root table parameters that actually exist in the schema. Non-root
// table parameter values depend on ancestor selections and cannot be
// pre-populated; they must be fetched via TableParameterValuesRequest.
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
		if err := validateTableParameters(&schema.Tables[i]); err != nil {
			return fmt.Errorf("table %q: %w", schema.Tables[i].Name, err)
		}
	}

	if err := validateTableParameterValues(schema.TableParameterValues, tablesByName); err != nil {
		return err
	}

	return nil
}

// validateTableParameters validates the table parameter definitions within a single
// table. It runs five checks in order:
//
//  1. Uniqueness  – no two table parameters share the same name.
//  2. References  – every DependsOn entry names a sibling table parameter.
//  3. Acyclicity  – the dependency graph contains no cycles.
//  4. Root rules  – at least one table parameter is Root and root table parameters
//     must not declare dependencies.
//  5. Required chain – a required table parameter cannot depend on an optional one,
//     because the consumer would be unable to resolve the required table parameter
//     without first resolving its optional ancestor.
//
// Skips validation (returns nil) when the table has no table parameters.
func validateTableParameters(table *Table) error {
	if len(table.TableParameters) == 0 {
		return nil
	}

	knownNames := make(map[string]struct{}, len(table.TableParameters))
	hasRoot := false
	for _, tableParameter := range table.TableParameters {
		if _, duplicate := knownNames[tableParameter.Name]; duplicate {
			return fmt.Errorf("duplicate table parameter name %q", tableParameter.Name)
		}
		knownNames[tableParameter.Name] = struct{}{}
		if tableParameter.Root {
			hasRoot = true
			if len(tableParameter.DependsOn) > 0 {
				return fmt.Errorf("table parameter %q is marked as root but has dependencies", tableParameter.Name)
			}
		}
	}

	if !hasRoot {
		return fmt.Errorf("table parameters defined but none are marked as root")
	}

	for _, tableParameter := range table.TableParameters {
		for _, dependency := range tableParameter.DependsOn {
			if _, exists := knownNames[dependency]; !exists {
				return fmt.Errorf("table parameter %q depends on non-existent table parameter %q", tableParameter.Name, dependency)
			}
		}
	}

	if err := detectCycle(table.TableParameters); err != nil {
		return err
	}

	if err := validateRequiredChain(table.TableParameters); err != nil {
		return err
	}

	return nil
}

// detectCycle performs a depth-first search over the table parameter dependency
// graph to detect circular dependencies. It uses three-state colouring:
//
//   - unvisited: the node has not been reached yet.
//   - visiting:  the node is on the current DFS path (an ancestor in the
//     recursion stack). Reaching a "visiting" node means we have
//     found a back-edge, which implies a cycle.
//   - visited:   the node and all its descendants have been fully explored
//     without finding a cycle through this node.
//
// Every table parameter is used as a DFS entry point so that disconnected
// components are also checked.
func detectCycle(tableParameters []TableParameter) error {
	const (
		unvisited = 0
		visiting  = 1
		visited   = 2
	)

	tableParameterByName := make(map[string]*TableParameter, len(tableParameters))
	for i := range tableParameters {
		tableParameterByName[tableParameters[i].Name] = &tableParameters[i]
	}

	visitState := make(map[string]int, len(tableParameters))

	var visit func(name string) error
	visit = func(name string) error {
		switch visitState[name] {
		case visiting:
			return fmt.Errorf("circular dependency detected involving table parameter %q", name)
		case visited:
			return nil
		}

		visitState[name] = visiting
		for _, dependency := range tableParameterByName[name].DependsOn {
			if err := visit(dependency); err != nil {
				return err
			}
		}
		visitState[name] = visited
		return nil
	}

	for _, tableParameter := range tableParameters {
		if err := visit(tableParameter.Name); err != nil {
			return err
		}
	}
	return nil
}

// validateRequiredChain enforces that if a table parameter is marked Required,
// every table parameter it depends on must also be Required. Without this rule a
// consumer could encounter a required table parameter whose ancestor values are
// optional and therefore potentially unresolved, making it impossible to
// satisfy the required table parameter.
//
// Example violation: table parameter "repository" (required) depends on
// "organization" (optional). The consumer must select an organization
// before it can resolve repository, but organization is not required,
// so the dependency chain is broken.
func validateRequiredChain(tableParameters []TableParameter) error {
	isRequired := make(map[string]bool, len(tableParameters))
	for _, tableParameter := range tableParameters {
		isRequired[tableParameter.Name] = tableParameter.Required
	}

	for _, tableParameter := range tableParameters {
		if !tableParameter.Required {
			continue
		}
		for _, dependency := range tableParameter.DependsOn {
			if !isRequired[dependency] {
				return fmt.Errorf(
					"required table parameter %q depends on non-required table parameter %q; "+
						"all dependencies of a required table parameter must also be required",
					tableParameter.Name, dependency,
				)
			}
		}
	}
	return nil
}

// validateTableParameterValues checks that Schema.TableParameterValues only references
// tables and table parameters that exist in the schema, and that every referenced
// table parameter is a root. Non-root table parameters have dependencies whose selected
// values determine the result set, so pre-populating them in a flat list is
// meaningless — consumers must use TableParameterValuesRequest with
// DependencyValues for correlated fetching instead.
func validateTableParameterValues(tableParameterValues map[string]map[string][]string, tablesByName map[string]*Table) error {
	for tableName, valuesByTableParameter := range tableParameterValues {
		table, tableExists := tablesByName[tableName]
		if !tableExists {
			return fmt.Errorf("tableParameterValues references non-existent table %q", tableName)
		}

		tableParameterRoots := make(map[string]bool, len(table.TableParameters))
		for i := range table.TableParameters {
			tableParameterRoots[table.TableParameters[i].Name] = table.TableParameters[i].Root
		}

		for tableParameterName := range valuesByTableParameter {
			root, exists := tableParameterRoots[tableParameterName]
			if !exists {
				return fmt.Errorf(
					"tableParameterValues references non-existent table parameter %q on table %q",
					tableParameterName, tableName,
				)
			}
			if !root {
				return fmt.Errorf(
					"tableParameterValues contains non-root table parameter %q on table %q; "+
						"only root table parameters may have pre-populated values",
					tableParameterName, tableName,
				)
			}
		}
	}
	return nil
}
