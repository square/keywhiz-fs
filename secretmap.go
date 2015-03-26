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
	"sync"
	"time"
)

// SecretMap is a thread-safe map for storing key -> secret mapping.
type SecretMap struct {
	m    map[string]SecretTime
	lock sync.RWMutex
}

// SecretTime contains a Secret record along with a timestamp when it was inserted.
type SecretTime struct {
	Secret Secret
	Time   time.Time
}

// NewSecretMap initializes a new SecretMap.
func NewSecretMap() *SecretMap {
	return &SecretMap{make(map[string]SecretTime), sync.RWMutex{}}
}

// Get retrieves a values from the map and indicates if the lookup was ok.
func (m *SecretMap) Get(key string) (s SecretTime, ok bool) {
	m.lock.RLock()
	s, ok = m.m[key]
	m.lock.RUnlock()
	return
}

// Put places a value in the map with a key, possibly overwriting an existing entry.
func (m *SecretMap) Put(key string, value Secret) {
	m.lock.Lock()
	m.m[key] = SecretTime{value, time.Now()}
	m.lock.Unlock()
}

// PutIfAbsent places a value in the map with a key, if that key did not exist.
// Returns whether the value was placed.
func (m *SecretMap) PutIfAbsent(key string, value Secret) (put bool) {
	m.lock.Lock()
	if _, ok := m.m[key]; !ok {
		m.m[key] = SecretTime{value, time.Now()}
		put = true
	}
	m.lock.Unlock()
	return
}

// Values returns a slice of stored secrets in no particular order.
func (m *SecretMap) Values() []SecretTime {
	m.lock.RLock()
	values := make([]SecretTime, len(m.m))
	i := 0
	for _, value := range m.m {
		values[i] = value
		i++
	}
	m.lock.RUnlock()
	return values
}

// Len returns the count of values stored.
func (m *SecretMap) Len() int {
	m.lock.RLock()
	defer m.lock.RUnlock()
	return len(m.m)
}

// Overwrite will copy and overwrite data from another SecretMap.
func (m *SecretMap) Overwrite(m2 *SecretMap) {
	m.lock.Lock()
	defer m.lock.Unlock()
	m2.lock.RLock()
	defer m2.lock.RUnlock()
	m.m = m2.m
}
