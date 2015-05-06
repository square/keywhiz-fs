#!/bin/bash

export REMOTE_URL="https://localhost:4443"
export MOUNTPOINT="/secrets/keywhiz_mount/"
export KEYWHIZ_USER="keywhiz"

# Create mount
mkdir -p $MOUNTPOINT
chown $KEYWHIZ_USER:$KEYWHIZ_USER $MOUNTPOINT
chown $KEYWHIZ_USER /dev/fuse
chmod 640 /dev/fuse

# This doesn't work with aufs. Need overlayFS to support it.
setcap 'cap_ipc_lock=+ep' /go/bin/keywhizfs

sudo -u $KEYWHIZ_USER /go/bin/keywhizfs -asuser=$KEYWHIZ_USER -group=$KEYWHIZ_USER -debug=true -ca=/go/src/github.com/square/keywhiz-fs/fixtures/cacert.crt -key=/go/src/github.com/square/keywhiz-fs/fixtures/client.pem $REMOTE_URL $MOUNTPOINT

