# Status

We plan to deprecate keywhiz-fs shortly.  While this approach has served us well, we've decided the advantages of using FUSE do not outweigh the operational difficulty.  A mostly drop-in replacement is https://github.com/square/keysync

# Overview

[![license](https://img.shields.io/badge/license-apache_2.0-red.svg?style=flat)](https://raw.githubusercontent.com/square/keywhiz-fs/master/LICENSE.txt)
[![build](https://img.shields.io/travis/square/keywhiz-fs/master.svg?style=flat)](https://travis-ci.org/square/keywhiz-fs)
[![coverage](https://coveralls.io/repos/github/square/keywhiz-fs/badge.svg?branch=master)](https://coveralls.io/github/square/keywhiz-fs?branch=master)

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

# Building

Run `make keywhiz-fs` to build a binary and `make test` to run tests.

We use [glide][3] to manage vendored dependencies.

# Running

## /etc/fuse.conf

In order to allow KeywhizFs to expose its filesystems to other users besides the owner of the process, fuse must be configured with the 'user_allow_other' option. Put the following snippet in `/etc/fuse.conf`.

```
# The following line was added for keywhiz-fs
user_allow_other
```

## fusermount setuid permissions

The `fusermount` progam is used within the go-fuse library. Generally, it is installed setuid root, with group read/execute permissions for group 'fuse'. For KeywhizFs to work, the running user must be a member of the 'fuse' group.

## `mlockall` / `CAP_IPC_LOCK` capability

To prevent secrets from ending up in swap, KeywhizFs will attempt to mlockall memory. This is not required, but is beneficial. To disable this behavior, pass `--disable-mlock` to keywhiz-fs on startup. Disabling `mlockall` means that secrets may end up in swap. 

If you want to `mlockall` memory, you will need to make sure the KeywhizFs binary has the `CAP_IPC_LOCK` capability. On Linux, set the proper capability on the KeywhizFs binary so memory can be locked without running as root. Example assumes your binary is at `/sbin/keywhiz-fs`.

```
setcap 'cap_ipc_lock=+ep' /sbin/keywhiz-fs
```

## Usage

```
usage: keywhiz-fs --key=FILE --ca=FILE [<flags>] <url> <mountpoint>

A FUSE based file-system client for Keywhiz.

Flags:
  --help                   Show context-sensitive help (also try --help-long and --help-man).
  --cert=FILE              PEM-encoded certificate file
  --key=FILE               PEM-encoded private key file
  --ca=FILE                PEM-encoded CA certificates file
  --asuser="keywhiz"       Default user to own files
  --group="keywhiz"        Default group to own files
  --debug                  Enable debugging output
  --timeout=20s            Timeout for communication with server
  --metrics-url=URL        Collect metrics and POST them periodically to the given URL (via HTTP/JSON).
  --metrics-prefix=PREFIX  Override the default metrics prefix used for reporting metrics.
  --syslog                 Send logs to syslog instead of stderr.
  --disable-mlock          Do not call mlockall on process memory.
  --version                Show application version.

Args:
  <url>         server url
  <mountpoint>  mountpoint
```

The `--cert` option may be omitted if the `--key` option contains both a PEM-encoded certificate and key.

## Running in Docker

We have included a Dockerfile so you can easily build and run KeywhizFs with all of its dependencies. To build a kewhizfs Docker image run the following command:

```
docker build --rm -t square/keywhiz-fs .
```

After building, you can run the newly built image by running:

```
docker run --device /dev/fuse:/dev/fuse --cap-add MKNOD --cap-add IPC_LOCK --cap-add SYS_ADMIN --security-opt apparmor:unconfined square/keywhiz-fs --debug --ca=/go/src/github.com/square/keywhiz-fs/fixtures/cacert.crt --key=/go/src/github.com/square/keywhiz-fs/fixtures/client.pem https://localhost:443 /secrets/kwfs
```

Note that we have to pass `--device /dev/fuse:/dev/fuse` to mount the fuse device into the container, and give `SYS_ADMIN` capabilities to the container, so it can mount fuse-fs filesystems.

This build mounts the KeywhizFs filesystem at `/secrets/kwfs/`.

# Contributing

Please contribute! And, please see CONTRIBUTING.md.

[1]: https://square.github.io/keywhiz
[2]: http://fuse.sourceforge.net/
[3]: https://glide.sh
