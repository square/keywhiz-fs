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

package keywhizfs

import (
	"time"

	"github.com/square/keywhiz-fs/log"
)

// SecretBackend represents an interface for storing secrets.
type SecretBackend interface {
	Secret(string) (secret *Secret, ok bool)
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
}

// Cache contains necessary state to return secrets, using previously cached content or retrieving
// from a server if necessary.
type Cache struct {
	*log.Logger
	secretMap *SecretMap
	backend   SecretBackend
	timeouts  Timeouts
}

// NewCache initializes a Cache.
func NewCache(backend SecretBackend, timeouts Timeouts, logConfig log.Config) *Cache {
	logger := log.New("kwfs_cache", logConfig)
	return &Cache{logger, NewSecretMap(), backend, timeouts}
}

// Clear empties the internal cache.
func (c *Cache) Clear() {
	c.Infof("Cache cleared")
	c.secretMap = NewSecretMap()
}

// Secret retrieves a Secret by name from cache or a server.
//
// Cache logic:
//  * If cache hit and very recent: return cache entry
//  * Ask backend w/ timeout
//  * If backend returns fast: update cache, return
//  * If timeout_backend_deadline AND cache hit: return cache entry, background update cache when
//    backend returns
//  * If timeout_max_wait: log error and pretend file doesn't exist
func (c *Cache) Secret(name string) (*Secret, bool) {
	failureDeadline := time.After(c.timeouts.MaxWait)
	var backendDeadline <-chan time.Time // inactive, until backend request starts

	var cachedSecret *Secret
	resultFromCache := func() (*Secret, bool) {
		success := cachedSecret != nil
		return cachedSecret, success
	}

	cacheDone := c.cacheSecret(name)
	var backendDone chan *Secret

	for {
		select {
		case s := <-backendDone:
			backendDone = nil
			if s != nil { // Always return successful value from backend
				return s, true
			}

			// Backend failed and cache lookup already finished
			if cacheDone == nil {
				return resultFromCache()
			}
		case s := <-cacheDone:
			cacheDone = nil
			if s != nil {
				cachedSecret = &s.Secret

				// If cache entry very recent, return cache result
				if time.Since(s.Time) < c.timeouts.Fresh {
					return resultFromCache()
				}
			}

			// Start backend request and wait until optimistic deadline
			backendDone = c.backendSecret(name)
			backendDeadline = time.After(c.timeouts.BackendDeadline)
		case <-backendDeadline:
			if cachedSecret != nil {
				return cachedSecret, true
			}
		case <-failureDeadline:
			c.Errorf("Cache and backend timeout: %v", name)
			return nil, false
		}
	}
}

// SecretList returns a listing of Secrets from cache or a server.
//
// Cache logic:
//  * Ask backend w/ timeout
//  * If backend returns fast: update cache, return
//  * If timeout_backend_deadline: return cache entries, background update cache when
//    backend returns
//  * If timeout_max_wait: log error and pretend no files
func (c *Cache) SecretList() []Secret {
	failureDeadline := time.After(c.timeouts.MaxWait)
	// Optimistically wait for a backend response before using a cached response.
	backendDeadline := time.After(c.timeouts.BackendDeadline)

	cacheDone := c.cacheSecretList()
	backendDone := c.backendSecretList()

	var cachedSecrets []Secret
	for {
		select {
		case secrets := <-backendDone:
			return secrets
		case cachedSecrets = <-cacheDone:
			cacheDone = nil
		case <-backendDeadline:
			if cachedSecrets != nil {
				return cachedSecrets
			}
		case <-failureDeadline:
			c.Errorf("Cache and backend timeout: secretList()")
			return make([]Secret, 0)
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
//
// Cache lookup may block, so retrieval is concurrent and a channel is returned to communicate a
// successful value. The channel will not be fulfilled on error.
func (c *Cache) cacheSecret(name string) chan *SecretTime {
	secretc := make(chan *SecretTime, 1)
	go func() {
		defer close(secretc)
		secret, ok := c.secretMap.Get(name)
		if ok && len(secret.Secret.Content) > 0 {
			c.Debugf("Cache hit: %v", name)
			secretc <- &secret
		} else {
			c.Debugf("Cache miss: %v", name)
			secretc <- nil
		}
	}()
	return secretc
}

// cacheSecretList retrieves a secret listing from the cache.
//
// Cache lookup may block, so retrieval is concurrent and a channel is returned to communicate
// a cache lookup result.
func (c *Cache) cacheSecretList() chan []Secret {
	secretsc := make(chan []Secret, 1)
	go func() {
		defer close(secretsc)
		values := c.secretMap.Values()
		secrets := make([]Secret, len(values))
		for i, v := range values {
			secrets[i] = v.Secret
		}
		secretsc <- secrets
	}()
	return secretsc
}

// backendSecret retrieves a secret from the backend and updates the cache.
//
// Retrieval is concurrent, so a channel is returned to communicate a successful value. The channel
// will not be fulfilled on error.
func (c *Cache) backendSecret(name string) chan *Secret {
	secretc := make(chan *Secret)
	go func() {
		defer close(secretc)
		secret, ok := c.backend.Secret(name)
		if !ok {
			secretc <- nil
			return
		}

		secretc <- secret
		c.secretMap.Put(name, *secret)
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
			return
		}

		secretsc <- secrets
		close(secretsc)

		newMap := NewSecretMap()

		for _, backendSecret := range secrets {
			// If the cache contains a secret with content, keep it over backendSecret.
			if s, ok := c.secretMap.Get(backendSecret.Name); ok && len(s.Secret.Content) > 0 {
				newMap.Put(backendSecret.Name, s.Secret)
			} else { // Otherwise, cache the latest information.
				newMap.Put(backendSecret.Name, backendSecret)
			}
		}
		c.secretMap.Overwrite(newMap)
	}()
	return secretsc
}
