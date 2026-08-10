# The image is the static binary, the CA bundle it needs to reach Sentry over
# TLS, and the timezone database. There is no shell, no libc and no package
# manager in it. The release workflow drops a CGO-free sentry-tunnel for each
# architecture next to this file, and that is the entire image.
#
# `TARGETARCH` is set by buildx per platform, so one build produces amd64 and
# arm64 under a single tag and the daemon picks the right one. Nothing is
# emulated: both binaries were already cross-compiled by the release matrix,
# and the rootfs stage is pinned to $BUILDPLATFORM because the two things taken
# out of it — certificates and zoneinfo — are the same bytes on every
# architecture.
FROM --platform=$BUILDPLATFORM alpine:3.24 AS rootfs
RUN apk add --no-cache ca-certificates tzdata

FROM scratch

COPY --from=rootfs /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=rootfs /usr/share/zoneinfo /usr/share/zoneinfo

ENV TZ=UTC

ARG TARGETARCH
COPY binaries/linux/${TARGETARCH}/sentry-tunnel /app/sentry-tunnel
COPY LICENSE /LICENSE

WORKDIR /app

# Unprivileged, and numeric because there is no /etc/passwd to resolve a name
# against.
USER 65532:65532

EXPOSE 7878

ENTRYPOINT ["/app/sentry-tunnel"]
