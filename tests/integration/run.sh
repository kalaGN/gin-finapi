#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."
containers=()
cleanup() {
  if ((${#containers[@]})); then docker rm -f "${containers[@]}" >/dev/null; fi
}
trap cleanup EXIT
redis_name="gin-finapi-integration-redis-$$"
mongo_name="gin-finapi-integration-mongo-$$"
docker run -d --name "$redis_name" -p 127.0.0.1::6379 redis:7.4.7 >/dev/null
containers+=("$redis_name")
docker run -d --name "$mongo_name" -p 127.0.0.1::27017 mongo:8.0.16 >/dev/null
containers+=("$mongo_name")
ready=false
for attempt in {1..30}; do
  if docker exec "$redis_name" redis-cli ping >/dev/null 2>&1 && docker exec "$mongo_name" mongosh --quiet --eval 'if (db.adminCommand({ping:1}).ok !== 1) quit(1)' >/dev/null 2>&1; then
    ready=true
    break
  fi
  sleep 1
done
if [ "$ready" != true ]; then
  echo 'Integration databases did not become ready' >&2
  exit 1
fi
redis_binding=$(docker port "$redis_name" 6379/tcp)
mongo_binding=$(docker port "$mongo_name" 27017/tcp)
export FINAPI_TEST_REDIS_ADDR="$redis_binding"
export FINAPI_TEST_MONGO_DSN="mongodb://$mongo_binding"
go test -race -count=1 -tags integration ./bootstrap -run '^TestLoanIntegration$' -v
go test -race -count=1 cmd/init_redis.go cmd/init_redis_integration_test.go -v
