# flixflox-telegram-bot

Telegram bot that receives movies and series and stores them tidily on disk,
under `uploads/<category>/<title>/`.

It uses a [local Bot API server](https://github.com/tdlib/telegram-bot-api) to
lift the 20 MB limit of the official API: with the local server files can reach
2 GB and, since the bot shares the volume with the container, they are moved
with a `rename` instead of being downloaded over HTTP.

## Structure

```
cmd/flixflox-bot/      entrypoint: loads config, builds the bot and handles signals
internal/config/       environment variables, validation and path translation
internal/storage/      safe names and atomic writes to disk
internal/bot/          Bot API client, handlers, sessions and downloads
docker-compose.yml     local Bot API server
```

The flow of an upload: `handlers.go` resolves category and title against the
`sessionStore`, `download.go` hands the file off to a goroutine and `storage`
puts it in its final place without overwriting anything.

## Getting started

```sh
cp .env.example .env     # fill in BOT_TOKEN, TELEGRAM_API_ID and TELEGRAM_API_HASH
make up                  # start the local Bot API server
make run                 # run the bot
```

`make help` lists the remaining targets (`build`, `test`, `cover`, `vet`,
`tidy`, `clean`, `down`, `logs`).

## Usage

| Command   | What it does                          |
| --------- | ------------------------------------- |
| `/start`  | choose between movie or series        |
| `/cancel` | forget the upload in progress         |
| `/skip`   | leave the field being asked for empty |
| `/help`   | show the help                         |

After picking a category the bot asks for the data one field at a time (title,
description, year, genres, rating and backdrop image; for a series, also
season, episode and the skip-intro timings) and only at the end the file.
Everything but the title can be skipped with `/skip`.

The file is saved under the name built from that data —
`Blade Runner (1982).mkv` for a movie, `S01E02 - Title.mkv` for an episode —
and the session stays open: several files can be sent in a row for the same
title.

## Configuration

Everything comes from environment variables; see `.env.example` for the full
list. The required ones are `BOT_TOKEN` and, for the container,
`TELEGRAM_API_ID` / `TELEGRAM_API_HASH`.

`ALLOWED_USER_IDS` holds the authorized numeric IDs, comma-separated. If it is
left empty the bot **does not start**, and a malformed ID also aborts startup:
a list that cannot be parsed would let anyone in.

## Security

- The token never goes into the code or the repo: it lives only in `.env`,
  which is ignored by git.
- The local Bot API server listens on `127.0.0.1` only, because it exposes the
  token in file URLs.
- `DEBUG=true` prints every Bot API request, token included: do not leave it on.
- Titles and file names come from the user and are sanitized before touching
  the disk (`internal/storage`), so they cannot escape `uploads/`.

## Tests

```sh
make test
```

They cover what can break silently: name sanitizing (traversal, truncation),
path translation from container to host, destination deduplication and the file
move.
