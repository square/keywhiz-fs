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
	"os"
	"time"

	"github.com/hanwen/go-fuse/fuse"
	"github.com/hanwen/go-fuse/fuse/nodefs"
	"github.com/square/keywhizfs"
	klog "github.com/square/keywhizfs/log"
	"golang.org/x/sys/unix"
)

var (
	certFile       = flag.String("cert", "", "PEM-encoded certificate file")
	keyFile        = flag.String("key", "client.key", "PEM-encoded private key file")
	caFile         = flag.String("ca", "cacert.crt", "PEM-encoded CA certificates file")
	user           = flag.String("asuser", "keywhiz", "Default user to own files")
	group          = flag.String("group", "keywhiz", "Default group to own files")
	ping           = flag.Bool("ping", false, "Enable startup ping to server")
	debug          = flag.Bool("debug", false, "Enable debugging output")
	timeoutSeconds = flag.Uint("timeout", 20, "Timeout for communication with server")
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

	serverURL, mountpoint := flag.Args()[0], flag.Args()[1]

	logConfig := klog.Config{*debug, mountpoint}
	logger = klog.New("kwfs_main", logConfig)
	defer logger.Close()

	if *certFile == "" {
		logger.Debugf("Certificate file not specified, assuming certificate also in %s", *keyFile)
		certFile = keyFile
	}

	lockMemory()

	clientTimeout := time.Duration(*timeoutSeconds) * time.Second
	freshThreshold := 200 * time.Millisecond
	backendDeadline := 500 * time.Millisecond
	maxWait := clientTimeout + backendDeadline
	timeouts := keywhizfs.Timeouts{freshThreshold, backendDeadline, maxWait}

	client := keywhizfs.NewClient(*certFile, *keyFile, *caFile, serverURL, clientTimeout, logConfig, *ping)

	ownership := keywhizfs.NewOwnership(*user, *group)
	kwfs, root, err := keywhizfs.NewKeywhizFs(&client, ownership, timeouts, logConfig)
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

	server.Serve()
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
