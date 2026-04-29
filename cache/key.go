package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
)

// Scope controls how broadly a cached entry may be reused.
//
// ScopeUser is the safe default for endpoints whose results depend on the
// calling user's permissions (Azure subscriptions, RBAC-restricted indices,
// etc.). Plugins should only relax to ScopeDatasource after auditing that the
// endpoint's results are user-independent.
type Scope int

const (
	// ScopeUser additionally appends the calling user's identity. Required
	// when results are filtered by user permissions.
	// This is the zero value so partially-specified options remain safe.
	ScopeUser Scope = iota
	// ScopeDatasource keys entries by (namespace + dsUID + updated + proxyHash).
	// Two requests for the same datasource share an entry regardless of user.
	ScopeDatasource
)

func (s Scope) String() string {
	switch s {
	case ScopeUser:
		return "user"
	case ScopeDatasource:
		return "datasource"
	default:
		return "unknown"
	}
}

// Key is an opaque cache key. It is constructed only via the package-level
// builders so the tenant/datasource prefix can never be omitted.
type Key struct {
	// prefix is the tenant/datasource/user scope. It contains only stable IDs
	// and hashes so String remains safe for logs.
	prefix string
	// body is the SHA-256 (hex) of the cache namespace and extra parts.
	body string
}

// String returns a stable, log-safe representation of the key. User identity
// and request parts are hashed before they reach the key string.
func (k Key) String() string {
	return k.prefix + "|" + k.body
}

// raw returns the full key string used by the underlying Cache.
// Kept unexported so callers cannot bypass the builders.
func (k Key) raw() string {
	return k.prefix + "|" + k.body
}

// Errors returned by KeyFromPluginContext.
var (
	// ErrMissingDatasourceSettings is returned when PluginContext does not
	// carry DataSourceInstanceSettings (no datasource scope is available).
	ErrMissingDatasourceSettings = errors.New("schemads/cache: PluginContext.DataSourceInstanceSettings is required")
	// ErrMissingDatasourceUID is returned when datasource settings do not carry
	// a UID. Numeric IDs are deprecated and intentionally not used as fallback.
	ErrMissingDatasourceUID = errors.New("schemads/cache: DataSourceInstanceSettings.UID is required")
	// ErrMissingNamespace is returned when PluginContext.Namespace is empty.
	// PluginContext.OrgID is deprecated and is intentionally not used as a
	// fallback.
	ErrMissingNamespace = errors.New("schemads/cache: PluginContext.Namespace is required")
	// ErrMissingUser is returned when ScopeUser is requested but
	// PluginContext.User is nil. Callers should bypass the cache rather than
	// risk leaking results across users.
	ErrMissingUser = errors.New("schemads/cache: PluginContext.User is required for ScopeUser")
	// ErrMissingUserIdentity is returned when ScopeUser is requested but the
	// user carries no stable identity fields. Callers should bypass the cache.
	ErrMissingUserIdentity = errors.New("schemads/cache: PluginContext.User identity is required for ScopeUser")
)

// KeyFromPluginContext builds a tenant-safe cache key from a backend
// PluginContext.
//
// The key prefix mirrors the plugin SDK's instancemgmt convention so reconfig,
// credential rotation and PDC changes auto-invalidate cached entries:
//
//	namespace#dsUID#updatedUnix#proxyHash[#userHash]
//
// The cacheNamespace and parts arguments are SHA-256 hashed into the key body,
// so user-controlled values (table parameters, column names) are not exposed in
// the key string.
//
// Returns an error when:
//   - DataSourceInstanceSettings is nil (no datasource scope available),
//   - Namespace is empty for any scope that requires it,
//   - ScopeUser is requested but User is nil.
//
// PluginContext.OrgID is deprecated in the SDK in favour of Namespace and is
// not consulted.
func KeyFromPluginContext(pc backend.PluginContext, scope Scope, cacheNamespace string, parts ...string) (Key, error) {
	if pc.DataSourceInstanceSettings == nil {
		return Key{}, ErrMissingDatasourceSettings
	}
	if pc.DataSourceInstanceSettings.UID == "" {
		return Key{}, ErrMissingDatasourceUID
	}
	if pc.Namespace == "" {
		return Key{}, ErrMissingNamespace
	}
	if scope == ScopeUser && pc.User == nil {
		return Key{}, ErrMissingUser
	}
	userID := ""
	if scope == ScopeUser {
		var ok bool
		userID, ok = userIdentity(pc.User)
		if !ok {
			return Key{}, ErrMissingUserIdentity
		}
	}

	dsUID := pc.DataSourceInstanceSettings.UID
	updatedUnix := pc.DataSourceInstanceSettings.Updated.Unix()
	proxyHash := pc.GrafanaConfig.ProxyHash()

	var sb strings.Builder
	sb.WriteString("ns:")
	sb.WriteString(pc.Namespace)
	sb.WriteString("#dsuid:")
	sb.WriteString(dsUID)
	sb.WriteString("#u:")
	sb.WriteString(strconv.FormatInt(updatedUnix, 10))
	sb.WriteString("#ph:")
	sb.WriteString(proxyHash)
	if scope == ScopeUser {
		sb.WriteString("#usr:")
		sb.WriteString(hashParts("user", []string{userID}))
	}
	prefix := sb.String()

	return Key{prefix: prefix, body: hashParts(cacheNamespace, parts)}, nil
}

// userIdentity returns a stable identifier for the calling user. Login is
// preferred (it's stable across name changes) and falls back to UID/Email
// when Login is absent, matching the SDK's user identity conventions.
func userIdentity(u *backend.User) (string, bool) {
	if u == nil {
		return "", false
	}
	if u.Login != "" {
		return u.Login, true
	}
	if u.Email != "" {
		return u.Email, true
	}
	if u.Name != "" {
		return u.Name, true
	}
	return "", false
}

// hashParts returns the hex SHA-256 of the cache namespace and extra parts.
func hashParts(cacheNamespace string, parts []string) string {
	h := sha256.New()
	_, _ = h.Write([]byte(cacheNamespace))
	for _, p := range parts {
		_, _ = h.Write([]byte(p))
	}
	return hex.EncodeToString(h.Sum(nil))
}
