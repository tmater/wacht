# Build stage
FROM golang:1.26-alpine AS builder

ARG BINARY
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go build -o /out/${BINARY} ./cmd/${BINARY}/

# Runtime stage: minimal Alpine image, no Go toolchain
FROM alpine:3.23.3
# Alpine doesn't include CA certificates by default — required for HTTPS checks
RUN apk add --no-cache ca-certificates
ARG BINARY
ENV BINARY=${BINARY}

COPY --from=builder /out/${BINARY} /usr/local/bin/${BINARY}

ENTRYPOINT ["/bin/sh", "-c", "exec /usr/local/bin/${BINARY} \"$@\"", "--"]
