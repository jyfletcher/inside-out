#!/bin/bash

docker run -dit --name out -e BACKENDS="http://localhost:9090,http://localhost:9191,http://localhost:9292,http://localhost:9393" -e LISTEN_PORT=":8080" -e AMQP_URI="amqp://guest:guest@localhost:5672/" -e AMQP_REQUEST_EXCHANGE="inout-request" -e AMQP_REQUEST_EXCHANGE_TYPE="fanout" -e AMQP_REQUEST_QUEUE_NAME="request" -e AMQP_RESPONSE_EXCHANGE="inout-response" -e AMQP_RESPONSE_EXCHANGE_TYPE="direct" -e AMQP_RESPONSE_QUEUE_NAME="response" out-20200514:latest
