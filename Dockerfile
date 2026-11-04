# Static Go binary on distroless. CGO_ENABLED=0: no libc, no OpenSSL surface.
# Built per-arch natively by docker.yml (no QEMU) and merged into one multi-arch tag set.
# The image ships the `serve` daemon (PRD §3.2, 127.0.0.1:8799) for headless hosts;
# the decision path makes zero network calls regardless.
# VERSION_PKG must match go.mod's module path (see the Makefile variable of the same name).
# syntax=docker/dockerfile:1
FROM golang:1.27-alpine AS build
ARG VERSION=dev
ARG VERSION_PKG=github.com/teochenglim/glove/internal/version
WORKDIR /src
COPY go.* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath \
      -ldflags "-s -w -X ${VERSION_PKG}.Version=${VERSION}" \
      -o /out/glove ./cmd/glove

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/glove /glove
EXPOSE 8799
USER nonroot
ENTRYPOINT ["/glove"]
