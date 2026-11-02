FROM golang:1.26-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/flixflox-bot ./cmd/flixflox-bot

FROM alpine:3.22

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app
COPY --from=build /out/flixflox-bot /usr/local/bin/flixflox-bot

ENV DOWNLOAD_DIR=/data/uploads \
    TELEGRAM_DATA_DIR=/data/telegram-data \
    TELEGRAM_SERVER_WORK_DIR=/var/lib/telegram-bot-api \
    TELEGRAM_API_ENDPOINT=http://telegram-bot-api:8081/bot%s/%s \
    TELEGRAM_FILE_ENDPOINT=http://telegram-bot-api:8081/file/bot%s/%s

RUN mkdir -p /data/uploads /data/telegram-data

USER flixflox

ENTRYPOINT ["flixflox-bot"]
