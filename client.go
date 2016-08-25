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
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io/ioutil"
	"net/http"
	"net/url"
	"path"
	"strings"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/rcrowley/go-metrics"
	"github.com/square/go-sq-metrics"
	klog "github.com/square/keywhiz-fs/log"
)

// clientRefresh is the rate the client reloads itself in the background.
var clientRefresh = 10 * time.Minute

// Cipher suites enabled in the client. No RC4 or 3DES.
var ciphers = []uint16{
	tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
	tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
	tls.TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA,
	tls.TLS_ECDHE_ECDSA_WITH_AES_256_CBC_SHA,
	tls.TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA,
	tls.TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA,
	tls.TLS_RSA_WITH_AES_128_CBC_SHA,
	tls.TLS_RSA_WITH_AES_256_CBC_SHA,
}

// Client basic struct.
type Client struct {
	*klog.Logger
	http        func() *http.Client
	url         *url.URL
	params      httpClientParams
	failCount   metrics.Counter
	lastSuccess metrics.Gauge
}

// httpClientParams are values necessary for constructing a TLS client.
type httpClientParams struct {
	CertFile string `json:"cert_file"`
	KeyFile  string `json:"key_file"`
	CaBundle string `json:"ca_bundle"`
	timeout  time.Duration
}

type SecretDeleted struct{}

func (e SecretDeleted) Error() string {
	return "deleted"
}

func (c Client) failCountInc() {
	c.failCount.Inc(1)
}

func (c Client) markSuccess() {
	c.failCount.Clear()
	c.lastSuccess.Update(time.Now().Unix())
}

// NewClient produces a read-to-use client struct given PEM-encoded certificate file, key file, and
// ca file with the list of trusted certificate authorities.
func NewClient(certFile, keyFile, caFile string, serverURL *url.URL, timeout time.Duration, logConfig klog.Config, metricsHandle *sqmetrics.SquareMetrics) (client Client) {
	logger := klog.New("kwfs_client", logConfig)
	params := httpClientParams{certFile, keyFile, caFile, timeout}

	failCount := metrics.GetOrRegisterCounter("runtime.server.fails", metricsHandle.Registry)
	lastSuccess := metrics.GetOrRegisterGauge("runtime.server.lastsuccess", metricsHandle.Registry)

	var httpClient unsafe.Pointer

	// Load HTTP client from atomic pointer
	getClient := func() *http.Client {
		return (*http.Client)(atomic.LoadPointer(&httpClient))
	}

	initial, err := params.buildClient()
	panicOnError(err)

	atomic.StorePointer(&httpClient, unsafe.Pointer(initial))

	// Asynchronously updates client and updates atomic reference
	go func() {
		for t := range time.Tick(clientRefresh) {
			if client, err := params.buildClient(); err == nil {
				logger.Infof("Updating http client at %v", t)
				atomic.StorePointer(&httpClient, unsafe.Pointer(client))
			} else {
				logger.Errorf("Error refreshing http client: %v", err)
			}
		}
	}()

	return Client{logger, getClient, serverURL, params, failCount, lastSuccess}
}

// ServerStatus returns raw JSON from the server's _status endpoint
func (c Client) ServerStatus() (data []byte, err error) {
	now := time.Now()
	t := *c.url
	t.Path = path.Join(c.url.Path, "_status")
	resp, err := c.http().Get(t.String())
	if err != nil {
		c.Errorf("Error retrieving server status: %v", err)
		return nil, err
	}
	c.Infof("GET /_status %d %v", resp.StatusCode, time.Since(now))
	defer resp.Body.Close()

	data, err = ioutil.ReadAll(resp.Body)
	if err != nil {
		c.Errorf("Error reading response body for server status %v", err)
		return nil, err
	}
	return data, nil
}

// RawSecret returns raw JSON from requesting a secret.
func (c Client) RawSecret(name string) (data []byte, err error) {
	now := time.Now()
	// note: path.Join does not know how to properly escape for URLs!
	t := *c.url
	t.Path = path.Join(c.url.Path, "secret", name)
	resp, err := c.http().Get(t.String())
	if err != nil {
		c.Errorf("Error retrieving secret %v: %v", name, err)
		c.failCountInc()
		return nil, err
	}
	c.Infof("GET /secret/%v %d %v", name, resp.StatusCode, time.Since(now))
	defer resp.Body.Close()

	data, err = ioutil.ReadAll(resp.Body)
	if err != nil {
		c.Errorf("Error reading response body for secret %v: %v", name, err)
		c.failCountInc()
		return nil, err
	}

	switch resp.StatusCode {
	case 200:
		c.markSuccess()
		return data, nil
	case 404:
		c.Warnf("Secret %v not found", name)
		return nil, SecretDeleted{}
	default:
		msg := strings.Join(strings.Split(string(data), "\n"), " ")
		c.Errorf("Bad response code getting secret %v: (status=%v, msg='%s')", name, resp.StatusCode, msg)
		c.failCountInc()
		return nil, errors.New(msg)
	}
}

// Secret returns an unmarshalled Secret struct after requesting a secret.
func (c Client) Secret(name string) (secret *Secret, err error) {
	data, err := c.RawSecret(name)
	if err != nil {
		return nil, err
	}

	secret, err = ParseSecret(data)
	if err != nil {
		c.Errorf("Error decoding retrieved secret %v: %v", name, err)
		return nil, err
	}

	return secret, nil
}

// RawSecretList returns raw JSON from requesting a listing of secrets.
func (c Client) RawSecretList() (data []byte, ok bool) {
	now := time.Now()
	t := *c.url
	t.Path = path.Join(c.url.Path, "secrets")
	resp, err := c.http().Get(t.String())
	if err != nil {
		c.Errorf("Error retrieving secrets: %v", err)
		c.failCountInc()
		return nil, false
	}
	c.Infof("GET /secrets %d %v", resp.StatusCode, time.Since(now))
	defer resp.Body.Close()

	data, err = ioutil.ReadAll(resp.Body)
	if err != nil {
		c.Errorf("Error reading response body for secrets: %v", err)
		c.failCountInc()
		return nil, false
	}

	if resp.StatusCode != 200 {
		msg := strings.Join(strings.Split(string(data), "\n"), " ")
		c.Errorf("Bad response code getting secrets: (status=%v, msg='%s')", resp.StatusCode, msg)
		c.failCountInc()
		return nil, false
	}
	c.markSuccess()
	return data, true
}

// SecretList returns a slice of unmarshalled Secret structs after requesting a listing of secrets.
func (c Client) SecretList() (secrets []Secret, ok bool) {
	data, ok := c.RawSecretList()
	if !ok {
		return nil, false
	}

	secrets, err := ParseSecretList(data)
	if err != nil {
		c.Errorf("Error decoding retrieved secrets: %v", err)
		return nil, false
	}
	return secrets, true
}

// buildClient constructs a new TLS client.
func (p httpClientParams) buildClient() (client *http.Client, err error) {
	keyPair, err := tls.LoadX509KeyPair(p.CertFile, p.KeyFile)
	if err != nil {
		return
	}

	caCert, err := ioutil.ReadFile(p.CaBundle)
	if err != nil {
		return
	}
	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(caCert)

	config := &tls.Config{
		Certificates: []tls.Certificate{keyPair},
		RootCAs:      caCertPool,
		MinVersion:   tls.VersionTLS12, // TLSv1.2 and up is required
		CipherSuites: ciphers,
	}
	config.BuildNameToCertificate()
	transport := &http.Transport{TLSClientConfig: config}
	return &http.Client{Transport: transport, Timeout: p.timeout}, nil
}
