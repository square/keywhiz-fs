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
	"bufio"
	"errors"
	"log"
	"os"
	"os/user"
	"strconv"
	"strings"
)

var groupFile = "/etc/group"

// Ownership indicates the default ownership of filesystem entries.
type Ownership struct {
	Uid uint32
	Gid uint32
}

// NewOwnership initializes default file ownership struct.
func NewOwnership(username, groupname string) Ownership {
	return Ownership{
		Uid: lookupUid(username),
		Gid: lookupGid(groupname),
	}
}

// lookupUid resolves a username to a numeric id. Current euid is returned on failure.
func lookupUid(username string) uint32 {
	u, err := user.Lookup(username)
	if err != nil {
		log.Printf("Error resolving uid for %v: %v\n", username, err)
		return uint32(os.Geteuid())
	}

	uid, err := strconv.ParseUint(u.Uid, 10 /* base */, 32 /* bits */)
	if err != nil {
		log.Printf("Error resolving uid for %v: %v\n", username, err)
		return uint32(os.Geteuid())
	}

	return uint32(uid)
}

// lookupGid resolves a groupname to a numeric id. Current egid is returned on failure.
func lookupGid(groupname string) uint32 {
	file, err := os.Open(groupFile)
	if err != nil {
		log.Printf("Error resolving gid for %v: %v\n", groupname, err)
		return uint32(os.Getegid())
	}
	defer file.Close()

	gid, err := lookupGidInFile(groupname, file)
	if err != nil {
		log.Printf("Error resolving gid for %v: %v\n", groupname, err)
		return uint32(os.Getegid())
	}

	return gid
}

func lookupGidInFile(groupname string, file *os.File) (uint32, error) {
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		entry := strings.Split(scanner.Text(), ":")
		if entry[0] == groupname && len(entry) >= 3 {
			gid, err := strconv.ParseUint(entry[2], 10 /* base */, 32 /* bits */)
			if err != nil {
				return 0, err
			}
			return uint32(gid), nil
		}
	}

	return 0, errors.New("no such group")
}
