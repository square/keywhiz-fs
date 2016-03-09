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
	"flag"
	"fmt"
	"log"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/hanwen/go-fuse/fuse"
	"github.com/hanwen/go-fuse/fuse/nodefs"
	"github.com/rcrowley/go-metrics"
	"github.com/square/go-sq-metrics"
	klog "github.com/square/keywhiz-fs/log"
	"golang.org/x/sys/unix"
)

var (
	certFile       = flag.String("cert", "", "PEM-encoded certificate file")
	keyFile        = flag.String("key", "client.key", "PEM-encoded private key file")
	caFile         = flag.String("ca", "cacert.crt", "PEM-encoded CA certificates file")
	asuser         = flag.String("asuser", "keywhiz", "Default user to own files")
	asgroup        = flag.String("group", "keywhiz", "Default group to own files")
	ping           = flag.Bool("ping", false, "Enable startup ping to server")
	debug          = flag.Bool("debug", false, "Enable debugging output")
	timeoutSeconds = flag.Uint("timeout", 20, "Timeout for communication with server")
	metricsURL     = flag.String("metrics-url", "", "Collect metrics and POST them periodically to the given URL (via HTTP/JSON).")
	metricsPrefix  = flag.String("metrics-prefix", "", "Override the default metrics prefix used for reporting metrics.")
	logger         *klog.Logger
)

func main() {
	var Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [options] url mountpoint\n", os.Args[0])
		flag.PrintDefaults()
	}

	flag.Parse()
	if flag.NArg() != 2 {
		Usage()
		os.Exit(1)
	}

	serverURL, err := url.Parse(flag.Args()[0])
	if err != nil {
		log.Fatalf("Invalid url: %s\n", err)
		os.Exit(1)
	}
	mountpoint := flag.Args()[1]

	logConfig := klog.Config{*debug, mountpoint}
	logger = klog.New("kwfs_main", logConfig)
	defer logger.Close()

	if *certFile == "" {
		logger.Debugf("Certificate file not specified, assuming certificate also in %s", *keyFile)
		certFile = keyFile
	}

	if *metricsURL != "" && !strings.HasPrefix(*metricsURL, "http://") && !strings.HasPrefix(*metricsURL, "https://") {
		log.Fatalf("--metrics-url should start with http:// or https://")
		os.Exit(1)
	}

	lockMemory()

	clientTimeout := time.Duration(*timeoutSeconds) * time.Second
	freshThreshold := 200 * time.Millisecond
	backendDeadline := 500 * time.Millisecond
	maxWait := clientTimeout + backendDeadline
	timeouts := Timeouts{freshThreshold, backendDeadline, maxWait}

	client := NewClient(*certFile, *keyFile, *caFile, serverURL, clientTimeout, logConfig, *ping)

	ownership := NewOwnership(*asuser, *asgroup)
	kwfs, root, err := NewKeywhizFs(&client, ownership, timeouts, logConfig)
	if err != nil {
		log.Fatalf("KeywhizFs init fail: %v\n", err)
	}

	mountOptions := &fuse.MountOptions{
		AllowOther: true,
		Name:       kwfs.String(),
		Options:    []string{"default_permissions"},
	}

	// Empty Options struct avoids setting a global uid/gid override.
	conn := nodefs.NewFileSystemConnector(root, &nodefs.Options{})
	server, err := fuse.NewServer(conn.RawFS(), mountpoint, mountOptions)
	if err != nil {
		log.Fatalf("Mount fail: %v\n", err)
	}

	// Catch SIGINT and SIGKILL and exit cleanly.
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	go func() {
		sig := <-c
		logger.Warnf("Got signal %s, unmounting", sig)
		err := server.Unmount()
		if err != nil {
			logger.Warnf("Error while unmounting: %v", err)
		}
	}()

	// Setup metrics
	// Replace slashes with _ for easier aggregation
	if *metricsURL != "" {
		log.Printf("metrics enabled; reporting metrics via POST to %s", *metricsURL)

		var prefix string
		if *metricsPrefix != "" {
			prefix = *metricsPrefix
		} else {
			// By default, prefix metrics with escaped mount path
			prefix = fmt.Sprintf("keywhizfs.%s", strings.Replace(strings.Replace(mountpoint, "-", "--", -1), "/", "-", -1))
		}

		sqmetrics.NewMetrics(*metricsURL, prefix, metrics.DefaultRegistry)
	}

	server.Serve()
	logger.Infof("Exiting")
}

// Locks memory, preventing memory from being written to disk as swap
func lockMemory() {
	err := unix.Mlockall(unix.MCL_FUTURE | unix.MCL_CURRENT)
	switch err {
	case nil:
	case unix.ENOSYS:
		logger.Warnf("mlockall() not implemented on this system")
	case unix.ENOMEM:
		logger.Warnf("mlockall() failed with ENOMEM")
	default:
		log.Fatalf("Could not perform mlockall and prevent swapping memory: %v", err)
	}
}
