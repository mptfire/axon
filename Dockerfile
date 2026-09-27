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
# host, chown them to uid/gid 10001 on the host (or override with docker run --user).
RUN apk add --no-cache tzdata \
    && addgroup -S ntfy \
    && adduser -S -D -H -h /var/lib/ntfy -s /sbin/nologin -G ntfy ntfy \
    && mkdir -p /var/cache/ntfy /var/lib/ntfy /etc/ntfy \
    && chown -R ntfy:ntfy /var/cache/ntfy /var/lib/ntfy /etc/ntfy
COPY --chown=ntfy:ntfy ntfy /usr/bin

USER ntfy

EXPOSE 80/tcp
ENTRYPOINT ["ntfy"]
