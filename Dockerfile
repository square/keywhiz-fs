FROM golang

MAINTAINER Diogo Monica "diogo@docker.com"

# Install sshfs and dependencies
RUN apt-get update && apt-get install -y \
    sshfs \
    sudo \
    --no-install-recommends

# Add keywhiz user and group
RUN useradd -ms /bin/false keywhiz

# Copy the local repo to the expected go path
COPY . /go/src/github.com/square/keywhiz-fs

# Install keywhizfs
RUN go get github.com/square/keywhiz-fs/keywhizfs

# Allows keywhiz-fs to expose its filesystems to other users besides the owner of the process
RUN echo "user_allow_other" >> /etc/fuse.conf

ENTRYPOINT ["/go/src/github.com/square/keywhiz-fs/docker_kwfs.sh"]
