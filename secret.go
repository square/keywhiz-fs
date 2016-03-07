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
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

// ParseSecret deserializes raw JSON into a Secret struct.
func ParseSecret(data []byte) (s *Secret, err error) {
	if err = json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("Fail to deserialize JSON Secret: %v", err)
	}
	return
}

// ParseSecretList deserializes raw JSON into a list of Secret structs.
func ParseSecretList(data []byte) (secrets []Secret, err error) {
	if err = json.Unmarshal(data, &secrets); err != nil {
		return nil, fmt.Errorf("Fail to deserialize JSON []Secret: %v", err)
	}
	return
}

// Secret represents data returned after processing a server request.
//
// json tags after fields indicate to json decoder the key name in JSON
type Secret struct {
	Name        string
	Content     content   `json:"secret"`
	Length      uint64    `json:"secretLength"`
	CreatedAt   time.Time `json:"creationDate"`
	IsVersioned bool
	Mode        string
	Owner       string
	Group       string
}

// ModeValue function helps by converting a textual mode to the expected value for fuse.
func (s Secret) ModeValue() uint32 {
	mode := s.Mode
	if mode == "" {
		mode = "0440"
	}
	modeValue, err := strconv.ParseUint(mode, 8 /* base */, 16 /* bits */)
	if err != nil {
		log.Printf("Unable to convert secret mode (%v) to octal, using '0440': %v\n", mode, err)
		modeValue = 0440
	}
	return uint32(modeValue | unix.S_IFREG)
}

// content is a helper type used to convert base64-encoded data from the server.
type content []byte

func (c *content) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("secret should be a string, got '%s' (%v)", data, err)
	}

	// Go's base64 requires padding to be present so we add it if necessary.
	if m := len(s) % 4; m != 0 {
		s += strings.Repeat("=", 4-m)
	}

	decoded, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return fmt.Errorf("secret not valid base64, got '%+v' (%v)", s, err)
	}

	*c = decoded
	return nil
}
