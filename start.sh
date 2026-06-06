#!/bin/bash
LOCK=/tmp/streamdeck-twitch.lock
if [ -f "$LOCK" ]; then
    OLD=$(cat "$LOCK")
    [ -n "$OLD" ] && [ -d "/proc/$OLD" ] && kill "$OLD" 2>/dev/null
fi
echo $$ > "$LOCK"
until getent hosts api.twitch.tv >/dev/null 2>&1; do sleep 1; done
exec /home/nifuramu/Desktop/StreamDeck/streamdeck-twitch "$@"
