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
	"io/ioutil"
	"os"
	"os/user"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestOwnershipCurrentUser(t *testing.T) {
	current, err := user.Current()
	panicOnError(err)
	ownership := NewOwnership(current.Username, "nobody")
	assert.NotNil(t, ownership, "should never return nil")
	assert.EqualValues(t, ownership.Uid, os.Geteuid())
}

func TestOwnershipInvalidUser(t *testing.T) {
	ownership := NewOwnership("invalid", "invalid")
	assert.NotNil(t, ownership, "should never return nil")
}

func TestGroupFileMissing(t *testing.T) {
	groupFile = "non-existent-file"
	defer func() { groupFile = "/etc/group" }()

	// Should fall back to current egid
	gid := lookupGid("test0")
	assert.EqualValues(t, gid, os.Getegid())
}

func TestGroupFileParsingValid(t *testing.T) {
	file, err := ioutil.TempFile("", "keywhiz-fs-test")
	panicOnError(err)
	defer os.Remove(file.Name())

	file.WriteString("test0:x:1234:test0,test1\n")
	file.WriteString("test1:x:1235:test0,test1\n")
	file.WriteString("test2:x:1236:test0,test1\n")
	file.Sync()

	groupFile = file.Name()
	defer func() { groupFile = "/etc/group" }()

	gid := lookupGid("test0")
	assert.EqualValues(t, 1234, gid)

	gid = lookupGid("test1")
	assert.EqualValues(t, 1235, gid)

	gid = lookupGid("test2")
	assert.EqualValues(t, 1236, gid)
}
