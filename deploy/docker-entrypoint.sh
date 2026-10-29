#!/bin/sh
set -eu

if [ "$(id -u)" = 0 ]; then
    # 绑定目录首次可能由 Docker 以 root 创建，先准备权限，再降权启动应用。
    key_file="${MASTER_KEY_FILE:-/run/opsdeck-secrets/master.key}"
    key_dir="$(dirname "$key_file")"
    mkdir -p "$key_dir"
    chown 10001:10001 "$key_dir"
    chmod 0700 "$key_dir"
    if [ -f "$key_file" ]; then
        chown 10001:10001 "$key_file"
        chmod 0600 "$key_file"
    fi
    exec su -s /bin/sh -c 'exec /app/opsdeck "$@"' opsdeck opsdeck "$@"
fi

exec /app/opsdeck "$@"
