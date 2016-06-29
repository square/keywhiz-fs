package main

import (
  "os/exec"
  "log"
  "io/ioutil"
  "testing"
  "time"

  "github.com/stretchr/testify/assert"
)

var server *exec.Cmd
var kwfs *exec.Cmd

func TestIntegrationTest(t *testing.T) {
  defer cleanup()

  assert := assert.New(t)

  // start fake_server
  server = exec.Command("./fake_server")
  err := server.Start()
  if err != nil {
    t.Log(err)
    t.Fail()
    return
	}
  t.Log("fake_server started")

  // start keywhiz-fs
  kwfs = exec.Command("../keywhiz-fs", "--cert", "kwfs.crt", "--key",
    "kwfs.key", "--ca", "keywhiz.crt", "--asuser", "alok", "--group",
    "staff", "--debug", "--disable-mlock", "https://localhost:8080/", "mount/")
  err = kwfs.Start()
  if err != nil {
    t.Log(err)
    t.Fail()
    return
  }
  t.Log("kwfs started")

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
  // bug in kwfs?
  assert.Equal([]byte("hello_1\x00\x00\x00\x00\x00"), content)

  // sleep 1
  time.Sleep(1 * time.Second)

  // check that cat returns new data
  content, err = ioutil.ReadFile("mount/test_secret")
  if err != nil {
    t.Log(err)
    t.Fail()
    return
  }
  assert.Equal([]byte("hello_2\x00\x00\x00\x00\x00"), content)
}

func cleanup() {
  // terminate fake_server and kwfs
  err := server.Process.Kill()
  if err != nil {
    log.Fatal(err)
  }
  err = kwfs.Process.Kill()
  if err != nil {
    log.Fatal(err)
  }
  // kwfs should unmount on exit, but it doesn't?
  err = exec.Command("/sbin/umount", "mount").Run()
  if err != nil {
    log.Fatal(err)
  }
}
