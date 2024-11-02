## Example env variables for both the in and out services.
# Note: it is safe to set all the variables for both services

## Variables for In and Out
export AMQP_URI="amqp://guest:guest@localhost:5672/"
export AMQP_REQUEST_EXCHANGE="inout-request"
export AMQP_REQUEST_EXCHANGE_TYPE="fanout"
export AMQP_REQUEST_QUEUE_NAME="request"
export AMQP_QUEUE_TTL_MS="120000"
export HTTP_TIMEOUT_S="120"
export AMQP_RESPONSE_EXCHANGE="inout-response"
export AMQP_RESPONSE_EXCHANGE_TYPE="direct"
export AMQP_RESPONSE_QUEUE_PREFIX="response"
export DEBUG=false

## Variables only used by Out
# Note: backends is a comma separated list and requests will be
# load balanced among them. Specifying only one is fine.
export BACKENDS="http://localhost:8080,http://localhost:8081"
export LISTEN_PORT=":9090"
export HEADER_BIT_SHIFT="20"
export RETRY_SLEEP_S=5
export MAX_RETRIES=2








