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

package keywhizfs

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/hanwen/go-fuse/fuse"
	"github.com/hanwen/go-fuse/fuse/nodefs"
	"github.com/hanwen/go-fuse/fuse/pathfs"
	"github.com/square/keywhiz-fs/log"
	"golang.org/x/sys/unix"
)

const (
	VERSION = "2.0"
	EISDIR  = fuse.Status(unix.EISDIR)
)

// KeywhizFs is the central struct for dispatching filesystem operations.
type KeywhizFs struct {
	pathfs.FileSystem
	*log.Logger
	Client    *Client
	Cache     *Cache
	StartTime time.Time
	Ownership Ownership
}

// NewKeywhizFs readies a KeywhizFs struct and its parent filesystem objects.
func NewKeywhizFs(client *Client, ownership Ownership, timeouts Timeouts, logConfig log.Config) (kwfs *KeywhizFs, root nodefs.Node, err error) {
	logger := log.New("kwfs", logConfig)
	cache := NewCache(client, timeouts, logConfig)

	defaultfs := pathfs.NewDefaultFileSystem()            // Returns ENOSYS by default
	readonlyfs := pathfs.NewReadonlyFileSystem(defaultfs) // R/W calls return EPERM

	kwfs = &KeywhizFs{readonlyfs, logger, client, cache, time.Now(), ownership}
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
		size := uint64(len(VERSION))
		attr = kwfs.fileAttr(size, 0444)
	case name == ".clear_cache":
		attr = kwfs.fileAttr(0, 0440)
	case name == ".running":
		size := uint64(len(running()))
		attr = kwfs.fileAttr(size, 0444)
	case name == ".json":
		attr = kwfs.directoryAttr(1, 0700)
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
		data, ok := kwfs.Client.RawSecret(name)
		if ok {
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
	switch {
	case name == "", name == ".json", name == ".json/secret":
		return nil, EISDIR
	case name == ".version":
		file = nodefs.NewDataFile([]byte(VERSION))
	case name == ".clear_cache":
		file = nodefs.NewDevNullFile()
	case name == ".running":
		file = nodefs.NewDataFile(running())
	case name == ".json/secrets":
		data, ok := kwfs.Client.RawSecretList()
		if ok {
			file = nodefs.NewDataFile(data)
		}
	case strings.HasPrefix(name, ".json/secret/"):
		name = name[len(".json/secret/"):]
		data, ok := kwfs.Client.RawSecret(name)
		if ok {
			file = nodefs.NewDataFile(data)
			kwfs.Infof("Access to %s by uid %d, with gid %d", name, context.Uid, context.Gid)
		}
	default:
		secret, ok := kwfs.Cache.Secret(name)
		if ok {
			file = nodefs.NewDataFile(secret.Content)
			kwfs.Infof("Access to %s by uid %d, with gid %d", name, context.Uid, context.Gid)
		}
	}

	if file != nil {
		file = nodefs.NewReadOnlyFile(file)
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
			fuse.DirEntry{Name: "secret", Mode: fuse.S_IFDIR},
			fuse.DirEntry{Name: "secrets", Mode: fuse.S_IFREG},
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

// running provides a formatted string with the current process ID.
func running() []byte {
	return []byte(fmt.Sprintf("pid=%d", os.Getpid()))
}

func (kwfs KeywhizFs) String() string {
	return "keywhiz-fs"
}
