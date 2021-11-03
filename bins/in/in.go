package main

import (
	"bytes"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"

	"io/ioutil"
	"log"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"time"

	uuid "github.com/satori/go.uuid"
	"github.com/streadway/amqp"
)

type AMQPREQRESP struct {
	Payload AMQPPayload
	CorrID  string
	Key     string
}

type AMQPConnInfo struct {
	AmqpURI      string
	Exchange     string
	ExchangeType string
	QueueName    string
}

type AMQPPayload struct {
	CorrID     string
	Key        string
	Backends   []string
	Host       string
	URL        string
	Method     string
	Proto      string
	Status     string
	StatusCode int
	Headers    map[string][]string
	Body       string
}

type Config struct {
	AMQPURI                  string
	AMQPRequestExchange      string
	AMQPRequestExchangeType  string
	AMQPRequestQueueName     string
	AMQPResponseExchange     string
	AMQPResponseExchangeType string
	AMQPResponseQueuePrefix  string
	AMQPQueueTTL             int
	HTTPTimeout              int
	DEBUG                    bool
}

var AMQPREQUESTCHAN chan AMQPPayload
var HTTPRESPONSECHAN chan AMQPPayload
var NODEID string
var CONFIG Config

func init() {
	rand.Seed(time.Now().Unix())
	AMQPREQUESTCHAN = make(chan AMQPPayload, 50)
	HTTPRESPONSECHAN = make(chan AMQPPayload, 50)
	NODEID = genID()
	CONFIG = Config{}

	CONFIG.AMQPURI = os.Getenv("AMQP_URI")
	if CONFIG.AMQPURI == "" {
		log.Fatal("Environment variable AMQP_URI must be set.")
	}
	CONFIG.AMQPRequestExchange = os.Getenv("AMQP_REQUEST_EXCHANGE")
	if CONFIG.AMQPRequestExchange == "" {
		log.Fatal("Environment variable AMQP_REQUEST_EXCHANGE must be set.")
	}
	CONFIG.AMQPRequestExchangeType = os.Getenv("AMQP_REQUEST_EXCHANGE_TYPE")
	if CONFIG.AMQPRequestExchangeType == "" {
		log.Fatal("Environment variable AMQP_REQUEST_EXCHANGE_TYPE must be set.")
	}
	CONFIG.AMQPRequestQueueName = os.Getenv("AMQP_REQUEST_QUEUE_NAME")
	if CONFIG.AMQPRequestQueueName == "" {
		log.Fatal("Environment variable AMQP_REQUEST_QUEUE_NAME must be set.")
	}
	CONFIG.AMQPResponseExchange = os.Getenv("AMQP_RESPONSE_EXCHANGE")
	if CONFIG.AMQPResponseExchange == "" {
		log.Fatal("Environment variable AMQP_RESPONSE_EXCHANGE must be set.")
	}
	CONFIG.AMQPResponseExchangeType = os.Getenv("AMQP_RESPONSE_EXCHANGE_TYPE")
	if CONFIG.AMQPResponseExchangeType == "" {
		log.Fatal("Environment variable AMQP_RESPONSE_EXCHANGE_TYPE must be set.")
	}
	queueTTL, err := strconv.Atoi(os.Getenv("AMQP_QUEUE_TTL_MS"))
	if err != nil {
		log.Fatal("Environment variable AMQP_QUEUE_TTL_MS must be set to a positive integer in milliseconds.")
	}
	if queueTTL <= 0 {
		log.Fatal("How would a 0 or negative TTL make any sense!?!?!?")
	}
	CONFIG.AMQPQueueTTL = queueTTL
	httpTimeout, err := strconv.Atoi(os.Getenv("HTTP_TIMEOUT_S"))
	if err != nil {
		log.Fatal("Environment variable HTTP_TIMEOUT_S must be set to a positive integer in seconds.")
	}
	if httpTimeout <= 0 {
		log.Fatal("Really?  You want instantly timeout the HTTP connection!?!?  Think about it, and try again...")
	}
	CONFIG.HTTPTimeout = httpTimeout
	debug, err := strconv.ParseBool(os.Getenv("DEBUG"))
	if err != nil {
		log.Fatal("Failure parsing debug setting.")
	}
	CONFIG.DEBUG = debug

}

func main() {

	requestConn := AMQPConnInfo{}
	responseConn := AMQPConnInfo{}

	requestConn.AmqpURI = CONFIG.AMQPURI
	requestConn.Exchange = CONFIG.AMQPRequestExchange
	requestConn.ExchangeType = CONFIG.AMQPRequestExchangeType
	requestConn.QueueName = CONFIG.AMQPRequestQueueName

	responseConn.AmqpURI = CONFIG.AMQPURI
	responseConn.Exchange = CONFIG.AMQPResponseExchange
	responseConn.ExchangeType = CONFIG.AMQPResponseExchangeType

	go requestReceiver(requestConn)
	go requestHandler()
	go responseSender(responseConn)

	select {}

}

func requestHandler() {
	log.Println("Starting Request Handler.")

	tr := &http.Transport{
		MaxIdleConns:    10,
		IdleConnTimeout: 30 * time.Second,
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}

	client := &http.Client{
		Transport: tr,
		Timeout:   time.Duration(CONFIG.HTTPTimeout) * time.Second,
	}

	for {
		p := <-AMQPREQUESTCHAN
		go executeRequest(p, client)
	}

}

func executeRequest(p AMQPPayload, client *http.Client) {
	var responsePayload AMQPPayload
	bodyBytes, err := base64.StdEncoding.DecodeString(p.Body)
	if err != nil {
		log.Println("Cannot decode base64 body, returning empty payload: ", err)
		responsePayload = AMQPPayload{CorrID: p.CorrID, Key: p.Key}
		HTTPRESPONSECHAN <- responsePayload
		return
	}

	// Sending <= 0 to rand.Intn will cause a panic so check for it
	// and handle appropriately
	numBackends := len(p.Backends)

	if numBackends <= 0 {
		log.Println("Length of backends is <= 0, returning empty payload.")
		responsePayload = AMQPPayload{CorrID: p.CorrID, Key: p.Key}
		HTTPRESPONSECHAN <- responsePayload
		return
	}

	backendHost := p.Backends[rand.Intn(numBackends)]

	if CONFIG.DEBUG {
		log.Printf("Payload: %+v\n", p)
		log.Println(p.Method)
	}

	req, err := http.NewRequest(p.Method, backendHost+p.URL, bytes.NewReader(bodyBytes))
	if err != nil {
		log.Fatal("Cannot create request object: ", err)
	}
	for header, values := range p.Headers {
		for _, value := range values {
			req.Header.Add(header, value)
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		log.Println("Error executing request, returning empty payload: ", err)
		// Since the request failed then we can't trust the response,
		// so just send back and empty payload.
		// The out component checks if the payload has valid response
		// data and will send a 503 back to the caller if not.
		responsePayload = AMQPPayload{CorrID: p.CorrID, Key: p.Key}
		HTTPRESPONSECHAN <- responsePayload
		return
	}
	defer resp.Body.Close()
	responsePayload = packagePayload(p, resp)
	HTTPRESPONSECHAN <- responsePayload
}

func responseSender(c AMQPConnInfo) {

	log.Println("Starting Response Sender.")

	ch, notifyClose := getAMQPChan(c)

	for {
		select {
		case p := <-HTTPRESPONSECHAN:

			encoded, err := json.Marshal(&p)
			if err != nil {
				log.Fatal("Error encoding payload to json: ", err)
			}

			msg := amqp.Publishing{
				Headers:         amqp.Table{},
				ContentType:     "application/json",
				ContentEncoding: "",
				Body:            encoded,
				DeliveryMode:    amqp.Transient,
				Priority:        0,
			}
			err = ch.Publish(c.Exchange, p.Key, true, false, msg)
			if err != nil {
				log.Fatal("Error publishing message to AMQP server: ", err)
			}
		case closed := <-notifyClose:
			log.Fatal("Received notification of channel closing: ", closed)
		}
	}

}

func packagePayload(requestPayload AMQPPayload, response *http.Response) AMQPPayload {
	p := AMQPPayload{}
	p.CorrID = requestPayload.CorrID
	p.Key = requestPayload.Key
	p.Host = requestPayload.Host
	p.Method = requestPayload.Method
	p.Proto = response.Proto
	p.Status = response.Status
	p.StatusCode = response.StatusCode
	p.URL = requestPayload.URL

	bodyBytes, err := ioutil.ReadAll(response.Body)
	if err != nil {
		log.Println("Unable to read request body: ", err)
		return AMQPPayload{CorrID: requestPayload.CorrID, Key: requestPayload.Key}

	}
	p.Body = base64.StdEncoding.EncodeToString(bodyBytes)

	p.Headers = make(map[string][]string)
	for name, values := range response.Header {
		for _, value := range values {
			p.Headers[name] = append(p.Headers[name], value)
		}
	}

	return p

}

func requestReceiver(c AMQPConnInfo) {
	log.Println("Starting Request Receiver.")

	ch, notifyClose := getAMQPChan(c)

	messagechan, err := ch.Consume(c.QueueName, NODEID, false, false, false, false, nil)
	if err != nil {
		log.Fatal("Could not consume from queue: ", err)
	}

	for {
		select {
		case m := <-messagechan:
			p := unpackagePayload(m)
			AMQPREQUESTCHAN <- p
			m.Ack(false)
		case closed := <-notifyClose:
			log.Fatal("Received notification of channel closing: ", closed)
		}
	}

}

func unpackagePayload(m amqp.Delivery) AMQPPayload {
	p := AMQPPayload{}
	json.Unmarshal(m.Body, &p)

	return p

}

func getAMQPChan(c AMQPConnInfo) (*amqp.Channel, chan *amqp.Error) {

	var conn *amqp.Connection
	var err error

	for conn, err = amqp.Dial(c.AmqpURI); err != nil; conn, err = amqp.Dial(c.AmqpURI) {
		time.Sleep(2 * time.Second)
		log.Println("Unable to connect to AMQP host: ", err)
	}

	notifyClose := conn.NotifyClose(make(chan *amqp.Error))

	ch, err := conn.Channel()
	if err != nil {
		log.Fatal("Unable to create a channel: ", err)
	}

	if c.Exchange != "" {
		err = ch.ExchangeDeclare(c.Exchange, c.ExchangeType, false, true, false, false, nil)
		if err != nil {
			log.Fatal("Could not declare exhange: ", err)
		}
	}

	if c.QueueName != "" {
		queueArgs := amqp.Table{}
		queueArgs["x-message-ttl"] = CONFIG.AMQPQueueTTL
		_, err = ch.QueueDeclare(c.QueueName, false, true, false, false, queueArgs)
		if err != nil {
			log.Fatal("Could not declare queue: ", err)
		}

		err = ch.QueueBind(c.QueueName, "request", c.Exchange, true, nil)
		if err != nil {
			log.Fatal("Could not bind queue to exchange: ", err)
		}
	}

	return ch, notifyClose

}

func genID() string {
	id := uuid.NewV4()
	return fmt.Sprintf("%s", id)
}
