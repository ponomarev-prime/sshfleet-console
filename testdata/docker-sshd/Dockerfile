FROM debian:12-slim

RUN apt-get update \
    && apt-get install -y --no-install-recommends openssh-server \
    && rm -rf /var/lib/apt/lists/* \
    && useradd --create-home --shell /bin/sh sshfleet \
    && printf '%s\n' 'sshfleet:sshf-fixture-password-auth-is-disabled' | chpasswd \
    && mkdir -p /run/sshd

COPY sshd_config /etc/ssh/sshd_config

EXPOSE 2222

CMD ["/usr/sbin/sshd", "-D", "-e", "-f", "/etc/ssh/sshd_config"]
