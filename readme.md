
# Publish Service

Publish Service is a high-performance, low-latency messaging hub designed to simplify the integration of microservices with RabbitMQ. It acts as a centralized service that listens to RabbitMQ messages and forwards them to microservices via HTTP. This design allows microservices to remain lightweight and REST-oriented, without the overhead of directly managing RabbitMQ connections.

## Features

-   **Centralized Message Handling**: Consolidates RabbitMQ message handling, reducing the complexity in microservices.
-   **HTTP Forwarding**: Automatically forwards messages to microservices over HTTP.
-   **High Performance**: Built with Go, offering excellent performance and low latency.
-   **Scalability**: Easily scales to handle increased load using Docker and Redis.
-   **Simple Configuration**: Configured through JSON, allowing for easy changes to RabbitMQ and Redis settings.

## Prerequisites

Ensure you have the following installed:

-   Docker
-   Go 1.15 or later
-   RabbitMQ
-   Redis

## Installation and Setup

### Clone the Repository

bash

Copy code

`git clone https://github.com/yourusername/publish-service.git
cd publish-service` 

### Configure the Service

Edit the `config.json` file to match your environment:

json

Copy code

`{
  "RabbitMQ": {
    "HostName": "localhost",
    "Port": 5672,
    "UserName": "guest",
    "Password": "guest",
    "Exchange": "Publisher"
  },
  "Redis": {
    "HostName": "localhost",
    "Port": 6379
  }
}` 

### Building with Docker

Build and run the Docker container:

bash

Copy code

`docker build -t publish-service:latest .
docker run -d -p 8080:8080 publish-service:latest` 

## API Reference

The service offers several API endpoints for managing listeners:

-   **GET /api/getListeners**: Retrieves all registered listeners.
-   **POST /api/register**: Registers a new listener.
-   **POST /api/remove**: Removes an existing listener.
-   **POST /api/removeAll**: Clears all listeners.

## Development

To set up a local development environment:

1.  **Install Dependencies**:
    
    bash
    
    Copy code
    
    `go mod tidy` 
    
2.  **Run the Application**:
    
    bash
    
    Copy code
    
    `go run main.go` 
    

## Contributing

Contributions are welcome and make our community thrive. Here's how you can contribute:

1.  Fork the Project
2.  Create your Feature Branch (`git checkout -b feature/AmazingFeature`)
3.  Commit your Changes (`git commit -m 'Add some AmazingFeature'`)
4.  Push to the Branch (`git push origin feature/AmazingFeature`)
5.  Open a Pull Request

## License

This project is licensed under the MIT License - see the `LICENSE` file for details.
