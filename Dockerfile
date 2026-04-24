# syntax=docker/dockerfile:1

# Stage 1: build
FROM golang:1.25-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG BUILD_VERSION=dev
ARG VCS_REF=unknown
ARG BUILD_DATE=unknown
ARG SOURCE_URL=https://github.com/hanmahong5-arch/lurus-tally
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w -X github.com/hanmahong5-arch/lurus-tally/internal/pkg/version.Version=${BUILD_VERSION}" \
    -trimpath -o /tally-backend ./cmd/server

# Stage 2: runtime (scratch — minimal attack surface, image < 15 MB)
FROM scratch
# Re-declare ARGs so they are available in this stage
ARG VCS_REF=unknown
ARG BUILD_DATE=unknown
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /tally-backend /tally-backend
LABEL org.opencontainers.image.source="https://github.com/hanmahong5-arch/lurus-tally" \
      org.opencontainers.image.revision="${VCS_REF}" \
      org.opencontainers.image.created="${BUILD_DATE}"
USER 65534
EXPOSE 18200
ENTRYPOINT ["/tally-backend"]
