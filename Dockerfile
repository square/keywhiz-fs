FROM golang

MAINTAINER Diogo Monica "diogo@docker.com"

# Install sshfs and dependencies
RUN apt-get update && \
    apt-get install -y sshfs sudo make --no-install-recommends

# Add keywhiz user and group
RUN useradd -ms /bin/false keywhiz

# Copy the local repo to the expected go path
COPY . /go/src/github.com/square/keywhiz-fs

# Install keywhizfs
RUN cd /go/src/github.com/square/keywhiz-fs && \
    make keywhiz-fs && \
    cp keywhiz-fs /go/bin/keywhiz-fs

# Allows keywhiz-fs to expose its filesystems to other users besides the owner of the process
RUN echo "user_allow_other" >> /etc/fuse.conf

ENTRYPOINT ["/go/src/github.com/square/keywhiz-fs/docker.sh"]
