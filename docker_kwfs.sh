#!/bin/bash

export MOUNTPOINT="/secrets/kwfs/"
export KEYWHIZ_USER="keywhiz"

# Create mount
mkdir -p $MOUNTPOINT
chown $KEYWHIZ_USER:$KEYWHIZ_USER $MOUNTPOINT
chown $KEYWHIZ_USER /dev/fuse
chmod 640 /dev/fuse

# This doesn't work with aufs. Need overlayFS to support it.
setcap 'cap_ipc_lock=+ep' /go/bin/keywhizfs

sudo -u $KEYWHIZ_USER /go/bin/keywhizfs -asuser=$KEYWHIZ_USER -group=$KEYWHIZ_USER $@
