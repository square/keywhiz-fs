# Overview

[![license](https://img.shields.io/badge/license-apache_2.0-red.svg?style=flat)](https://raw.githubusercontent.com/square/keywhiz-fs/master/LICENSE)
[![build](https://img.shields.io/travis/square/keywhiz-fs/master.svg?style=flat)](https://travis-ci.org/square/keywhiz-fs)

KeywhizFs is a client for [Keywhiz][1] which represents accessible secrets as a userland filesystem (using [FUSE][2]). This client will mount a directory which contains files for each secret that is accessible.

Exposing secrets as a filesystem has many benefits.

1. Consumers of secrets require no special libraries or code.
2. Unix user and group permissions restrict which processes can read a secret.

Transparently, authentication is performed with a Keywhiz server using mutually-authenticated TLS. A client certificate, trusted by Keywhiz, is required and used to authenticate KeywhizFs. Refer to the [Keywhiz documentation][1] for generating and managing client access.

# Directory structure

KeywhizFs will display all secrets under the top level directory of the mountpoint. Secrets may not begin with the '.' character, which is reserved for special control "files".

## Control files

- `.running`
 - This "file" contains the PID of the owner process.
- `.clear_cache`
 - Deleting this empty "file" will cause the internal cache of KeywhizFs to be cleared. This should seldom be necessary in practice but has been useful at times.
- `.json/`
 - This sub-directory mimics the REST API of Keywhiz. Reading files will directly communicate with the backend server and display the unparsed JSON response.

# Filesystem permissions

# Building

Run `go build keywhizfs/main.go`.

# Testing

Simply run `go test ./...`.

# Running

## /etc/fuse.conf

In order to allow keywhiz-fs to expose its filesystems to other users besides the owner of the process, fuse must be configured with the 'user_allow_other' option. Put the following snippet in `/etc/fuse.conf`.

```
# The following line was added by keywhiz-fs
user_allow_other
```

## fusermount setuid permissions

The `fusermount` progam is used within the go-fuse library. Generally, it is installed setuid root, with group read/execute permissions for group 'fuse'. For KeywhizFs to work, the running user must be a member of the 'fuse' group.

## `CAP_IPC_LOCK` capability

To prevent secrets from ending up in swap, KeywhizFs will attempt to mlockall memory. This is not required, but is beneficial. On Linux, set the proper capability on the KeywhizFs binary so memory can be locked without running as root. Example assumes your binary is at `/sbin/keywhiz-fs`.

```
setcap 'cap_ipc_lock=+ep' /sbin/keywhiz-fs
```

## Usage

```
Usage: ./keywhiz-fs [options] url mountpoint
Options:
  --asuser="keywhiz": Default user to own files
  --ca="cacert.crt": PEM-encoded CA certificates file
  --cert="": PEM-encoded certificate file
  --debug=false: Enable debugging output
  --group="keywhiz": Default group to own files
  --key="client.key": PEM-encoded private key file
  --ping=false: Enable startup ping to server
  --timeout=20: Timeout for communication with server in seconds
```

The `-cert` option may be omitted if the `-key` option contains both a PEM-encoded certificate and key.

## Running in Docker

We have included a Dockerfile so you can easily build and run keywhiz-fs with all of its dependencies. To build a kewhizfs Docker image run the following command:

```
docker build --rm -t keywhizfs .
```

After building, you can run the newly built image by running:

```
docker run --device /dev/fuse:/dev/fuse --cap-add=IPC_LOCK --cap-add=SYS_ADMIN keywhizfs --debug=true --ca=/go/src/github.com/square/keywhiz-fs/fixtures/cacert.crt --key=/go/src/github.com/square/keywhiz-fs/fixtures/client.pem https://localhost:443 /secrets/kwfs
```

Note that we have to pass `--device /dev/fuse:/dev/fuse` to mount the fuse device into the container, and give `IPC_LOCK` and `SYS_ADMIN` capabilities to the container, so it can set `cap_ipc_lock` on the keywhiz-fs binary, and so it can mount fuse-fs filesystems, respectively.

This build mounts the fuseFS at `/secrets/kwfs/`.

### Caveats

Currently keywhiz-fs is not a [12 factor](http://12factor.net/) application, and it does not send unbuffered logs to stdout. It currently expects there to be a syslog server being ran locally.

We can follow [this tutorial](https://jpetazzo.github.io/2014/08/24/syslog-docker/) and run syslogd in a separate container, allowing us to use an external container as our syslogd. After that, we can use the external container as our keywhiz-fs syslog server:

```
docker run -v /tmp/syslogdev/log:/dev/log --device /dev/fuse:/dev/fuse --cap-add=IPC_LOCK --cap-add=SYS_ADMIN keywhizfs --debug=true --ca=/go/src/github.com/square/keywhiz-fs/fixtures/cacert.crt --key=/go/src/github.com/square/keywhiz-fs/fixtures/client.pem https://localhost:443 /secrets/kwfs
```

Additionally, if you see the following error: `mlockall() failed with ENOMEM`, you are probably running your docker deamon with aufs, which does not support capability extensions, making `setcap 'cap_ipc_lock=+ep' /go/bin/keywhizfs` fail. You should use overlayFS instead.

# Contributing

Please contribute! And, please see CONTRIBUTING.md.

[1]: https://square.github.io/keywhiz
[2]: http://fuse.sourceforge.net/
