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
	"golang.org/x/sys/unix"
)

func TestDeserializeSecret(t *testing.T) {
	assert := assert.New(t)

	s, err := ParseSecret(fixture("secret.json"))
	assert.NoError(err)
	assert.Equal("Nobody_PgPass", s.Name)
	assert.EqualValues(6, s.Length)
	assert.False(s.IsVersioned)
	assert.Equal("0400", s.Mode)
	assert.Equal("nobody", s.Owner)
	assert.Equal("nobody", s.Group)
	assert.EqualValues("asddas", s.Content)

	expectedCreatedAt := time.Date(2011, time.September, 29, 15, 46, 0, 232000000, time.UTC)
	assert.Equal(s.CreatedAt.Unix(), expectedCreatedAt.Unix())
}

func TestDeserializeSecretWithoutBase64Padding(t *testing.T) {
	assert := assert.New(t)

	s, err := ParseSecret(fixture("secretWithoutBase64Padding.json"))
	assert.NoError(err)
	assert.Equal("NonexistentOwner_Pass", s.Name)
	assert.EqualValues("12345", s.Content)
}

func TestDeserializeSecretList(t *testing.T) {
	assert := assert.New(t)

	fixtures := []string{"secrets.json", "secretsWithoutContent.json"}
	for _, f := range fixtures {
		secrets, err := ParseSecretList(fixture(f))
		assert.NoError(err)
		assert.Len(secrets, 2)
	}
}

func TestSecretModeValue(t *testing.T) {
	assert := assert.New(t)

	cases := []struct {
		secret Secret
		mode   uint32
	}{
		{Secret{Mode: "0440"}, 288},
		{Secret{Mode: "0400"}, 256},
		{Secret{}, 288},
	}
	for _, c := range cases {
		assert.Equal(c.mode|unix.S_IFREG, c.secret.ModeValue())
	}
}
