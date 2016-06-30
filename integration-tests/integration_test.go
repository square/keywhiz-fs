package main

import (
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"os/user"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

var server *exec.Cmd
var kwfs *exec.Cmd

func TestIntegrationTest(t *testing.T) {
	defer cleanup()

	assert := assert.New(t)

	// start fake-server
	server = exec.Command("./fake-server")
	err := server.Start()
	if err != nil {
		t.Fatal(err)
	}

	user, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}

	// start keywhiz-fs
	os.MkdirAll("./mount", 0755)
	kwfs = exec.Command("../keywhiz-fs", "--cert", "kwfs.crt", "--key",
		"kwfs.key", "--ca", "keywhiz.crt", "--debug", "--disable-mlock",
		"--asuser", user.Username, "--group", lookupGroup(user.Gid),
		"--cache-timeout", "1s", "https://localhost:8080/", "./mount")
	kwfs.Stdout = os.Stdout
	kwfs.Stderr = os.Stderr
	err = kwfs.Start()
	if err != nil {
		t.Log(err)
		t.Fail()
		return
	}

	// check that ls returns the right data
	time.Sleep(1 * time.Second)
	files, err := ioutil.ReadDir("mount")
	if err != nil {
		t.Log(err)
		t.Fail()
		return
	}
	fileNames := make([]string, len(files))
	for i, v := range files {
		fileNames[i] = v.Name()
	}
	// fileNames should contain: .clear_cache .json .running .version test_secret
	assert.Contains(fileNames, "test_secret")

	// check that cat returns the right data
	content, err := ioutil.ReadFile("mount/test_secret")
	if err != nil {
		t.Log(err)
		t.Fail()
		return
	}
	assert.Equal([]byte("hello_1"), content)

	// sleep 1
	time.Sleep(1 * time.Second)

	// check that cat returns new data
	content, err = ioutil.ReadFile("mount/test_secret")
	if err != nil {
		t.Log(err)
		t.Fail()
		return
	}
	assert.Equal([]byte("hello_2"), content)
}

func cleanup() {
	// terminate fake-server and kwfs
	err := server.Process.Kill()
	if err != nil {
		log.Fatal(err)
	}
	err = kwfs.Process.Kill()
	if err != nil {
		log.Fatal(err)
	}
	// kwfs should unmount on exit, but it doesn't?
	err = exec.Command("fusermount", "-u", "mount").Run()
	if err != nil {
		log.Fatal(err)
	}
}
