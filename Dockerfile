# Multi-stage, cross-compiled: the build stage runs on the runner's native arch and Go
# emits the target arch, so linux/arm64 builds need no QEMU.
FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS build
ARG TARGETOS TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /out/mario-maker ./cmd/mario-maker

FROM alpine:3.20
# Links the ghcr package to the repository (the release workflow sets the full label set too).
LABEL org.opencontainers.image.source="https://github.com/openpak/mario-maker" \
      org.opencontainers.image.licenses="AGPL-3.0-only"
RUN apk add --no-cache ca-certificates tzdata \
 && adduser -D -u 65532 -h /app app \
 && mkdir -p /app/cache && chown -R 65532:65532 /app
WORKDIR /app
COPY --from=build /out/mario-maker /app/mario-maker
# Unprivileged; the listener (default :20161) is above 1024 so no capability is needed.
USER 65532:65532
ENV MARIO_MAKER_CACHE_DIR=/app/cache
EXPOSE 20161
CMD ["/app/mario-maker"]
