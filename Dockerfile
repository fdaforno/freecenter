# ---------- ui ----------
# The output does not depend on the architecture: built once, on the build platform.
FROM --platform=$BUILDPLATFORM node:24-alpine AS ui
WORKDIR /src/web/ui
COPY web/ui/package.json web/ui/package-lock.json ./
RUN npm ci
COPY web/ui ./
RUN npm run build

# ---------- build ----------
FROM golang:1.26.5-alpine AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal
COPY web/embed.go ./web/embed.go
COPY --from=ui /src/web/dist ./web/dist

RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/hostwarden ./cmd/hostwarden

# ---------- run ----------
FROM alpine:3.20
RUN adduser -D -u 10001 -g hostwarden hostwarden && apk add --no-cache ca-certificates qemu-img cdrkit
COPY --from=build /out/hostwarden /usr/local/bin/hostwarden
COPY LICENSE NOTICE /usr/share/licenses/hostwarden/

ENV HOSTWARDEN_ADDR=:8080 \
    HOSTWARDEN_DATA=/data
RUN mkdir -p /data && chown hostwarden:hostwarden /data
USER hostwarden
VOLUME ["/data"]
EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD wget -q -O /dev/null http://127.0.0.1:8080/ || exit 1

ENTRYPOINT ["hostwarden"]
