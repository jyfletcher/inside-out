# Example env variables to provide both the in and out docker containers.
# Or source this file if running the bins directly from the CLI
#export BACKENDS="http://localhost:9090,http://localhost:9191,http://localhost:9292,http://localhost:9393"
#export BACKENDS="http://localhost:9595,http://localhost:9696,http://localhost:9797,http://localhost:9898"
#export BACKENDS="http://httpbin.org"
export BACKENDS="http://localhost:8080"
export LISTEN_PORT=":9090"
export AMQP_URI="amqp://guest:guest@localhost:5672/"
export AMQP_REQUEST_EXCHANGE="inout-request"
export AMQP_REQUEST_EXCHANGE_TYPE="fanout"
export AMQP_REQUEST_QUEUE_NAME="request"
export AMQP_RESPONSE_EXCHANGE="inout-response"
export AMQP_RESPONSE_EXCHANGE_TYPE="direct"
export AMQP_RESPONSE_QUEUE_PREFIX="response"
export AMQP_QUEUE_TTL_MS="120000"
export HTTP_TIMEOUT_S="120"
export HEADER_BIT_SHIFT="20"
export RETRY_SLEEP_S=5
export MAX_RETRIES=2
export DEBUG=false
