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

RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/freecenter ./cmd/freecenter

# ---------- run ----------
FROM alpine:3.20
RUN adduser -D -u 10001 -g freecenter freecenter && apk add --no-cache ca-certificates qemu-img cdrkit
COPY --from=build /out/freecenter /usr/local/bin/freecenter
COPY LICENSE NOTICE /usr/share/licenses/freecenter/

ENV FREECENTER_ADDR=:8080 \
    FREECENTER_DATA=/data
RUN mkdir -p /data && chown freecenter:freecenter /data
USER freecenter
VOLUME ["/data"]
EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD wget -q -O /dev/null http://127.0.0.1:8080/ || exit 1

ENTRYPOINT ["freecenter"]
