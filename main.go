package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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
	EventName string `json:"EventName"`
	URL       string `json:"URL"`
}

type EventObject struct {
	ControllerName string `json:"ControllerName"`
	Name           string `json:"Name"`
	Payload        string `json:"Payload"`
}

type ServiceEvent struct {
	ServiceName string      `json:"ServiceName"`
	EventObj    EventObject `json:"EventObj"`
	TimeStamp   string      `json:"TimeStamp"`
}

var (
	config      Config
	redisClient *redis.Client
	listeners   = make(map[string][]Listener)
	lock        sync.Mutex
)

func formatServiceName(serviceEvent ServiceEvent) string {
	return fmt.Sprintf("%s.%s.%s", serviceEvent.ServiceName, serviceEvent.EventObj.ControllerName, serviceEvent.EventObj.Name)
}

func main() {
	log.Println("Starting service...")
	loadConfig()
	setupRedis()
	router := mux.NewRouter()

	router.HandleFunc("/api/getListeners", getListenersHandler).Methods("GET")
	router.HandleFunc("/api/register", registerListener).Methods("POST")
	router.HandleFunc("/api/remove", removeListener).Methods("POST")
	router.HandleFunc("/api/removeAll", removeAllListeners).Methods("POST")

	log.Println("Setting up RabbitMQ listener...")
	go listenToRabbitMQ()

	log.Println("Service is now listening on port 8080...")
	log.Fatal(http.ListenAndServe(":8080", router))
}

func loadConfig() {
	file, err := os.Open("config.json")
	if err != nil {
		log.Fatalf("Error opening config file: %v", err)
	}
	defer file.Close()

	if err := json.NewDecoder(file).Decode(&config); err != nil {
		log.Fatalf("Error decoding config file: %v", err)
	}
	log.Println("Configuration loaded successfully.")
}

func setupRedis() {
	redisClient = redis.NewClient(&redis.Options{
		Addr: fmt.Sprintf("%s:%d", config.Redis.HostName, config.Redis.Port),
	})

	if _, err := redisClient.Ping(context.Background()).Result(); err != nil {
		log.Fatalf("Failed to connect to Redis: %v", err)
	}
	log.Println("Connected to Redis successfully.")
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

// getEventListeners returns all URLs registered for a given event name.
func getEventListeners(eventName string) ([]string, error) {
	ctx := context.Background()
	// Use LRange to get all elements stored in the list for this event name.
	listeners, err := redisClient.LRange(ctx, "listeners_"+eventName, 0, -1).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve listeners from Redis: %v", err)
	}

	// Convert the list of Redis values to a slice of strings.
	urls := make([]string, len(listeners))
	for i, listener := range listeners {
		urls[i] = listener
	}

	return urls, nil
}

func registerListener(w http.ResponseWriter, r *http.Request) {
	var listener Listener
	if err := json.NewDecoder(r.Body).Decode(&listener); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	lock.Lock()
	listeners[listener.EventName] = append(listeners[listener.EventName], listener)
	lock.Unlock()

	log.Printf("Registered new listener for event %s to %s\n", listener.EventName, listener.URL)
	w.WriteHeader(http.StatusCreated)
}

func removeListener(w http.ResponseWriter, r *http.Request) {
	var listener Listener
	if err := json.NewDecoder(r.Body).Decode(&listener); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	lock.Lock()
	defer lock.Unlock()

	if lst, ok := listeners[listener.EventName]; ok {
		for i, l := range lst {
			if l.URL == listener.URL {
				listeners[listener.EventName] = append(lst[:i], lst[i+1:]...)
				break
			}
		}
	}

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

func listenToRabbitMQ() {
	conn, err := amqp.Dial(fmt.Sprintf("amqp://%s:%s@%s:%d/", config.RabbitMQ.UserName, config.RabbitMQ.Password, config.RabbitMQ.HostName, config.RabbitMQ.Port))
	if err != nil {
		log.Fatalf("Failed to connect to RabbitMQ: %v", err)
	}
	defer conn.Close()

	ch, err := conn.Channel()
	if err != nil {
		log.Fatalf("Failed to open a channel: %v", err)
	}
	defer ch.Close()

	if err := ch.ExchangeDeclare(
		config.RabbitMQ.Exchange, "fanout", false, false, false, false, nil,
	); err != nil {
		log.Fatalf("Failed to declare an exchange: %v", err)
	}

	q, err := ch.QueueDeclare("", false, true, true, false, nil)
	if err != nil {
		log.Fatalf("Failed to declare a queue: %v", err)
	}

	if err := ch.QueueBind(q.Name, "*", config.RabbitMQ.Exchange, false, nil); err != nil {
		log.Fatalf("Failed to bind a queue: %v", err)
	}

	msgs, err := ch.Consume(q.Name, "", true, false, false, false, nil)
	if err != nil {
		log.Fatalf("Failed to register a consumer: %v", err)
	}

	forever := make(chan bool)
	go func() {
		for d := range msgs {
			log.Printf("Received a message: %s", d.Body)
			var event ServiceEvent
			if err := json.Unmarshal(d.Body, &event); err != nil {
				log.Printf("Error parsing message JSON: %v", err)
				continue
			}
			dispatchEvent(event)
		}
	}()

	log.Println(" [*] Waiting for messages. To exit press CTRL+C")
	<-forever
}

func dispatchEvent(event ServiceEvent) {
	formattedEventName := formatServiceName(event)
	if urls, ok := listeners[formattedEventName]; ok {
		log.Printf("Dispatching event to %d listeners for %s\n", len(urls), formattedEventName)
		for _, listener := range urls {
			go callListenerAPI(listener.URL, event)
		}
	} else {
		log.Printf("No listeners registered for event %s\n", formattedEventName)
	}
}

func callListenerAPI(url string, event ServiceEvent) {
	message, err := json.Marshal(event)
	if err != nil {
		log.Printf("Error marshalling event to JSON: %v", err)
		return
	}

	req, err := http.NewRequest("POST", url, bytes.NewReader(message))
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
