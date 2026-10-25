# sshd sidecar of an SSH sandbox. The sidecar runs entrypoint.sh, which stages the
# keys of the Secret mounted at /etc/forgeai-sshd and execs /usr/sbin/sshd with the
# options of pkg/sshgateway (SSHDArgs): only public-key logins as the sandbox user.
FROM debian:bookworm-slim

RUN apt-get update \
 && apt-get install -y --no-install-recommends openssh-server \
 && rm -rf /var/lib/apt/lists/*

# The one login account: SSHDArgs allows no other user. useradd locks the
# password by default, and sshd refuses a locked account before it looks at the
# key, so the password field is set to "*": no password can ever match it, and
# password authentication is off anyway.
RUN groupadd --gid 1000 sandbox \
 && useradd --uid 1000 --gid 1000 --password '*' --create-home \
      --home-dir /home/sandbox --shell /bin/bash sandbox

# sshd needs its privilege separation directory. The host keys shipped with the
# package are removed: every sandbox presents its own, from the Secret.
RUN mkdir -p /run/sshd \
 && chmod 0755 /run/sshd \
 && rm -f /etc/ssh/ssh_host_*

COPY entrypoint.sh /usr/local/bin/forgeai-sshd
RUN chmod 0755 /usr/local/bin/forgeai-sshd

EXPOSE 22

ENTRYPOINT ["/usr/local/bin/forgeai-sshd"]
