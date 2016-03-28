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
	m    map[string]SecretTime
	lock sync.Mutex
}

// SecretTime contains a Secret record along with a timestamp when it was inserted.
type SecretTime struct {
	Secret Secret
	Time   time.Time
}

// NewSecretMap initializes a new SecretMap.
func NewSecretMap() *SecretMap {
	return &SecretMap{make(map[string]SecretTime), sync.Mutex{}}
}

// Get retrieves a values from the map and indicates if the lookup was ok.
func (m *SecretMap) Get(key string) (s SecretTime, ok bool) {
	m.lock.Lock()
	defer m.lock.Unlock()

	s, ok = m.m[key]
	return
}

// Put places a value in the map with a key, possibly overwriting an existing entry.
func (m *SecretMap) Put(key string, value Secret) {
	m.lock.Lock()
	defer m.lock.Unlock()

	m.m[key] = SecretTime{value, time.Now()}
}

// Values returns a slice of stored secrets in no particular order.
func (m *SecretMap) Values() []SecretTime {
	m.lock.Lock()
	defer m.lock.Unlock()

	values := make([]SecretTime, len(m.m))
	i := 0
	for _, value := range m.m {
		values[i] = value
		i++
	}
	return values
}

// Len returns the count of values stored.
func (m *SecretMap) Len() int {
	m.lock.Lock()
	defer m.lock.Unlock()

	return len(m.m)
}

// Overwrite will copy and overwrite data from another SecretMap.
func (m *SecretMap) Overwrite(m2 *SecretMap) {
	m.lock.Lock()
	defer m.lock.Unlock()
	m2.lock.Lock()
	defer m2.lock.Unlock()
	m.m = m2.m
}
