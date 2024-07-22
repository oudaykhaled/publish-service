# Use the official Golang image to create a build artifact.
# This image is based on Debian and includes the Go toolchain.
FROM golang:1.22.5 as builder

# Set the Current Working Directory inside the container.
WORKDIR /app

# Copy the source code and configuration file into the container.
COPY . .

# Initialize a new module and tidy up dependencies.
# Replace 'example.com/myapp' with your actual module path.
RUN go mod init example.com/myapp && \
    go mod tidy

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
COPY --from=builder /app/config.json .

# Expose port 8080 to the outside world.
EXPOSE 8080

# Command to run the executable.
CMD ["./main"]
