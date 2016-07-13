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
	"sync"
	"time"
)

// SecretMap is a thread-safe map for storing key -> secret mapping.
type SecretMap struct {
	m        map[string]SecretTime
	lock     sync.Mutex
	timeouts Timeouts
	now      func() time.Time
}

// SecretTime contains a Secret record along with a timestamp when it was inserted.
// We often rotate secrets in Keywhiz by deleting the existing secret and adding
// a new one. This implies, we risk purging the secret from the cache if we don't
// set a time to live. If the ttl value is 0, we keep the secret until it gets deleted
// and its ttl changes.
type SecretTime struct {
	Secret Secret
	Time   time.Time
	ttl    time.Time
}

// NewSecretMap initializes a new SecretMap.
func NewSecretMap(timeouts Timeouts, now func() time.Time) *SecretMap {
	return &SecretMap{make(map[string]SecretTime), sync.Mutex{}, timeouts, now}
}

func (m *SecretMap) getNow() time.Time {
	if m.now == nil {
		return time.Now()
	}
	return m.now()
}

func isExpired(s SecretTime, now time.Time) bool {
	return !s.ttl.IsZero() && s.ttl.Before(now)
}

// Get retrieves a values from the map and indicates if the lookup was ok.
func (m *SecretMap) Get(key string) (s SecretTime, ok bool) {
	m.lock.Lock()
	defer m.lock.Unlock()

	s, ok = m.m[key]
	if ok && isExpired(s, m.getNow()) {
		delete(m.m, key)
		return SecretTime{}, false
	}
	return
}

// Put places a value in the map with a key, possibly overwriting an existing entry.
func (m *SecretMap) Put(key string, value Secret) {
	m.lock.Lock()
	defer m.lock.Unlock()

	m.m[key] = SecretTime{value, m.getNow(), time.Time{}}
}

// Schedules an entry for deletion.
func (m *SecretMap) Delete(key string) {
	m.lock.Lock()
	defer m.lock.Unlock()
	expire := m.getNow().Add(m.timeouts.DeletionDelay)
	v, ok := m.m[key]
	if ok {
		if v.ttl.IsZero() {
			v.ttl = expire
		}
		m.m[key] = v
	}
}

// Schedules all values for deletion. Entries will be dropped if they aren't put back
// before DeletionDelay elapses.
func (m *SecretMap) DeleteAll() {
	m.lock.Lock()
	defer m.lock.Unlock()
	expire := m.getNow().Add(m.timeouts.DeletionDelay)
	for k, v := range m.m {
		if v.ttl.IsZero() {
			v.ttl = expire
		}
		m.m[k] = v
	}
}

// Similar to Overwrite, but keeps all the keys which aren't in m2 and marks them for delayed deletion.
func (m *SecretMap) Replace(m2 *SecretMap) {
	m.lock.Lock()
	defer m.lock.Unlock()
	m2.lock.Lock()
	defer m2.lock.Unlock()

	// Delete existing entries
	expire := m.getNow().Add(m.timeouts.DeletionDelay)
	for k, v := range m.m {
		// Only hold on to secrets which actually have data.
		if len(v.Secret.Content) == 0 {
			delete(m.m, k)
		} else if v.ttl.IsZero() {
			v.ttl = expire
			m.m[k] = v
		}
	}

	// Replace values with data from m2
	for k, v := range m2.m {
		m.m[k] = v
	}
}

// Values returns a slice of stored secrets in no particular order.
func (m *SecretMap) Values() []SecretTime {
	m.lock.Lock()
	defer m.lock.Unlock()

	values := make([]SecretTime, len(m.m))
	i := 0
	now := m.getNow()
	for key, value := range m.m {
		if isExpired(value, now) {
			delete(m.m, key)
		} else {
			values[i] = value
			i++
		}
	}
	return values[0:i]
}

// Len returns the count of values stored (not including keys marked for
// delayed deletion).
// Only used by tests.
func (m *SecretMap) Len() int {
	return len(m.Values())
}

// Overwrite will copy and overwrite data from another SecretMap.
func (m *SecretMap) Overwrite(m2 *SecretMap) {
	m.lock.Lock()
	defer m.lock.Unlock()
	m2.lock.Lock()
	defer m2.lock.Unlock()
	m.m = m2.m
}
