package cache

import (
	"testing"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/stretchr/testify/require"
)

func mockPluginContext() backend.PluginContext {
	return backend.PluginContext{
		Namespace: "stack-1",
		DataSourceInstanceSettings: &backend.DataSourceInstanceSettings{
			UID:     "ds-42",
			Updated: time.Unix(1_700_000_000, 0),
		},
		User: &backend.User{Login: "alice"},
	}
}

func TestKeyFromPluginContext_RequiresDatasourceSettings(t *testing.T) {
	pc := mockPluginContext()
	pc.DataSourceInstanceSettings = nil
	_, err := KeyFromPluginContext(pc, ScopeDatasource, "ns")
	require.ErrorIs(t, err, ErrMissingDatasourceSettings)
}

func TestKeyFromPluginContext_RequiresDatasourceUID(t *testing.T) {
	pc := mockPluginContext()
	pc.DataSourceInstanceSettings.UID = ""
	_, err := KeyFromPluginContext(pc, ScopeDatasource, "ns")
	require.ErrorIs(t, err, ErrMissingDatasourceUID)
}

func TestKeyFromPluginContext_RequiresNamespace(t *testing.T) {
	for _, scope := range []Scope{ScopeDatasource, ScopeUser} {
		t.Run(scope.String(), func(t *testing.T) {
			pc := mockPluginContext()
			pc.Namespace = ""
			_, err := KeyFromPluginContext(pc, scope, "ns")
			require.ErrorIs(t, err, ErrMissingNamespace)
		})
	}
}

func TestKeyFromPluginContext_ScopeUserRequiresUser(t *testing.T) {
	pc := mockPluginContext()
	pc.User = nil
	_, err := KeyFromPluginContext(pc, ScopeUser, "ns")
	require.ErrorIs(t, err, ErrMissingUser)
}

func TestKeyFromPluginContext_ScopeUserRequiresIdentity(t *testing.T) {
	pc := mockPluginContext()
	pc.User = &backend.User{}
	_, err := KeyFromPluginContext(pc, ScopeUser, "ns")
	require.ErrorIs(t, err, ErrMissingUserIdentity)
}

func TestKeyFromPluginContext_TenantIsolation(t *testing.T) {
	a := mockPluginContext()
	b := mockPluginContext()
	b.Namespace = "stack-2"
	ka, err := KeyFromPluginContext(a, ScopeDatasource, "tables")
	require.NoError(t, err)
	kb, err := KeyFromPluginContext(b, ScopeDatasource, "tables")
	require.NoError(t, err)
	require.NotEqual(t, ka.String(), kb.String())
}

func TestKeyFromPluginContext_DatasourceIsolation(t *testing.T) {
	a := mockPluginContext()
	b := mockPluginContext()
	dsB := *b.DataSourceInstanceSettings
	dsB.UID = "ds-99"
	b.DataSourceInstanceSettings = &dsB

	ka, err := KeyFromPluginContext(a, ScopeDatasource, "tables")
	require.NoError(t, err)
	kb, err := KeyFromPluginContext(b, ScopeDatasource, "tables")
	require.NoError(t, err)
	require.NotEqual(t, ka.String(), kb.String())
}

func TestKeyFromPluginContext_UserScopeSeparatesUsers(t *testing.T) {
	a := mockPluginContext()
	b := mockPluginContext()
	b.User = &backend.User{Login: "bob"}
	ka, err := KeyFromPluginContext(a, ScopeUser, "tables")
	require.NoError(t, err)
	kb, err := KeyFromPluginContext(b, ScopeUser, "tables")
	require.NoError(t, err)
	require.NotEqual(t, ka.String(), kb.String())
}

func TestKeyFromPluginContext_DatasourceScopeSharesAcrossUsers(t *testing.T) {
	a := mockPluginContext()
	b := mockPluginContext()
	b.User = &backend.User{Login: "bob"}
	ka, err := KeyFromPluginContext(a, ScopeDatasource, "tables")
	require.NoError(t, err)
	kb, err := KeyFromPluginContext(b, ScopeDatasource, "tables")
	require.NoError(t, err)
	require.Equal(t, ka.String(), kb.String())
}

func TestKeyFromPluginContext_UpdatedInvalidates(t *testing.T) {
	a := mockPluginContext()
	b := mockPluginContext()
	dsB := *b.DataSourceInstanceSettings
	dsB.Updated = dsB.Updated.Add(1 * time.Second)
	b.DataSourceInstanceSettings = &dsB
	ka, err := KeyFromPluginContext(a, ScopeDatasource, "tables")
	require.NoError(t, err)
	kb, err := KeyFromPluginContext(b, ScopeDatasource, "tables")
	require.NoError(t, err)
	require.NotEqual(t, ka.String(), kb.String())
}

func TestKey_StringDoesNotExposeUserIdentity(t *testing.T) {
	pc := mockPluginContext()
	pc.User = &backend.User{Login: "alice", Email: "alice@example.com", Name: "Alice"}
	k, err := KeyFromPluginContext(pc, ScopeUser, "tables")
	require.NoError(t, err)

	require.NotContains(t, k.String(), "alice")
	require.NotContains(t, k.String(), "alice@example.com")
	require.NotContains(t, k.String(), "Alice")
}

// TestKey_InjectionResistance verifies a user-controlled value containing the
// key separator characters cannot collide with a different (cacheNamespace,
// parts) tuple of the same length.
func TestKey_InjectionResistance(t *testing.T) {
	pc := mockPluginContext()
	// "foo" + "#" + "bar" vs "foo#" + "" + "bar" — concatenation would
	// produce identical bytes; length-prefixing must not.
	a, err := KeyFromPluginContext(pc, ScopeDatasource, "ns", "foo", "#bar")
	require.NoError(t, err)
	b, err := KeyFromPluginContext(pc, ScopeDatasource, "ns", "foo#", "bar")
	require.NoError(t, err)
	require.NotEqual(t, a.String(), b.String())
}

func TestKey_DifferentEndpointsAreDistinct(t *testing.T) {
	pc := mockPluginContext()
	a, err := KeyFromPluginContext(pc, ScopeDatasource, "tables")
	require.NoError(t, err)
	b, err := KeyFromPluginContext(pc, ScopeDatasource, "columns")
	require.NoError(t, err)
	require.NotEqual(t, a.String(), b.String())
}
