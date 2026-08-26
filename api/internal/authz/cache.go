package authz

import (
	"sync"
	"time"
)

const (
	grantTTL     = 15 * time.Second
	grantMaxSize = 8192
)

type cacheEntry struct {
	grant   Grant
	err     error
	expires time.Time
}

type grantCache struct {
	mu      sync.RWMutex
	entries map[string]cacheEntry
	now     func() time.Time
}

func newGrantCache() *grantCache {
	return &grantCache{entries: make(map[string]cacheEntry), now: time.Now}
}

func (c *grantCache) get(key string) (Grant, error, bool) {
	c.mu.RLock()
	e, ok := c.entries[key]
	c.mu.RUnlock()
	if !ok || c.now().After(e.expires) {
		return Grant{}, nil, false
	}
	return e.grant, e.err, true
}

func (c *grantCache) put(key string, g Grant, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.entries) >= grantMaxSize {
		c.evictLocked()
	}
	c.entries[key] = cacheEntry{grant: g, err: err, expires: c.now().Add(grantTTL)}
}

func (c *grantCache) evictLocked() {
	now := c.now()
	for k, e := range c.entries {
		if now.After(e.expires) {
			delete(c.entries, k)
		}
	}
	if len(c.entries) < grantMaxSize {
		return
	}
	drop := len(c.entries) / 2
	for k := range c.entries {
		if drop == 0 {
			return
		}
		delete(c.entries, k)
		drop--
	}
}

func (c *grantCache) invalidateUser(userID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for k, e := range c.entries {
		if e.grant.UserID == userID {
			delete(c.entries, k)
		}
	}
}

func (c *grantCache) invalidateOrg(orgID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for k, e := range c.entries {
		if e.grant.OrgID == orgID {
			delete(c.entries, k)
		}
	}
}
