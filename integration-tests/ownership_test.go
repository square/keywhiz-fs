package main

import (
	"bufio"
	"os"
	"strconv"
	"strings"
)

var groupFile = "/etc/group"

func lookupGroup(gidRaw string) string {
	gid, err := strconv.ParseUint(gidRaw, 10 /* base */, 32 /* bits */)
	if err != nil {
		panic(err)
	}

	file, err := os.Open(groupFile)
	if err != nil {
		panic(err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		entry := strings.Split(scanner.Text(), ":")
		if len(entry) < 3 {
			continue
		}
		id, err := strconv.ParseUint(entry[2], 10 /* base */, 32 /* bits */)
		if err != nil {
			continue
		}
		if id == gid {
			return entry[0]
		}
	}

	panic("error resolving group")
}
