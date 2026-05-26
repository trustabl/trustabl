# GoReleaser dockers_v2 builds this image multi-arch via buildx and arranges the
# build context so each arch's binary lives under its platform dir
# (linux/amd64/trustabl, linux/arm64/trustabl) — hence the $TARGETPLATFORM COPY.
# Distroless `cc` (not `static`) because tree-sitter dynamically links libc.
# Runs as the nonroot user (65532).
FROM gcr.io/distroless/cc-debian12:nonroot
ARG TARGETPLATFORM
COPY $TARGETPLATFORM/trustabl /usr/local/bin/trustabl
ENTRYPOINT ["/usr/local/bin/trustabl"]
