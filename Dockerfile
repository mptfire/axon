FROM alpine

LABEL org.opencontainers.image.authors="philipp.heckel@gmail.com"
LABEL org.opencontainers.image.url="https://ntfy.sh/"
LABEL org.opencontainers.image.documentation="https://docs.ntfy.sh/"
LABEL org.opencontainers.image.source="https://github.com/binwiederhier/ntfy"
LABEL org.opencontainers.image.vendor="Philipp C. Heckel"
LABEL org.opencontainers.image.licenses="Apache-2.0, GPL-2.0"
LABEL org.opencontainers.image.title="ntfy"
LABEL org.opencontainers.image.description="Send push notifications to your phone or desktop using PUT/POST"

# axon: run as an unprivileged user. If you bind-mount cache/config dirs from the
# host, chown them to the image's ntfy uid/gid (check with: docker run --rm IMAGE id)
# or override with docker run --user.
# The binary keeps cap_net_bind_service so the default :80 listen address still works;
# on rootless runtimes (which ignore file capabilities), publish to an unprivileged
# container port (e.g. -p 80:8080 serve --listen-http :8080) or pass
# --sysctl net.ipv4.ip_unprivileged_port_start=0.
RUN apk add --no-cache tzdata libcap \
    && addgroup -S ntfy \
    && adduser -S -D -H -h /var/lib/ntfy -s /sbin/nologin -G ntfy ntfy \
    && mkdir -p /var/cache/ntfy /var/lib/ntfy /etc/ntfy \
    && chown -R ntfy:ntfy /var/cache/ntfy /var/lib/ntfy /etc/ntfy
COPY --chown=ntfy:ntfy ntfy /usr/bin
RUN setcap cap_net_bind_service=+ep /usr/bin/ntfy

USER ntfy

EXPOSE 80/tcp
ENTRYPOINT ["ntfy"]
