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
	"errors"
	"testing"
	"time"

	"github.com/square/keywhiz-fs/log"
	"github.com/stretchr/testify/assert"
)

var logConfig = log.Config{Debug: false, Mountpoint: "/tmp/mnt"}

// FailingBackend always returns ok==false
type FailingBackend struct {
}

func (b FailingBackend) Secret(name string) (*Secret, error) {
	return nil, errors.New("some error")
}

func (b FailingBackend) SecretList() ([]Secret, bool) {
	return nil, false
}

// DeletedBackend, always returns ok==true, deleted==true
type DeletedBackend struct {
}

func (b DeletedBackend) Secret(name string) (*Secret, error) {
	return nil, SecretDeleted{}
}

func (b DeletedBackend) SecretList() ([]Secret, bool) {
	return []Secret{}, true
}

// ChannelBackend reads values from channels to return or blocks.
type ChannelBackend struct {
	secretc     chan *Secret
	secretListc chan []Secret
}

func (b ChannelBackend) Secret(name string) (*Secret, error) {
	secret := <-b.secretc
	return secret, nil
}

func (b ChannelBackend) SecretList() ([]Secret, bool) {
	secretList := <-b.secretListc
	return secretList, true
}

var timeouts = Timeouts{0, 10 * time.Millisecond, 20 * time.Millisecond, 1 * time.Hour}

func TestCacheSecretUsesValuesFromClient(t *testing.T) {
	assert := assert.New(t)

	secretFixture, _ := ParseSecret(fixture("secret.json"))

	secretc := make(chan *Secret, 1)
	backend := ChannelBackend{secretc: secretc}
	secretc <- secretFixture

	cache := NewCache(backend, timeouts, logConfig, nil)
	secret, ok := cache.Secret("password-file")
	assert.True(ok)
	assert.Equal(secretFixture, secret)
}

func TestCachePassesThroughSecretNotFound(t *testing.T) {
	assert := assert.New(t)

	secretFixture, _ := ParseSecret(fixture("secret.json"))

	fake_clock := time.Now()
	cache := NewCache(FailingBackend{}, timeouts, logConfig, func() time.Time { return fake_clock })
	secret, ok := cache.Secret(secretFixture.Name)
	assert.False(ok)
	assert.Nil(secret)

	cache.Add(*secretFixture)
	secret, ok = cache.Secret(secretFixture.Name)
	assert.True(ok)
	assert.Equal(secretFixture, secret)

	// After a while, the secret should still be there since the backend is failing.
	fake_clock = fake_clock.Add(2 * time.Hour)
	secret, ok = cache.Secret(secretFixture.Name)
	assert.True(ok)
	assert.Equal(secretFixture, secret)
}

func TestCachePassesThroughSecretDeleted(t *testing.T) {
	assert := assert.New(t)

	secretFixture, _ := ParseSecret(fixture("secret.json"))

	fake_clock := time.Now()
	cache := NewCache(DeletedBackend{}, timeouts, logConfig, func() time.Time { return fake_clock })
	secret, ok := cache.Secret(secretFixture.Name)
	assert.False(ok)
	assert.Nil(secret)

	cache.Add(*secretFixture)
	secret, ok = cache.Secret(secretFixture.Name)
	assert.True(ok)
	assert.Equal(secretFixture, secret)

	// After a while, secret should still be there since the backend is failing.
	fake_clock = fake_clock.Add(2 * time.Hour)
	_, ok = cache.Secret(secretFixture.Name)
	assert.False(ok)
}

func TestCacheSecretWhenClientTimesOut(t *testing.T) {
	assert := assert.New(t)

	secretFixture, _ := ParseSecret(fixture("secret.json"))
	backend := ChannelBackend{} // channels are nil and will block
	fake_clock := time.Now()
	cache := NewCache(backend, timeouts, logConfig, func() time.Time { return fake_clock })

	// empty cache
	secret, ok := cache.Secret(secretFixture.Name)
	assert.False(ok)
	assert.Nil(secret)

	// cache with entry
	cache.Add(*secretFixture)
	secret, ok = cache.Secret(secretFixture.Name)
	assert.True(ok)
	assert.Equal(secretFixture, secret)

	// After a while, secret should still be there since the backend is timing out
	fake_clock = fake_clock.Add(2 * time.Hour)
	_, ok = cache.Secret(secretFixture.Name)
	assert.True(ok)
	assert.Equal(secretFixture, secret)
}

func TestCacheSecretUsesClientOverCache(t *testing.T) {
	assert := assert.New(t)

	fixture1, _ := ParseSecret(fixture("secret.json"))
	fixture2, _ := ParseSecret(fixture("secretNormalOwner.json"))
	fixture2.Name = fixture1.Name

	secretc := make(chan *Secret, 1)
	backend := ChannelBackend{secretc: secretc}
	secretc <- fixture1

	cache := NewCache(backend, timeouts, logConfig, nil)
	cache.Add(*fixture2)

	// Although fixture2 is in the cache, the client returns fixture1.
	secret, ok := cache.Secret(fixture2.Name)
	assert.True(ok)
	assert.Equal(fixture1, secret)

	assert.Equal(1, cache.Len())
}

func TestCacheSecretAvoidsBackendWhenResultFresh(t *testing.T) {
	assert := assert.New(t)

	fixture1, _ := ParseSecret(fixture("secret.json"))
	fixture2, _ := ParseSecret(fixture("secretNormalOwner.json"))
	fixture2.Name = fixture1.Name

	// Backend has fixture1, cache has fixture2
	secretc := make(chan *Secret, 1)
	backend := ChannelBackend{secretc: secretc}
	secretc <- fixture1

	// 1 Hour fresh threshold is sure to be fresh
	timeouts := Timeouts{1 * time.Hour, 10 * time.Millisecond, 20 * time.Millisecond, 1 * time.Hour}
	cache := NewCache(backend, timeouts, logConfig, nil)
	cache.Add(*fixture2)

	secret, ok := cache.Secret(fixture2.Name)
	assert.True(ok)
	assert.Equal(fixture2, secret)
	secret, ok = cache.Secret(fixture2.Name)
	assert.True(ok)
	assert.Equal(fixture2, secret)

	// 1 Nanosecond fresh threshold is sure to make a server request
	timeouts = Timeouts{1 * time.Nanosecond, 10 * time.Millisecond, 20 * time.Millisecond, 1 * time.Hour}
	cache = NewCache(backend, timeouts, logConfig, nil)
	cache.Add(*fixture2)
	time.Sleep(2 * time.Nanosecond)

	secret, ok = cache.Secret(fixture2.Name)
	assert.True(ok)
	assert.Equal(fixture1, secret) // fixture1 comes form the backend
}

func TestCacheSecretUsesBackendWhenResultStale(t *testing.T) {
	assert := assert.New(t)

	fixture1, _ := ParseSecret(fixture("secret.json"))
	fixture2, _ := ParseSecret(fixture("secretNormalOwner.json"))
	fixture2.Name = fixture1.Name

	// Backend returns fixture1, then fixture2
	secretc := make(chan *Secret, 2)
	backend := ChannelBackend{secretc: secretc}
	secretc <- fixture1
	secretc <- fixture2

	timeouts = Timeouts{1 * time.Nanosecond, 10 * time.Millisecond, 20 * time.Millisecond, 1 * time.Hour}
	cache := NewCache(backend, timeouts, logConfig, nil)
	secret, ok := cache.Secret(fixture1.Name)
	assert.True(ok)
	assert.Equal(fixture1, secret)

	time.Sleep(2 * time.Nanosecond)
	secret, ok = cache.Secret(fixture1.Name)
	assert.True(ok)
	assert.Equal(fixture2, secret)
}

func TestCacheSecretListUsesValuesFromCacheIfClientFails(t *testing.T) {
	assert := assert.New(t)

	secretFixture, _ := ParseSecret(fixture("secret.json"))

	fake_clock := time.Now()
	cache := NewCache(FailingBackend{}, timeouts, logConfig, func() time.Time { return fake_clock })
	cache.Add(*secretFixture)
	list := cache.SecretList()
	assert.Len(list, 1)
	assert.Contains(list, *secretFixture)

	// After a while, secret should still be there since the backend failed
	fake_clock = fake_clock.Add(2 * time.Hour)
	list = cache.SecretList()
	assert.Len(list, 1)
	assert.Contains(list, *secretFixture)
}

func TestCacheSecretListsDeleted(t *testing.T) {
	assert := assert.New(t)

	secretFixture, _ := ParseSecret(fixture("secret.json"))

	fake_clock := time.Now()
	cache := NewCache(DeletedBackend{}, timeouts, logConfig, func() time.Time { return fake_clock })
	cache.Add(*secretFixture)
	list := cache.SecretList()
	assert.Len(list, 1)
	assert.Contains(list, *secretFixture)

	// After a while, secret should be deleted
	fake_clock = fake_clock.Add(2 * time.Hour)
	list = cache.SecretList()
	assert.Len(list, 0)
}

func TestCacheSecretListWhenClientTimesOut(t *testing.T) {
	assert := assert.New(t)

	secretFixture, _ := ParseSecret(fixture("secret.json"))
	backend := ChannelBackend{} // channels are nil and will block
	fake_clock := time.Now()
	cache := NewCache(backend, timeouts, logConfig, func() time.Time { return fake_clock })

	// cache empty
	list := cache.SecretList()
	assert.Empty(list)

	// cache with entry
	cache.Add(*secretFixture)
	list = cache.SecretList()
	assert.Len(list, 1)
	assert.Contains(list, *secretFixture)

	// After a while, secret should still be there since the backend failed
	fake_clock = fake_clock.Add(2 * time.Hour)
	list = cache.SecretList()
	assert.Len(list, 1)
	assert.Contains(list, *secretFixture)
}

func TestCacheSecretListUsesValuesFromClient(t *testing.T) {
	assert := assert.New(t)

	secretFixture, _ := ParseSecret(fixture("secret.json"))

	secretListc := make(chan []Secret, 1)
	backend := ChannelBackend{secretListc: secretListc}
	secretListc <- []Secret{*secretFixture}

	cache := NewCache(backend, timeouts, logConfig, nil)
	list := cache.SecretList()
	assert.Len(list, 1)
	assert.Contains(list, *secretFixture)

	assert.Equal(1, cache.Len())
}

func TestCacheSecretListUsesClientOverCache(t *testing.T) {
	assert := assert.New(t)

	fixture1, _ := ParseSecret(fixture("secret.json"))
	fixture2, _ := ParseSecret(fixture("secretNormalOwner.json"))

	secretListc := make(chan []Secret, 1)
	backend := ChannelBackend{secretListc: secretListc}
	secretListc <- []Secret{*fixture1}

	fake_clock := time.Now()
	cache := NewCache(backend, timeouts, logConfig, func() time.Time { return fake_clock })
	cache.Add(*fixture2)

	// The cache contains fixture2, the backend only returns fixture1.
	// fixture2 gets marked for deletion.
	list := cache.SecretList()
	assert.Len(list, 2)
	assert.Contains(list, *fixture1)
	assert.Equal(2, cache.Len())

	// Advance clock, cache should now have only 1 element
	fake_clock = fake_clock.Add(2 * time.Hour)
	assert.Equal(1, cache.Len())
}

func TestCacheClears(t *testing.T) {
	assert := assert.New(t)

	cache := NewCache(nil, timeouts, logConfig, nil)

	secretFixture, _ := ParseSecret(fixture("secret.json"))
	cache.Add(*secretFixture)
	assert.NotEqual(0, cache.Len())

	cache.Clear()
	assert.Equal(0, cache.Len())
}

func TestCacheSecretListDoesNotOverrideWithEmptyContent(t *testing.T) {
	assert := assert.New(t)

	secretFixture, _ := ParseSecret(fixture("secret.json"))

	secretListc := make(chan []Secret, 1)
	backend := ChannelBackend{secretListc: secretListc}

	cache := NewCache(backend, timeouts, logConfig, nil)
	cache.Add(*secretFixture)

	// Secret list should not override the cache entry with an empty value
	secretFixtureWithNoData, _ := ParseSecret(fixture("secret.json"))
	secretFixtureWithNoData.Content = content{}
	secretListc <- []Secret{*secretFixtureWithNoData}

	list := cache.SecretList()
	assert.Len(list, 1)
	assert.Contains(list, *secretFixture)
	assert.Equal(1, cache.Len())
}

// An interesting test to write might be a combination of data being returned and deleted.
// E.g.
// Get content A.
// Get content B.
// Delete A.
// list.
// make sure A & B are still there.
// time passes.
// make sure A goes away, B is still there.
