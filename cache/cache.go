// Package cache provides the in-memory caching primitives used by the schemads
// response wrapper and by plugin handlers for in-handler sub-fetches.
//
// All caching is backed by [MemoryCache], which uses patrickmn/go-cache for
// TTL expiry and cleanup. For typed in-process caching that avoids JSON
// marshaling on every hit, use [Typed].
package cache
