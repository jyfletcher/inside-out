package main

import "encoding/base64"
import "encoding/json"
import "fmt"
import "github.com/satori/go.uuid"
import "github.com/streadway/amqp"
import "io"
import "log"
import "net/http"
import "os"
import "strconv"
import "strings"
import "time"

type HTTPREQRESP struct {
	Request  *http.Request
	Response chan AMQPPayload
	CorrID   string
	Key      string
}

type AMQPConnInfo struct {
	AmqpURI      string
	Exchange     string
	ExchangeType string
	QueueName    string
	TTL          int
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
	Backends                 []string
	ListenPort               string
	AMQPURI                  string
	AMQPRequestExchange      string
	AMQPRequestExchangeType  string
	AMQPResponseExchange     string
	AMQPResponseExchangeType string
	AMQPResponseQueuePrefix  string
	AMQPQueueTTL             int
	HTTPTimeout              int
	HeaderBitShift           int
	RetrySleepSec            int
	MaxRetries               int
}

var HTTPREQRESPCHAN chan HTTPREQRESP
var AMQPSENDCHAN chan AMQPPayload
var AMQPRECEIVECHAN chan AMQPPayload
var NODEID string
var CONFIG Config

func init() {
	HTTPREQRESPCHAN = make(chan HTTPREQRESP, 50)
	AMQPSENDCHAN = make(chan AMQPPayload, 50)
	AMQPRECEIVECHAN = make(chan AMQPPayload, 50)
	NODEID = genID()
	CONFIG = Config{}

	CONFIG.Backends = strings.Split(os.Getenv("BACKENDS"), ",")
	if CONFIG.Backends[0] == "" {
		log.Fatal("Environment variable BACKENDS (comma separated list) must be set.")
	}
	CONFIG.ListenPort = os.Getenv("LISTEN_PORT")
	if CONFIG.ListenPort == "" {
		log.Fatal("Environment variable LISTEN_PORT (ex \":8080\") must be set.")
	}
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
	CONFIG.AMQPResponseExchange = os.Getenv("AMQP_RESPONSE_EXCHANGE")
	if CONFIG.AMQPResponseExchange == "" {
		log.Fatal("Environment variable AMQP_RESPONSE_EXCHANGE must be set.")
	}
	CONFIG.AMQPResponseExchangeType = os.Getenv("AMQP_RESPONSE_EXCHANGE_TYPE")
	if CONFIG.AMQPResponseExchangeType == "" {
		log.Fatal("Environment variable AMQP_RESPONSE_EXCHANGE_TYPE must be set.")
	}
	CONFIG.AMQPResponseQueuePrefix = os.Getenv("AMQP_RESPONSE_QUEUE_PREFIX")
	if CONFIG.AMQPResponseQueuePrefix == "" {
		log.Fatal("Environment variable AMQP_RESPONSE_QUEUE_PREFIX must be set.")
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
		log.Fatal("Really?  You want to instantly timeout the HTTP connection!?!?!?")
	}
	CONFIG.HTTPTimeout = httpTimeout

	headerBitShift, err := strconv.Atoi(os.Getenv("HEADER_BIT_SHIFT"))
	if err != nil {
		log.Println("Environment variable HEADER_BIT_SHIFT must be set to a positive integer.")
		log.Fatal("This number is a bit shift of 1 to specify the max header size.  A value of 20 gives 1MB.")
	}
	if headerBitShift <= 0 {
		log.Fatal("Really?  If you don't have room for headers then your probably making things difficult for yourself....")
	}
	CONFIG.HeaderBitShift = headerBitShift

	sleepTime, err := strconv.Atoi(os.Getenv("RETRY_SLEEP_S"))
	if err != nil {
		log.Fatal("RETRY_SLEEP_S must be an integer in seconds.")
	}
	CONFIG.RetrySleepSec = sleepTime

	maxRetries, err := strconv.Atoi(os.Getenv("MAX_RETRIES"))
	if err != nil {
		log.Fatal("MAX_RETRIES must be an integer.")
	}
	CONFIG.MaxRetries = maxRetries
}

func main() {

	requestConn := AMQPConnInfo{}

	requestConn.AmqpURI = CONFIG.AMQPURI
	requestConn.Exchange = CONFIG.AMQPRequestExchange
	requestConn.ExchangeType = CONFIG.AMQPRequestExchangeType
	requestConn.TTL = CONFIG.AMQPQueueTTL

	responseConn := AMQPConnInfo{}

	responseConn.AmqpURI = CONFIG.AMQPURI
	responseConn.Exchange = CONFIG.AMQPResponseExchange
	responseConn.ExchangeType = CONFIG.AMQPResponseExchangeType
	responseConn.QueueName = CONFIG.AMQPResponseQueuePrefix + "-" + NODEID
	responseConn.TTL = CONFIG.AMQPQueueTTL

	go requestSender(requestConn)

	go responseReceiver(responseConn)

	go router()

	s := &http.Server{
		Addr:           CONFIG.ListenPort,
		ReadTimeout:    time.Duration(CONFIG.HTTPTimeout) * time.Second,
		WriteTimeout:   time.Duration(CONFIG.HTTPTimeout) * time.Second,
		MaxHeaderBytes: 1 << CONFIG.HeaderBitShift,
	}
	http.HandleFunc("/", httpReceiver)
	log.Println("Starting HTTP listener.")
	log.Fatal(s.ListenAndServe())

}

func router() {
	log.Println("Starting Router.")

	connectionMap := make(map[string]HTTPREQRESP)

	for {
		select {
		case httprequest := <-HTTPREQRESPCHAN:
			connectionMap[httprequest.CorrID] = httprequest
			p := packagePayload(httprequest)
			AMQPSENDCHAN <- p
		case amqpresponse := <-AMQPRECEIVECHAN:
			rr := connectionMap[amqpresponse.CorrID]
			if rr.CorrID == "" {
				log.Println("Received AMQP response with no matching CorrID.")
			}
			rr.Response <- amqpresponse
			delete(connectionMap, rr.CorrID)
		}
	}
}

func packagePayload(rr HTTPREQRESP) AMQPPayload {
	p := AMQPPayload{}
	p.CorrID = rr.CorrID
	p.Key = NODEID
	p.Host = rr.Request.Host
	p.Method = rr.Request.Method
	p.URL = rr.Request.URL.String()
	p.Backends = CONFIG.Backends

	bodyBytes, err := io.ReadAll(rr.Request.Body)
	if err != nil {
		// Something is wrong with the connection, return empty payload with routing info
		// TODO: Maybe this should return a 4xx (400?)
		log.Println("Unable to read request body: ", err)
		return AMQPPayload{CorrID: p.CorrID, Key: p.Key}
	}
	p.Body = base64.StdEncoding.EncodeToString(bodyBytes)

	p.Headers = make(map[string][]string)
	for name, values := range rr.Request.Header {
		for _, value := range values {
			p.Headers[name] = append(p.Headers[name], value)
		}
	}

	return p

}

func responseReceiver(c AMQPConnInfo) {

	ch, notifyClose := getAMQPChan(c)

	messagechan, err := ch.Consume(c.QueueName, NODEID, false, false, false, false, nil)
	if err != nil {
		log.Fatal("could not consume from queue:", err)
	}
	for {
		select {
		case m := <-messagechan:
			p := unpackagePayload(m)
			AMQPRECEIVECHAN <- p
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

func requestSender(c AMQPConnInfo) {

	log.Println("Starting Request Sender.")

	ch, notifyClose := getAMQPChan(c)

	for {
		select {
		case p := <-AMQPSENDCHAN:
			encoded, err := json.Marshal(&p)
			if err != nil {
				log.Println("Error encoding payload to json: ", err)
				AMQPRECEIVECHAN <- AMQPPayload{CorrID: p.CorrID, Key: p.Key}
			}

			msg := amqp.Publishing{
				Headers:         amqp.Table{},
				ContentType:     "application/json",
				ContentEncoding: "",
				Body:            encoded,
				DeliveryMode:    amqp.Transient,
				Priority:        0,
			}

			// Loop over sending the message until CONFIG.MaxRetries is reached.
			// TODO: The for statement is ugly.
			// Maybe loop over curTries <= CONFIG.MaxRetries instead.
			curTries := 1
			for err = ch.Publish(c.Exchange, NODEID, true, false, msg); err != nil; ch.Publish(c.Exchange, NODEID, true, false, msg) {
				fmt.Println("Error publishing message to AMQP server: ", err, " retries: ", curTries)
				if curTries >= CONFIG.MaxRetries {
					AMQPRECEIVECHAN <- AMQPPayload{CorrID: p.CorrID, Key: p.Key}
					log.Fatal("Unable to send message to AMQP server after ", CONFIG.MaxRetries, " tries.  Exiting.")
				}
				ch.Close()
				ch, notifyClose = getAMQPChan(c)
				curTries += 1
				time.Sleep(time.Duration(CONFIG.RetrySleepSec) * time.Second)
			}

		case closed := <-notifyClose:
			log.Fatal("Received notification of channel closing: ", closed)
		}
	}
}

func getAMQPChan(c AMQPConnInfo) (*amqp.Channel, chan *amqp.Error) {
	var conn *amqp.Connection
	var err error

	log.Println("Connecting to AMQP server.")
	curTries := 1
	for conn, err = amqp.Dial(c.AmqpURI); err != nil; conn, err = amqp.Dial(c.AmqpURI) {
		log.Println("Failed to connect to AMQP server: ", err, " retries: ", curTries)
		if curTries >= CONFIG.MaxRetries {
			log.Fatal("Failed to connect to AMQP server after ", CONFIG.MaxRetries, " tries.  Exiting.")
		}
		curTries += 1
		time.Sleep(time.Duration(CONFIG.RetrySleepSec) * time.Second)
	}
	log.Println("Successfully connected to AMQP server.")

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
		queueArgs["x-message-ttl"] = c.TTL
		_, err = ch.QueueDeclare(c.QueueName, false, true, false, false, queueArgs)
		if err != nil {
			log.Fatal("Could not declare queue: ", err)
		}

		err = ch.QueueBind(c.QueueName, NODEID, c.Exchange, true, nil)
		if err != nil {
			log.Fatal("Could not bind queue to exchange: ", err)
		}
	}

	if err != nil {
		log.Fatal("Unable to create a channel: ", err)
	}

	return ch, notifyClose

}

func httpReceiver(w http.ResponseWriter, r *http.Request) {
	var bodyBytes []byte
	var err error
	rr := HTTPREQRESP{}
	rr.Request = r
	rr.Response = make(chan AMQPPayload)
	rr.CorrID = genID()

	HTTPREQRESPCHAN <- rr

	response := <-rr.Response

	// Add the response headers to the response message
	// If we received an empty payload then this does nothing
	for header, values := range response.Headers {
		for _, value := range values {
			w.Header().Add(header, value)
		}
	}

	// Check if the inside sent us an empty payload, meaning that it encountered
	// an error when executing the request and return a 503 to the client
	// with an empty body
	if response.StatusCode == 0 {
		w.WriteHeader(503)
		bodyBytes = []byte{}
	} else {
		bodyBytes, err = base64.StdEncoding.DecodeString(response.Body)
		if err != nil {
			log.Println("Unable to decode response body from AMQP payload: ", err)
			w.WriteHeader(503)
			bodyBytes = []byte{}
		} else {
			w.WriteHeader(response.StatusCode)
		}
	}

	_, err = w.Write(bodyBytes)
	if err != nil {
		log.Println("Unable to write response: ", err)
	}

}

func genID() string {
	id := uuid.NewV4()
	return fmt.Sprintf("%s", id)
}
