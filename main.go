package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sync"

	"github.com/go-redis/redis/v8"
	"github.com/gorilla/mux"
	"github.com/streadway/amqp"
)

type Config struct {
	RabbitMQ struct {
		HostName string `json:"HostName"`
		Port     int    `json:"Port"`
		UserName string `json:"UserName"`
		Password string `json:"Password"`
		Exchange string `json:"Exchange"`
	} `json:"RabbitMQ"`
	Redis struct {
		HostName string `json:"HostName"`
		Port     int    `json:"Port"`
	} `json:"Redis"`
}

type Listener struct {
	EventName string
	URL       string
}

var (
	config      Config
	redisClient *redis.Client
	listeners   = make(map[string][]Listener)
	lock        sync.Mutex
)

func main() {
	log.Println("Starting service...")
	loadConfig()
	setupRedis()
	router := mux.NewRouter()

	log.Println("Setting up RabbitMQ listener...")
	go listenToRabbitMQ()

	log.Println("Configuring API endpoints...")
	router.HandleFunc("/api/getListeners", getListenersHandler).Methods("GET")
	router.HandleFunc("/api/register", registerListener).Methods("POST")
	router.HandleFunc("/api/remove", removeListener).Methods("POST")
	router.HandleFunc("/api/removeAll", removeAllListeners).Methods("POST")

	log.Println("Service is now listening on port 8080...")
	log.Fatal(http.ListenAndServe(":8080", router))
}

func loadConfig() {
	log.Println("Loading configuration from config.json...")
	file, err := os.Open("config.json")
	if err != nil {
		log.Fatalf("Error opening config file: %v", err)
	}
	defer file.Close()

	decoder := json.NewDecoder(file)
	err = decoder.Decode(&config)
	if err != nil {
		log.Fatalf("Error decoding config file: %v", err)
	}
	log.Println("Configuration loaded successfully.")
}

func setupRedis() {
	log.Printf("Connecting to Redis at %s:%d...\n", config.Redis.HostName, config.Redis.Port)
	redisClient = redis.NewClient(&redis.Options{
		Addr: fmt.Sprintf("%s:%d", config.Redis.HostName, config.Redis.Port),
	})

	_, err := redisClient.Ping(context.Background()).Result()
	if err != nil {
		log.Fatalf("Failed to connect to Redis: %v", err)
	}
	log.Println("Connected to Redis successfully.")
}

// getEventListeners returns all URLs registered for a given event name
func getEventListeners(eventName string) ([]string, error) {
	ctx := context.Background()

	// Use LRange to get all elements stored in the list for this event name
	listeners, err := redisClient.LRange(ctx, "listeners_"+eventName, 0, -1).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve listeners from Redis: %v", err)
	}

	return listeners, nil
}

func getListenersHandler(w http.ResponseWriter, r *http.Request) {
	eventName := r.URL.Query().Get("eventName")
	if eventName == "" {
		http.Error(w, "Event name is required", http.StatusBadRequest)
		return
	}

	listeners, err := getEventListeners(eventName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	response, err := json.Marshal(listeners)
	if err != nil {
		http.Error(w, "Failed to parse listeners response", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(response)
}

func ensureExchangeExists(ch *amqp.Channel, exchangeName string) error {
	// Attempt to declare the exchange, which is idempotent and will not recreate if it already exists with the same parameters
	return ch.ExchangeDeclare(
		exchangeName, // exchange name
		"topic",      // type
		true,         // durable
		false,        // auto-deleted
		false,        // internal
		false,        // no-wait
		nil,          // arguments
	)
}

func listenToRabbitMQ() {
	log.Println("Connecting to RabbitMQ...")
	conn, err := amqp.Dial(fmt.Sprintf("amqp://%s:%s@%s:%d/",
		config.RabbitMQ.UserName, config.RabbitMQ.Password, config.RabbitMQ.HostName, config.RabbitMQ.Port))
	if err != nil {
		log.Fatalf("Failed to connect to RabbitMQ: %v", err)
	}
	defer conn.Close()

	ch, err := conn.Channel()
	if err != nil {
		log.Fatalf("Failed to open a channel: %v", err)
	}
	defer ch.Close()

	// Ensure the exchange exists
	if err := ensureExchangeExists(ch, config.RabbitMQ.Exchange); err != nil {
		log.Fatalf("Failed to declare an exchange: %v", err)
	}

	// Declare a queue
	q, err := ch.QueueDeclare(
		"",    // Empty queue name, RabbitMQ will generate a unique name
		false, // Non-durable
		true,  // Delete when unused
		true,  // Exclusive
		false, // No-wait
		nil,   // Arguments
	)
	if err != nil {
		log.Fatalf("Failed to declare a queue: %v", err)
	}

	// The routing key needs to match the exchange name used in the .NET publisher
	routingKey := "*" // Adjust this to match your actual routing key
	err = ch.QueueBind(
		q.Name,                   // queue name
		routingKey,               // routing key
		config.RabbitMQ.Exchange, // exchange - use the default exchange
		false,                    // no-wait
		nil,                      // arguments
	)
	if err != nil {
		log.Fatalf("Failed to bind a queue: %v", err)
	}

	msgs, err := ch.Consume(
		q.Name, // queue
		"",     // consumer tag
		true,   // auto-ack
		false,  // exclusive
		false,  // no-local
		false,  // no-wait
		nil,    // args
	)
	if err != nil {
		log.Fatalf("Failed to register a consumer: %v", err)
	}

	forever := make(chan bool)

	go func() {
		for d := range msgs {
			log.Printf("Received a message: %s", d.Body)
			dispatchEvent(string(d.Body))
		}
	}()

	log.Println(" [*] Waiting for messages. To exit press CTRL+C")
	<-forever
}

func registerListener(w http.ResponseWriter, r *http.Request) {
	var listener Listener
	err := json.NewDecoder(r.Body).Decode(&listener)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	lock.Lock()
	listeners[listener.EventName] = append(listeners[listener.EventName], listener)
	lock.Unlock()

	// Store each new listener URL in a Redis list associated with the event name
	storeInRedis("listeners_"+listener.EventName, listener.URL)

	log.Printf("Registered new listener for event %s to %s\n", listener.EventName, listener.URL)
	w.WriteHeader(http.StatusCreated)
}

func removeListener(w http.ResponseWriter, r *http.Request) {
	var listener Listener
	err := json.NewDecoder(r.Body).Decode(&listener)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	lock.Lock()
	if _, ok := listeners[listener.EventName]; ok {
		var newList []Listener
		for _, l := range listeners[listener.EventName] {
			if l.URL != listener.URL {
				newList = append(newList, l)
			}
		}
		listeners[listener.EventName] = newList
	}
	lock.Unlock()

	log.Printf("Removed listener for event %s from %s\n", listener.EventName, listener.URL)
	w.WriteHeader(http.StatusOK)
}

func removeAllListeners(w http.ResponseWriter, r *http.Request) {
	lock.Lock()
	listeners = make(map[string][]Listener)
	lock.Unlock()

	log.Println("Removed all listeners")
	w.WriteHeader(http.StatusOK)
}

func dispatchEvent(message string) {
	lock.Lock()
	defer lock.Unlock()

	storeInRedis("lastEvent", message)
	for _, l := range listeners {
		for _, listener := range l {
			go callListenerAPI(listener.URL, message)
		}
	}
	log.Printf("Dispatching event to %d listeners\n", len(listeners))
}

func callListenerAPI(url, message string) {
	jsonStr := []byte(message)
	req, err := http.NewRequest("POST", url, io.NopCloser(bytes.NewReader(jsonStr)))
	if err != nil {
		log.Printf("Error creating request: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Error calling listener API: %v", err)
		return
	}
	defer resp.Body.Close()
	log.Printf("Called %s, got status %d", url, resp.StatusCode)
}

func storeInRedis(key, value string) {
	err := redisClient.RPush(context.Background(), key, value).Err()
	if err != nil {
		log.Printf("Failed to append to list in Redis: %v", err)
	} else {
		log.Printf("Appended %s to list %s in Redis", value, key)
	}
}
