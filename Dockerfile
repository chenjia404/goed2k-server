# syntax=docker/dockerfile:1

FROM golang:1.25-alpine AS builder

WORKDIR /src

RUN apk add --no-cache ca-certificates git

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/goed2k-server ./cmd/goed2k-server

FROM alpine:latest

RUN apk add --no-cache ca-certificates tzdata \
	&& adduser -D -H -s /sbin/nologin -u 65532 app

WORKDIR /app

COPY --from=builder /out/goed2k-server /app/goed2k-server

USER 65532:65532

EXPOSE 4661/tcp 8080/tcp

ENTRYPOINT ["/app/goed2k-server"]
CMD ["-config", "/app/config.json"]
