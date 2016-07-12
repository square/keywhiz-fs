// Copyright 2015 Square Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"time"

	"github.com/square/keywhiz-fs/log"
)

// SecretBackend represents an interface for storing secrets.
type SecretBackend interface {
	Secret(string) (secret *Secret, err error)
	SecretList() (secretList []Secret, ok bool)
}

// Timeouts contains configuration for timeouts:
// timeout_backend_deadline: optimistic timeout to wait for cache
// timeout_max_wait: timeout for client to get data from server
type Timeouts struct {
	// FUSE may make many lookups in quick succession. If cached data is recent within the threshold,
	// a backend request is not attempted.
	Fresh time.Duration
	// BackendDeadline is distinct from the backend timeout. It is an optimistic timeout to wait
	// until resorting to cached data.
	BackendDeadline time.Duration
	MaxWait         time.Duration
	// Controls how long to keep a deleted entry before purging it.
	DeletionDelay time.Duration
}

// Cache contains necessary state to return secrets, using previously cached content or retrieving
// from a server if necessary.
type Cache struct {
	*log.Logger
	secretMap *SecretMap
	backend   SecretBackend
	timeouts  Timeouts
	now       func() time.Time
}

type secretResult struct {
	secret *Secret
	err    error
}

// NewCache initializes a Cache.
func NewCache(backend SecretBackend, timeouts Timeouts, logConfig log.Config, now func() time.Time) *Cache {
	logger := log.New("kwfs_cache", logConfig)
	return &Cache{logger, NewSecretMap(timeouts, now), backend, timeouts, now}
}

// Warmup reads the secret list from the backend to prime the cache.
// Should only be called after creating a new cache on startup.
func (c *Cache) Warmup() {
	// Attempt to warmup cache
	newMap := NewSecretMap(c.timeouts, c.now)
	secrets, ok := c.backend.SecretList()
	if ok {
		for _, backendSecret := range secrets {
			newMap.Put(backendSecret.Name, backendSecret)
		}
		c.secretMap.Overwrite(newMap)
	} else {
		c.Warnf("Failed to warmup cache on startup")
	}
}

// Clear empties the internal cache. This function does not honor the
// delayed deletion contract. The function is called when the user deletes
// .clear_cache.
func (c *Cache) Clear() {
	c.Infof("Cache cleared")
	c.secretMap = NewSecretMap(c.timeouts, c.now)
}

// Secret retrieves a Secret by name from cache or a server.
//
// Cache logic:
//  1. Check cache for secret.
//			* If entry is fresh, return cache entry.
//			* If entry is not fresh, call backend.
//  2. Ask backend for secret (with timeout).
//			* If backend returns success: update cache, return.
//			* If backend returns deleted: set delayed deletion, return data from cache.
//  3. If timeout backend deadline hit return whatever we have.
func (c *Cache) Secret(name string) (*Secret, bool) {
	// Perform cache lookup first
	cacheResult := c.cacheSecret(name)

	var secret *Secret
	var success bool

	if cacheResult != nil {
		secret = &cacheResult.Secret
		success = true
	}

	// If cache succeeded, and entry is very recent, return cache result
	if success && (time.Since(cacheResult.Time) < c.timeouts.Fresh) {
		return &cacheResult.Secret, success
	}

	backendDeadline := time.After(c.timeouts.BackendDeadline)
	backendDone := c.backendSecret(name)

	select {
	case s := <-backendDone:
		if s.err == nil {
			secret = s.secret
			success = true
		}
		if _, ok := s.err.(SecretDeleted); ok {
			c.secretMap.Delete(name)
		}
	case <-backendDeadline:
		c.Errorf("Backend timeout on secret fetch for '%s'", name)
	}

	return secret, success
}

// SecretList returns a listing of Secrets from cache or a server.
//
// Cache logic:
//  * If backend returns fast: update cache, return.
//  * If timeout backend deadline: return cache entries, background update cache.
//  * If timeout max wait: return cache version.
func (c *Cache) SecretList() []Secret {
	// Perform cache lookup first
	var secretList []Secret

	cacheResult := c.cacheSecretList()
	if cacheResult != nil {
		secretList = cacheResult
	}

	backendDeadline := time.After(c.timeouts.BackendDeadline)
	backendDone := c.backendSecretList()

	for {
		select {
		case backendResult := <-backendDone:
			return backendResult
		case <-backendDeadline:
			c.Errorf("Backend timeout for secret list")
			return secretList
		}
	}
}

// Add inserts a secret into the cache. If a secret is already in the cache with a matching
// identifier, it will be overridden  This method is most useful for testing since lookups
// may add data to the cache.
func (c *Cache) Add(s Secret) {
	c.secretMap.Put(s.Name, s)
}

// Len returns the number of values stored in the cache. This method is most useful for testing.
func (c *Cache) Len() int {
	return c.secretMap.Len()
}

// cacheSecret retrieves a secret from the cache.
func (c *Cache) cacheSecret(name string) *SecretTime {
	secret, ok := c.secretMap.Get(name)
	if ok && len(secret.Secret.Content) > 0 {
		c.Debugf("Cache hit: %v", name)
		return &secret
	}
	c.Debugf("Cache miss: %v", name)
	return nil
}

// cacheSecretList retrieves a secret listing from the cache.
func (c *Cache) cacheSecretList() []Secret {
	values := c.secretMap.Values()
	secrets := make([]Secret, len(values))
	for i, v := range values {
		secrets[i] = v.Secret
	}
	return secrets
}

// backendSecret retrieves a secret from the backend and updates the cache.
//
// Retrieval is concurrent, so a channel is returned to communicate a successful value.
// The channel will not be fulfilled on error.
func (c *Cache) backendSecret(name string) chan secretResult {
	secretc := make(chan secretResult)
	go func() {
		defer close(secretc)
		secret, err := c.backend.Secret(name)
		secretc <- secretResult{secret, err}
		if err == nil {
			c.secretMap.Put(name, *secret)
		}
	}()
	return secretc
}

// backendSecretList retrieves a secret listing from the backend and updates the cache.
//
// Retrieval is concurrent, so a channel is returned to communicate successful values. The channel
// will not be fulfilled on error.
func (c *Cache) backendSecretList() chan []Secret {
	secretsc := make(chan []Secret, 1)
	go func() {
		secrets, ok := c.backend.SecretList()
		if !ok {
			// Don't close the channel so that we use the result from the cache.
			return
		}

		newMap := NewSecretMap(c.timeouts, c.now)
		for _, backendSecret := range secrets {
			if len(backendSecret.Content) == 0 {
				// The backend didn't return any content. The cache might contain a secret with content, in
				// which case we want to keep the cache's value (and not schedule it for delayed deletion).
				if s, ok := c.secretMap.Get(backendSecret.Name); ok && len(s.Secret.Content) > 0 {
					newMap.Put(backendSecret.Name, s.Secret)
				} else {
					// We don't have content for this secret. This happens when the cache has never seen a given secret
					// (at startup or when a new secret is added).
					// can happen.
					newMap.Put(backendSecret.Name, backendSecret)
				}
			} else {
				// TODO: explain why this case can happen. It doesn't seem like it can,
				// listing secrets always returns just the names.
				// Cache the latest info.
				newMap.Put(backendSecret.Name, backendSecret)
			}
		}
		c.secretMap.Replace(newMap)

		// TODO: copy-pasta from cacheSecretList(), should refactor.
		values := c.secretMap.Values()
		secrets = make([]Secret, len(values))
		for i, v := range values {
			secrets[i] = v.Secret
		}
		secretsc <- secrets
		close(secretsc)
	}()
	return secretsc
}
