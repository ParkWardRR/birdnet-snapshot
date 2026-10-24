# syntax=docker/dockerfile:1
# birdnet-snapshot with headless Chrome bundled: one container, no other setup.
FROM --platform=$BUILDPLATFORM docker.io/library/golang:1.27 AS build
ARG TARGETOS TARGETARCH VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/birdnet-snapshot .

FROM docker.io/chromedp/headless-shell:157.0.8103.15
ARG VERSION=dev
LABEL org.opencontainers.image.title="birdnet-snapshot" \
      org.opencontainers.image.description="Publish a read-only snapshot of a BirdNET-Go web UI to a CDN" \
      org.opencontainers.image.source="https://github.com/ParkWardRR/birdnet-snapshot" \
      org.opencontainers.image.licenses="BlueOak-1.0.0" \
      org.opencontainers.image.version="${VERSION}"
# headless-shell ships no CA bundle; Go needs one for HTTPS to Bunny.
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /out/birdnet-snapshot /usr/local/bin/birdnet-snapshot
RUN useradd --uid 10001 --create-home --shell /usr/sbin/nologin snapshot \
 && mkdir -p /data && chown snapshot:snapshot /data
USER snapshot
# Chrome has no user namespaces to sandbox with inside a container.
ENV CHROME_PATH=/headless-shell/headless-shell \
    CHROME_NO_SANDBOX=true \
    SNAPSHOT_OUT=/data/site
VOLUME /data
ENTRYPOINT ["/usr/local/bin/birdnet-snapshot"]
CMD ["run", "-every", "1h"]
