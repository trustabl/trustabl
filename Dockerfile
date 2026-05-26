# GoReleaser copies the pre-built Linux binary for the target arch into this
# image (see .goreleaser.yaml `dockers:`). Distroless `cc` (not `static`)
# because tree-sitter dynamically links libc. Runs as the nonroot user (65532).
FROM gcr.io/distroless/cc-debian12:nonroot
COPY trustabl /usr/local/bin/trustabl
ENTRYPOINT ["/usr/local/bin/trustabl"]
