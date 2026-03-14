#!/bin/sh
set -eu

if [ "$(id -u)" -eq 0 ]; then
    for path in /captures /logs; do
        if [ -d "$path" ]; then
            echo "Fixing ownership under $path for ankerctl..."
            chown -R ankerctl:ankerctl "$path"
        fi
    done

    exec su-exec ankerctl "$@"
fi

exec "$@"
