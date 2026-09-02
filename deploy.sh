#!/bin/sh
# Запускается на сервере из /var/www/log-awg (репозиторий уже склонирован туда).

set -e
cd "$(dirname "$0")"

sudo -u www-data git pull

echo "build"

go version

sudo -u www-data env "PATH=$PATH" go build -buildvcs=false -o log-awg ./cmd/log-awg

echo "migrate"

set -a
. ./env
set +a
migrate -path db/migrations -database "$DATABASE_URL" up

echo "restart"

sudo supervisorctl restart log-awg

echo "success"
