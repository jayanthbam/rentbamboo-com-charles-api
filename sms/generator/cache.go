package generator

import (
	"fmt"
	"sync"
	"time"

	"go.mongodb.org/mongo-driver/bson"
)

// ─────────────────────────────────────────────────────────────────────────────
// Team Config Cache — command center + team info + training data
// TTL: 5 min. These change maybe once a week.
// ─────────────────────────────────────────────────────────────────────────────

type cachedTeamConfig struct {
	cmdCenter bson.M
	teamInfo  bson.M
	fetchedAt time.Time
}

type TeamConfigCache struct {
	mu    sync.RWMutex
	entries map[string]*cachedTeamConfig
	ttl   time.Duration
}

func NewTeamConfigCache() *TeamConfigCache {
	return &TeamConfigCache{
		entries: make(map[string]*cachedTeamConfig),
		ttl:     5 * time.Minute,
	}
}

func (c *TeamConfigCache) Get(teamID string) (cmdCenter bson.M, teamInfo bson.M, ok bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	entry, exists := c.entries[teamID]
	if !exists {
		return nil, nil, false
	}
	if time.Since(entry.fetchedAt) > c.ttl {
		return nil, nil, false
	}
	return entry.cmdCenter, entry.teamInfo, true
}

func (c *TeamConfigCache) Set(teamID string, cmdCenter bson.M, teamInfo bson.M) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[teamID] = &cachedTeamConfig{
		cmdCenter: cmdCenter,
		teamInfo:  teamInfo,
		fetchedAt: time.Now(),
	}
}

// Invalidate removes a cached team config entry, forcing the next fetch to
// read from MongoDB. Used when command center or property data changes.
func (c *TeamConfigCache) Invalidate(teamID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, teamID)
}

// ─────────────────────────────────────────────────────────────────────────────
// Availability Cache — formatted availability context string per property+team
// TTL: 2 min. Slots change when bookings happen, but not every 30 seconds.
// ─────────────────────────────────────────────────────────────────────────────

type cachedAvailability struct {
	context   string // pre-formatted availability text
	fetchedAt time.Time
}

type AvailabilityCache struct {
	mu    sync.RWMutex
	entries map[string]*cachedAvailability
	ttl   time.Duration
}

func NewAvailabilityCache() *AvailabilityCache {
	return &AvailabilityCache{
		entries: make(map[string]*cachedAvailability),
		ttl:     2 * time.Minute,
	}
}

func availabilityCacheKey(propertyID, teamID string) string {
	return fmt.Sprintf("%s:%s", propertyID, teamID)
}

func (c *AvailabilityCache) Get(propertyID, teamID string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	key := availabilityCacheKey(propertyID, teamID)
	entry, exists := c.entries[key]
	if !exists {
		return "", false
	}
	if time.Since(entry.fetchedAt) > c.ttl {
		return "", false
	}
	return entry.context, true
}

func (c *AvailabilityCache) Set(propertyID, teamID string, context string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	key := availabilityCacheKey(propertyID, teamID)
	c.entries[key] = &cachedAvailability{
		context:   context,
		fetchedAt: time.Now(),
	}
}

// Invalidate removes a cached availability entry for a property+team.
func (c *AvailabilityCache) Invalidate(propertyID, teamID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	key := availabilityCacheKey(propertyID, teamID)
	delete(c.entries, key)
}
