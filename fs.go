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
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/hanwen/go-fuse/fuse"
	"github.com/hanwen/go-fuse/fuse/nodefs"
	"github.com/hanwen/go-fuse/fuse/pathfs"
	"github.com/square/go-sq-metrics"
	"github.com/square/keywhiz-fs/log"
	"golang.org/x/sys/unix"
)

const (
	fsVersion  = "2.0"
	fuseEISDIR = fuse.Status(unix.EISDIR)
)

// Initialized via ldflags
var (
	buildRevision = "unknown"
	buildTime     = "0"
	buildMachine  = "unknown"
)

// StatusInfo contains debug info accessible via `.json/status`.
type StatusInfo struct {
	BuildRevision  string           `json:"build_revision"`
	BuildMachine   string           `json:"build_machine"`
	BuildTime      time.Time        `json:"build_time"`
	StartTime      time.Time        `json:"start_time"`
	RuntimeVersion string           `json:"runtime_version"`
	ServerURL      string           `json:"server_url"`
	ClientParams   httpClientParams `json:"client_params"`
}

// KeywhizFs is the central struct for dispatching filesystem operations.
type KeywhizFs struct {
	pathfs.FileSystem
	*log.Logger
	Client    *Client
	Cache     *Cache
	Metrics   *sqmetrics.SquareMetrics
	StartTime time.Time
	Ownership Ownership
}

func (kwfs KeywhizFs) statusJSON() []byte {
	// Convert buildTime (seconds since epoch) into an actual time.Time object,
	// makes for nicer JSON marshalling (and matches mount time format).
	seconds, err := strconv.ParseInt(buildTime, 10, 64)
	panicOnError(err)

	status, err := json.Marshal(
		StatusInfo{
			BuildRevision:  buildRevision,
			BuildMachine:   buildMachine,
			BuildTime:      time.Unix(seconds, 0),
			StartTime:      kwfs.StartTime,
			RuntimeVersion: runtime.Version(),
			ServerURL:      kwfs.Client.url.String(),
			ClientParams:   kwfs.Client.params,
		})
	panicOnError(err)
	return status
}

func (kwfs KeywhizFs) metricsJSON() []byte {
	if kwfs.Metrics != nil {
		metrics := kwfs.Metrics.SerializeMetrics()
		data, err := json.Marshal(metrics)
		if err == nil {
			return data
		}
		kwfs.Warnf("Error serializing metrics: %v", err)
	}
	return []byte{}
}

// NewKeywhizFs readies a KeywhizFs struct and its parent filesystem objects.
func NewKeywhizFs(client *Client, ownership Ownership, timeouts Timeouts, metrics *sqmetrics.SquareMetrics, logConfig log.Config) (kwfs *KeywhizFs, root nodefs.Node, err error) {
	logger := log.New("kwfs", logConfig)
	cache := NewCache(client, timeouts, logConfig, nil)

	defaultfs := pathfs.NewDefaultFileSystem()            // Returns ENOSYS by default
	readonlyfs := pathfs.NewReadonlyFileSystem(defaultfs) // R/W calls return EPERM

	kwfs = &KeywhizFs{readonlyfs, logger, client, cache, metrics, time.Now(), ownership}
	nfs := pathfs.NewPathNodeFs(kwfs, nil)
	nfs.SetDebug(logConfig.Debug)
	return kwfs, nfs.Root(), nil
}

// GetAttr is a FUSE function which tells FUSE which files and directories exist.
//
// name is empty when getting information on the base directory
func (kwfs KeywhizFs) GetAttr(name string, context *fuse.Context) (*fuse.Attr, fuse.Status) {
	kwfs.Debugf("GetAttr called with '%v'", name)

	var attr *fuse.Attr
	switch {
	case name == "": // Base directory
		attr = kwfs.directoryAttr(1, 0755) // Writability necessary for .clear_cache
	case name == ".version":
		size := uint64(len(fsVersion))
		attr = kwfs.fileAttr(size, 0444)
	case name == ".clear_cache":
		attr = kwfs.fileAttr(0, 0440)
	case name == ".running":
		size := uint64(len(running()))
		attr = kwfs.fileAttr(size, 0444)
	case name == ".json":
		attr = kwfs.directoryAttr(1, 0700)
	case name == ".json/status":
		size := uint64(len(kwfs.statusJSON()))
		attr = kwfs.fileAttr(size, 0444)
	case name == ".json/metrics":
		size := uint64(len(kwfs.metricsJSON()))
		attr = kwfs.fileAttr(size, 0444)
	case name == ".json/secret":
		attr = kwfs.directoryAttr(0, 0700)
	case name == ".json/secrets":
		data, ok := kwfs.Client.RawSecretList()
		if ok {
			size := uint64(len(data))
			attr = kwfs.fileAttr(size, 0400)
		}
	case strings.HasPrefix(name, ".json/secret/"):
		name = name[len(".json/secret/"):]
		data, err := kwfs.Client.RawSecret(name)
		if err == nil {
			size := uint64(len(data))
			attr = kwfs.fileAttr(size, 0400)
		}
	default:
		secret, ok := kwfs.Cache.Secret(name)
		if ok {
			attr = kwfs.secretAttr(secret)
		}
	}

	if attr != nil {
		return attr, fuse.OK
	}
	return nil, fuse.ENOENT
}

// Open is a FUSE function where an in-memory open file struct is constructed.
func (kwfs KeywhizFs) Open(name string, flags uint32, context *fuse.Context) (nodefs.File, fuse.Status) {
	kwfs.Debugf("Open called with '%v'", name)

	var file nodefs.File
	var mode uint32
	switch {
	case name == "", name == ".json", name == ".json/secret":
		return nil, fuseEISDIR
	case name == ".version":
		file = nodefs.NewDataFile([]byte(fsVersion))
		mode = 0440
	case name == ".json/status":
		file = nodefs.NewDataFile(kwfs.statusJSON())
		mode = 0444
	case name == ".json/metrics":
		file = nodefs.NewDataFile(kwfs.metricsJSON())
		mode = 0444
	case name == ".clear_cache":
		file = nodefs.NewDevNullFile()
		mode = 0440
	case name == ".running":
		file = nodefs.NewDataFile(running())
		mode = 0444
	case name == ".json/secrets":
		data, ok := kwfs.Client.RawSecretList()
		if ok {
			file = nodefs.NewDataFile(data)
			mode = 0400
		}
	case strings.HasPrefix(name, ".json/secret/"):
		name = name[len(".json/secret/"):]
		data, err := kwfs.Client.RawSecret(name)
		if err == nil {
			file = nodefs.NewDataFile(data)
			mode = 0400
			kwfs.Infof("Access to %s by uid %d, with gid %d", name, context.Uid, context.Gid)
		}
	default:
		secret, ok := kwfs.Cache.Secret(name)
		if ok {
			file = nodefs.NewDataFile(secret.Content)
			mode = secret.ModeValue()
			kwfs.Infof("Access to %s by uid %d, with gid %d", name, context.Uid, context.Gid)
		}
	}

	if file != nil {
		file = NewModeFile(nodefs.NewReadOnlyFile(file), mode)
		return file, fuse.OK
	}
	return nil, fuse.ENOENT
}

// OpenDir is a FUSE function called when performing a directory listing.
func (kwfs KeywhizFs) OpenDir(name string, context *fuse.Context) (stream []fuse.DirEntry, code fuse.Status) {
	kwfs.Debugf("OpenDir called with '%v'", name)

	var entries []fuse.DirEntry
	switch name {
	case "": // Base directory
		entries = kwfs.secretsDirListing(
			fuse.DirEntry{Name: ".clear_cache", Mode: fuse.S_IFREG},
			fuse.DirEntry{Name: ".json", Mode: fuse.S_IFDIR},
			fuse.DirEntry{Name: ".running", Mode: fuse.S_IFREG},
			fuse.DirEntry{Name: ".version", Mode: fuse.S_IFREG})
	case ".json":
		entries = []fuse.DirEntry{
			{Name: "metrics", Mode: fuse.S_IFREG},
			{Name: "secret", Mode: fuse.S_IFDIR},
			{Name: "secrets", Mode: fuse.S_IFREG},
			{Name: "status", Mode: fuse.S_IFREG},
		}
	case ".json/secret":
		entries = kwfs.secretsDirListing()
	}

	if len(entries) == 0 {
		return entries, fuse.ENOENT
	}
	return entries, fuse.OK
}

// Unlink is a FUSE function called when an object is deleted.
func (kwfs KeywhizFs) Unlink(name string, context *fuse.Context) fuse.Status {
	kwfs.Debugf("Unlink called with '%v'", name)
	if name == ".clear_cache" {
		kwfs.Cache.Clear()
		return fuse.OK
	}
	return fuse.EACCES
}

// StatFs is a FUSE function called to provide information about the filesystem
// We return zeros, which makes "df" think this is a dummy fs, which it is.
func (kwfs KeywhizFs) StatFs(name string) *fuse.StatfsOut {
	kwfs.Debugf("StatFs called with '%v'", name)
	return &fuse.StatfsOut{}
}

// secretsDirListing produces directory entries containing all secret files. Extra entries passed
// to this function are included.
func (kwfs KeywhizFs) secretsDirListing(extraEntries ...fuse.DirEntry) []fuse.DirEntry {
	secrets := kwfs.Cache.SecretList()
	entries := make([]fuse.DirEntry, 0, len(secrets)+len(extraEntries))
	for _, s := range secrets {
		entries = append(entries, fuse.DirEntry{Name: s.Name, Mode: fuse.S_IFREG})
	}
	entries = append(entries, extraEntries...)
	return entries
}

// secretAttr constructs a fuse.Attr based on a given Secret.
func (kwfs KeywhizFs) secretAttr(s *Secret) *fuse.Attr {
	created := uint64(s.CreatedAt.Unix())
	attr := &fuse.Attr{
		Size: s.Length,
		// The resolution for nsec time (uint32) is too small.
		Atime: created,
		Mtime: created,
		Ctime: created,
		Mode:  s.ModeValue(),
	}

	attr.Uid = kwfs.Ownership.Uid
	attr.Gid = kwfs.Ownership.Gid

	if s.Owner != "" {
		attr.Uid = lookupUid(s.Owner)
	}
	if s.Group != "" {
		attr.Gid = lookupGid(s.Group)
	}
	return attr
}

// fileAttr constructs a generic file fuse.Attr with the given parameters.
func (kwfs KeywhizFs) fileAttr(size uint64, mode uint32) *fuse.Attr {
	created := uint64(kwfs.StartTime.Unix())
	attr := fuse.Attr{
		Size:  size,
		Atime: created,
		Mtime: created,
		Ctime: created,
		Mode:  fuse.S_IFREG | mode,
	}
	attr.Uid = kwfs.Ownership.Uid
	attr.Gid = kwfs.Ownership.Gid
	return &attr
}

// directoryAttr constructs a generic directory fuse.Attr with the given parameters.
func (kwfs KeywhizFs) directoryAttr(subdirCount, mode uint32) *fuse.Attr {
	// 4K is typically the minimum size of inode storage for a directory.
	const directoryInodeSize = 4096
	created := uint64(kwfs.StartTime.Unix())

	attr := fuse.Attr{
		Size:  directoryInodeSize,
		Atime: created,
		Mtime: created,
		Ctime: created,
		Mode:  fuse.S_IFDIR | mode,
		Nlink: 2 + subdirCount, // '.', '..', and any other subdirectories
	}
	attr.Uid = kwfs.Ownership.Uid
	attr.Gid = kwfs.Ownership.Gid
	return &attr
}

// NewModeFile wraps a File so all GetAttr operations return a
// constant Mode
func NewModeFile(f nodefs.File, mode uint32) nodefs.File {
	return &modeFile{File: f, mode: mode}
}

type modeFile struct {
	nodefs.File
	mode uint32
}

func (f *modeFile) InnerFile() nodefs.File {
	return f.File
}

func (f *modeFile) String() string {
	return fmt.Sprintf("modeFile(%s)", f.File.String())
}

func (f *modeFile) GetAttr(out *fuse.Attr) fuse.Status {
	status := f.File.GetAttr(out)
	out.Mode = f.mode
	return status
}

// running provides a formatted string with the current process ID.
func running() []byte {
	return []byte(fmt.Sprintf("pid=%d", os.Getpid()))
}

func (kwfs KeywhizFs) String() string {
	return "keywhiz-fs"
}
