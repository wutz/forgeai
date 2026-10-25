#!/bin/sh
# Entrypoint of the sshd sidecar. The Secret with the host key and the platform
# authorized key is mounted read-only, and Kubernetes mounts a Secret volume's
# directory world-writable, which sshd refuses under StrictModes. So the keys are
# copied into a directory of their own, root-owned and closed to the world, and
# sshd is pointed there by the arguments of pkg/sshgateway.SSHDArgs.
set -eu

src=/etc/forgeai-sshd
run=/run/forgeai-sshd

# StrictModes only refuses a directory anyone but its owner may write, so 0755
# is accepted. sshd reads the authorized keys as the login user, which must be
# able to enter the directory and read the file; both hold public material.
mkdir -p "$run"
chown root:root "$run"
chmod 0755 "$run"

# Copy through the symlinks of the Secret volume, with modes only their owner
# may change: sshd refuses a key it finds writable by anyone else.
cp "$src/ssh_host_ed25519_key" "$run/ssh_host_ed25519_key"
cp "$src/authorized_keys" "$run/authorized_keys"
chown root:root "$run/ssh_host_ed25519_key" "$run/authorized_keys"
chmod 0400 "$run/ssh_host_ed25519_key"
chmod 0444 "$run/authorized_keys"

# sshd keeps its privilege separation directory here.
mkdir -p /run/sshd
chmod 0755 /run/sshd

exec /usr/sbin/sshd "$@"
