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
	"log"
	"os"
	"os/signal"
	"strings"
	"time"

	"gopkg.in/alecthomas/kingpin.v2"

	"github.com/hanwen/go-fuse/fuse"
	"github.com/hanwen/go-fuse/fuse/nodefs"
	"github.com/rcrowley/go-metrics"
	"github.com/square/go-sq-metrics"
	klog "github.com/square/keywhiz-fs/log"
	"golang.org/x/sys/unix"
)

var (
	app = kingpin.New("keywhiz-fs", "A FUSE based file-system client for Keywhiz.")

	certFile      = app.Flag("cert", "PEM-encoded certificate file").PlaceHolder("FILE").Default("").String()
	keyFile       = app.Flag("key", "PEM-encoded private key file").PlaceHolder("FILE").Required().String()
	caFile        = app.Flag("ca", "PEM-encoded CA certificates file").PlaceHolder("FILE").Required().String()
	asuser        = app.Flag("asuser", "Default user to own files").Default("keywhiz").String()
	asgroup       = app.Flag("group", "Default group to own files").Default("keywhiz").String()
	debug         = app.Flag("debug", "Enable debugging output").Default("false").Bool()
	timeout       = app.Flag("timeout", "Timeout for communication with server").Default("20s").Duration()
	cacheTimeout  = app.Flag("cache-timeout", "Timeout for cache eviction. Useful for testing.").Default("1h").Duration()
	metricsURL    = app.Flag("metrics-url", "Collect metrics and POST them periodically to the given URL (via HTTP/JSON).").PlaceHolder("URL").String()
	metricsPrefix = app.Flag("metrics-prefix", "Override the default metrics prefix used for reporting metrics.").PlaceHolder("PREFIX").String()
	syslog        = app.Flag("syslog", "Send logs to syslog instead of stderr.").Default("false").Bool()
	disableMlock  = app.Flag("disable-mlock", "Do not call mlockall on process memory.").Default("false").Bool()
	serverURL     = app.Arg("url", "server url").Required().URL()
	mountpoint    = app.Arg("mountpoint", "mountpoint").Required().String()
	logger        *klog.Logger
)

func main() {
	app.Version(fmt.Sprintf("rev %s-%s on \"%s\"", buildRevision, buildTime, buildMachine))
	kingpin.MustParse(app.Parse(os.Args[1:]))

	logConfig := klog.Config{Debug: *debug, Mountpoint: *mountpoint, Syslog: *syslog}
	logger = klog.New("kwfs_main", logConfig)
	defer logger.Close()

	if *certFile == "" {
		logger.Debugf("Certificate file not specified, assuming certificate also in %s", *keyFile)
		certFile = keyFile
	}

	metricsHandle := setupMetrics(metricsURL, metricsPrefix, *mountpoint)

	if !*disableMlock {
		lockMemory()
	}

	// TODO: move time limit settings to config file?
	// TODO: or at least make it consistent? some are set here, some are set above with app.Flag()
	freshThreshold := *cacheTimeout
	backendDeadline := 5 * time.Second
	maxWait := *timeout + backendDeadline
	delayDeletion := 1 * time.Hour
	timeouts := Timeouts{freshThreshold, backendDeadline, maxWait, delayDeletion}

	client := NewClient(*certFile, *keyFile, *caFile, *serverURL, *timeout, logConfig)

	ownership := NewOwnership(*asuser, *asgroup)
	kwfs, root, err := NewKeywhizFs(&client, ownership, timeouts, metricsHandle, logConfig)
	if err != nil {
		log.Fatalf("KeywhizFs init fail: %v\n", err)
	}
	kwfs.Cache.Warmup()

	mountOptions := &fuse.MountOptions{
		AllowOther: true,
		Name:       kwfs.String(),
		Options:    []string{"default_permissions"},
	}

	// Empty Options struct avoids setting a global uid/gid override.
	conn := nodefs.NewFileSystemConnector(root, &nodefs.Options{})
	server, err := fuse.NewServer(conn.RawFS(), *mountpoint, mountOptions)
	if err != nil {
		log.Fatalf("Mount fail: %v\n", err)
	}

	// Catch SIGINT and exit cleanly.
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	go func() {
		for {
			sig := <-c
			logger.Warnf("Got signal %s, unmounting", sig)
			err := server.Unmount()
			if err != nil {
				logger.Warnf("Error while unmounting: %v", err)
			}
		}
	}()

	server.Serve()
	logger.Infof("Exiting")
}

// Setup metrics
func setupMetrics(metricsURL *string, metricsPrefix *string, mountpoint string) *sqmetrics.SquareMetrics {
	if *metricsURL != "" {
		if !strings.HasPrefix(*metricsURL, "http://") && !strings.HasPrefix(*metricsURL, "https://") {
			log.Fatalf("--metrics-url should start with http:// or https://")
			os.Exit(1)
		}
		log.Printf("metrics enabled; reporting metrics via POST to %s", *metricsURL)
	}

	var prefix string
	if *metricsPrefix != "" {
		prefix = *metricsPrefix
	} else {
		// By default, prefix metrics with escaped mount path. Replace slashes with - for easier aggregation
		prefix = fmt.Sprintf("keywhizfs.%s", strings.Replace(strings.Replace(mountpoint, "-", "--", -1), "/", "-", -1))
	}

	return sqmetrics.NewMetrics(*metricsURL, prefix, (30 * time.Second), metrics.DefaultRegistry)
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

// Helper function to panic on error
func panicOnError(err error) {
	if err != nil {
		logger.Errorf("panic: %v", err)
		panic(err)
	}
}
