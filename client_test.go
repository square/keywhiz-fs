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
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

var (
	clientFile = "fixtures/client.pem"
	testCaFile = "fixtures/localhost.crt"
)

func TestClientCallsServer(t *testing.T) {
	assert := assert.New(t)

	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/secrets"):
			fmt.Fprint(w, string(fixture("secrets.json")))
		case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/secret/foo"):
			fmt.Fprint(w, string(fixture("secret.json")))
		default:
			w.WriteHeader(404)
		}
	}))
	server.TLS = testCerts(testCaFile)
	server.StartTLS()
	defer server.Close()

	serverURL, _ := url.Parse(server.URL)
	client := NewClient(clientFile, clientFile, testCaFile, serverURL, time.Second, logConfig, false)

	secrets, ok := client.SecretList()
	assert.True(ok)
	assert.Len(secrets, 2)

	data, ok := client.RawSecretList()
	assert.True(ok)
	assert.Equal(fixture("secrets.json"), data)

	secret, ok := client.Secret("foo")
	assert.True(ok)
	assert.Equal("Nobody_PgPass", secret.Name)

	data, ok = client.RawSecret("foo")
	assert.True(ok)
	assert.Equal(fixture("secret.json"), data)

	_, ok = client.Secret("non-existent")
	assert.False(ok)
}
