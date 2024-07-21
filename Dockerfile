# Use the official Golang image to create a build artifact.
# This image is based on Debian and includes the Go toolchain.
FROM golang:1.18 as builder

# Set the Current Working Directory inside the container.
WORKDIR /app

# Copy go mod and sum files.
COPY go.mod go.sum ./

# Download all dependencies. Dependencies will be cached if the go.mod and go.sum files are not changed.
RUN go mod download

# Copy the source code into the container.
COPY main.go .

# Build the Go app as a static binary.
# The -o main flag specifies the output file (main) of the build process.
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o main .

# Start a new stage from scratch to minimize the size of the final image.
# This is known as a multi-stage build.
FROM alpine:latest  

# Install ca-certificates in case you need to call HTTPS endpoints.
RUN apk --no-cache add ca-certificates

# Set the Current Working Directory inside the container.
WORKDIR /root/

# Copy the Pre-built binary file from the previous stage and the config file.
COPY --from=builder /app/main .
COPY config.json .

# Expose port 8080 to the outside world.
EXPOSE 8080

# Command to run the executable.
CMD ["./main"]
