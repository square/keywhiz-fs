// +build !race

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

package keywhizfs_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hanwen/go-fuse/fuse"
	"github.com/hanwen/go-fuse/fuse/nodefs"
	"github.com/square/keywhiz-fs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

const _SomeUID uint32 = 12345

var fuseContext = &fuse.Context{Owner: fuse.Owner{Uid: 0, Gid: 0}}

type FsTestSuite struct {
	suite.Suite
	url    string
	assert *assert.Assertions
	fs     *keywhizfs.KeywhizFs
}

func (suite *FsTestSuite) SetupTest() {
	timeouts := keywhizfs.Timeouts{0, 10 * time.Millisecond, 20 * time.Millisecond}
	client := keywhizfs.NewClient(clientFile, clientFile, caFile, suite.url, timeouts.MaxWait, logConfig, false)
	ownership := keywhizfs.Ownership{Uid: _SomeUID, Gid: _SomeUID}
	kwfs, _, _ := keywhizfs.NewKeywhizFs(&client, ownership, timeouts, logConfig)
	suite.fs = kwfs
}

func (suite *FsTestSuite) TestSpecialFileAttrs() {
	assert := suite.assert

	cases := []struct {
		filename string
		size     int
		mode     int
	}{
		{"", 4096, 0755 | fuse.S_IFDIR},
		{".version", len(keywhizfs.VERSION), 0444 | fuse.S_IFREG},
		{".running", -1, 0444 | fuse.S_IFREG},
		{".clear_cache", 0, 0440 | fuse.S_IFREG},
		{".json", 4096, 0700 | fuse.S_IFDIR},
		{".json/secret", 4096, 0700 | fuse.S_IFDIR},
		{".json/secrets", -1, 0400 | fuse.S_IFREG},
	}

	for _, c := range cases {
		attr, status := suite.fs.GetAttr(c.filename, nil)
		assert.Equal(fuse.OK, status, "Expected %v attr status to be fuse.OK", c.filename)
		assert.EqualValues(c.mode, attr.Mode, "Expected %v mode %#o, was %#o", c.filename, c.mode, attr.Mode)
		if c.size >= 0 {
			assert.EqualValues(c.size, attr.Size, "Expected %v size %d, was %d", c.filename, c.size, attr.Size)
		}
	}
}

func (suite *FsTestSuite) TestFileAttrs() {
	assert := suite.assert

	nobodySecretData := fixture("secret.json")
	nobodySecret, _ := keywhizfs.ParseSecret(nobodySecretData)
	hmacSecretData := fixture("secretNormalOwner.json")
	hmacSecret, _ := keywhizfs.ParseSecret(hmacSecretData)
	secretListData := fixture("secrets.json")

	cases := []struct {
		filename string
		content  []byte
		mode     uint32
	}{
		{"hmac.key", hmacSecret.Content, 0440 | fuse.S_IFREG},
		{"Nobody_PgPass", nobodySecret.Content, 0400 | fuse.S_IFREG},
		{".json/secret/hmac.key", hmacSecretData, 0400 | fuse.S_IFREG},
		{".json/secret/Nobody_PgPass", nobodySecretData, 0400 | fuse.S_IFREG},
		{".json/secrets", secretListData, 0400 | fuse.S_IFREG},
	}

	for _, c := range cases {
		attr, status := suite.fs.GetAttr(c.filename, fuseContext)
		assert.Equal(fuse.OK, status, "Expected %v attr status to be fuse.OK", c.filename)
		assert.Equal(c.mode, attr.Mode, "Expected %v mode %#o, was %#o", c.filename, c.mode, attr.Mode)
		assert.Equal(uint32(len(c.content)), attr.Size, "Expected %v size to match", c.filename)
	}
}

func (suite *FsTestSuite) TestFileAttrOwnership() {
	assert := suite.assert

	cases := []string{
		".clear_cache",
		".json/secret/hmac.key",
		".json/secrets",
		".running",
		".version",
		"hmac.key",
	}

	for _, filename := range cases {
		attr, status := suite.fs.GetAttr(filename, fuseContext)
		assert.Equal(fuse.OK, status, "Expected %v attr status to be fuse.OK", filename)
		assert.Equal(_SomeUID, attr.Uid, "Expected %v uid to be default", filename)
		assert.Equal(_SomeUID, attr.Gid, "Expected %v gid to be default", filename)
	}

	filename := "Nobody_PgPass"
	attr, status := suite.fs.GetAttr(filename, fuseContext)
	assert.Equal(fuse.OK, status, "Expected %v attr status to be fuse.OK", filename)
	assert.NotEqual(_SomeUID, attr.Uid, "Expected %v uid to not be default", filename)
	assert.NotEqual(0, attr.Uid, "Expected %v uid to be set", filename)
	assert.NotEqual(_SomeUID, attr.Gid, "Expected %v gid to not be default", filename)
	assert.NotEqual(0, attr.Gid, "Expected %v gid to be set", filename)
}

func (suite *FsTestSuite) TestSpecialFileOpen() {
	assert := suite.assert

	read := func(f nodefs.File) []byte {
		buf := make([]byte, 4000)
		res, _ := f.Read(buf, 0)
		bytes, _ := res.Bytes(buf)
		return bytes
	}

	file, status := suite.fs.Open(".version", 0, fuseContext)
	assert.Equal(fuse.OK, status)
	assert.EqualValues(keywhizfs.VERSION, read(file))

	file, status = suite.fs.Open(".clear_cache", 0, fuseContext)
	assert.Equal(fuse.OK, status)
	assert.Empty(read(file))

	file, status = suite.fs.Open(".running", 0, fuseContext)
	assert.Equal(fuse.OK, status)
	assert.Contains(string(read(file)), "pid=")
}

func (suite *FsTestSuite) TestOpen() {
	assert := suite.assert

	nobodySecretData := fixture("secret.json")
	nobodySecret, _ := keywhizfs.ParseSecret(nobodySecretData)
	hmacSecretData := fixture("secretNormalOwner.json")
	hmacSecret, _ := keywhizfs.ParseSecret(hmacSecretData)
	secretListData := fixture("secrets.json")

	read := func(f nodefs.File) []byte {
		buf := make([]byte, 4000)
		res, _ := f.Read(buf, 0)
		bytes, _ := res.Bytes(buf)
		return bytes
	}

	cases := []struct {
		filename string
		content  []byte
	}{
		{"hmac.key", hmacSecret.Content},
		{"Nobody_PgPass", nobodySecret.Content},
		{".json/secret/hmac.key", hmacSecretData},
		{".json/secret/Nobody_PgPass", nobodySecretData},
		{".json/secrets", secretListData},
	}

	for _, c := range cases {
		file, status := suite.fs.Open(c.filename, 0, fuseContext)
		assert.Equal(fuse.OK, status, "Expected %v open status to be fuse.OK", c.filename)
		assert.Equal(c.content, read(file), "Expected %v file content to match", c.filename)
	}
}

func (suite *FsTestSuite) TestOpenBadFiles() {
	assert := suite.assert

	cases := []struct {
		filename string
		status   fuse.Status
	}{
		{"", keywhizfs.EISDIR},
		{"non-existent", fuse.ENOENT},
		{".json/secret/non-existent", fuse.ENOENT},
		{".json/secret", keywhizfs.EISDIR},
	}

	for _, c := range cases {
		_, status := suite.fs.Open(c.filename, 0, fuseContext)
		assert.Equal(c.status, status, "Expected %v open status to match", c.filename)
	}
}

func (suite *FsTestSuite) TestOpenDir() {
	assert := suite.assert

	cases := []struct {
		directory string
		contents  map[string]bool // name -> isFile?
	}{
		{
			"",
			map[string]bool{
				".version":     true,
				".running":     true,
				".clear_cache": true,
				".json":        false,
				"General_Password..0be68f903f8b7d86": true,
				"Nobody_PgPass":                      true,
			},
		},
		{
			".json",
			map[string]bool{
				"secret":  false,
				"secrets": true,
			},
		},
		{
			".json/secret",
			map[string]bool{
				"General_Password..0be68f903f8b7d86": true,
				"Nobody_PgPass":                      true,
			},
		},
	}

	for _, c := range cases {
		fsEntries, status := suite.fs.OpenDir(c.directory, fuseContext)
		assert.Equal(fuse.OK, status)
		assert.Len(fsEntries, len(c.contents))

		for _, fsEntry := range fsEntries {
			expectedIsFile, ok := c.contents[fsEntry.Name]
			assert.True(ok)
			assert.Equal(expectedIsFile, fsEntry.Mode&fuse.S_IFREG == fuse.S_IFREG)
		}
	}
}

func TestFsTestSuite(t *testing.T) {
	// Starts a server for the duration of the test
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/secrets"):
			fmt.Fprint(w, string(fixture("secrets.json")))
		case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/secret/hmac.key"):
			fmt.Fprint(w, string(fixture("secretNormalOwner.json")))
		case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/secret/Nobody_PgPass"):
			fmt.Fprint(w, string(fixture("secret.json")))
		default:
			w.WriteHeader(404)
		}
	}))
	server.TLS = testCerts(caFile)
	server.StartTLS()
	defer server.Close()

	fsSuite := new(FsTestSuite)
	fsSuite.url = server.URL
	fsSuite.assert = assert.New(t)

	suite.Run(t, fsSuite)
}
