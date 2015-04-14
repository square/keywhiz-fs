# Overview

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
  -asuser="keywhiz": Default user to own files
  -ca="cacert.crt": PEM-encoded CA certificates file
  -cert="": PEM-encoded certificate file
  -debug=false: Enable debugging output
  -group="keywhiz": Default group to own files
  -key="client.key": PEM-encoded private key file
  -ping=false: Enable startup ping to server
  -timeout=20: Timeout for communication with server in seconds
```

The `-cert` option may be omitted if the `-key` option contains both a PEM-encoded certificate and key.

# Contributing

Please contribute! And, please see CONTRIBUTING.md.

[1]: https://square.github.io/keywhiz
[2]: http://fuse.sourceforge.net/
