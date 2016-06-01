#!/bin/bash

export MOUNTPOINT="/secrets/kwfs/"
export KEYWHIZ_USER="keywhiz"

# Create mount
mkdir -p $MOUNTPOINT
chown $KEYWHIZ_USER:$KEYWHIZ_USER $MOUNTPOINT
chown $KEYWHIZ_USER /dev/fuse
chmod 640 /dev/fuse

sudo -u $KEYWHIZ_USER /go/bin/keywhiz-fs --disable-mlock --asuser=$KEYWHIZ_USER --group=$KEYWHIZ_USER "$@"
