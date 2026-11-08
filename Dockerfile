# Multi-stage Dockerfile for GoList

# Stage 1: Build static binary
FROM golang:1.22-alpine AS builder

WORKDIR /src

# Copy go module files
COPY go.mod ./
RUN go mod download

# Copy source code
COPY . .

ARG VERSION=dev
ARG COMMIT=none
ARG DATE=unknown

# Build static binary without CGO
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w -X main.Version=${VERSION} -X main.Commit=${COMMIT} -X main.Date=${DATE}" \
    -o /bin/golist .

# Stage 2: Minimal runtime image
FROM alpine:3.20

# Install CA certificates for TLS calls to Gemini & eBay APIs
RUN apk --no-cache add ca-certificates tzdata

# Create non-root user
RUN addgroup -S golist && adduser -S golist -G golist

# Prepare data storage directory
RUN mkdir -p /data && chown -R golist:golist /data

USER golist

WORKDIR /app
COPY --from=builder /bin/golist /usr/local/bin/golist

ENV DATA_DIR=/data

VOLUME ["/data"]

ENTRYPOINT ["/usr/local/bin/golist"]
CMD ["status"]
