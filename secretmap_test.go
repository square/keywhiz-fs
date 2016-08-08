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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestSecretMapOperations(t *testing.T) {
	assert := assert.New(t)

	s, err := ParseSecret(fixture("secret.json"))
	assert.NoError(err)

	fake_now := time.Now()

	secretMap := NewSecretMap(timeouts, func() time.Time { return fake_now })
	assert.Equal(0, secretMap.Len())
	assert.Empty(secretMap.Values())

	lookup, ok := secretMap.Get("foo")
	assert.False(ok)

	secretMap.Put("foo", *s, time.Time{})
	assert.Equal(1, secretMap.Len())

	values := secretMap.Values()
	assert.Len(values, 1)
	assert.Equal(*s, values[0])

	lookup, ok = secretMap.Get("foo")
	assert.True(ok)
	assert.Equal(*s, lookup.Secret)

	secretMap.Put("foo", Secret{}, time.Time{})

	lookup, ok = secretMap.Get("foo")
	assert.True(ok)
	assert.NotEqual(*s, lookup.Secret)

	// Put a secret
	secretMap.Put("foo", *s, time.Time{})
	secretMap.DeleteAll()
	// Secret should still exist for a short amount of time
	values = secretMap.Values()
	assert.Len(values, 1)
	assert.Equal(*s, values[0])
	// Advance current time by more than an hour, secret should now be gone
	fake_now = fake_now.Add(2 * time.Hour)
	values = secretMap.Values()
	assert.Len(values, 0)
}

func TestSecretMapTimestamp(t *testing.T) {
	assert := assert.New(t)

	secretMap := NewSecretMap(timeouts, nil)
	secretMap.Put("foo", Secret{}, time.Time{})

	val, ok := secretMap.Get("foo")
	assert.True(ok)
	earlierTime := val.Time

	secretMap.Put("foo", Secret{}, time.Time{})
	val, ok = secretMap.Get("foo")
	assert.True(ok)
	assert.True(val.Time.After(earlierTime))
}
