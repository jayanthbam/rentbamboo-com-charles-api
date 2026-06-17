# Start from a base image
FROM golang:1.24-alpine AS builder


# Set the working directory
WORKDIR /app

# Copy the source code into the container
COPY . .

# Build the Go application
RUN go build -o main .

# Start a new stage
FROM alpine:latest

# Set the working directory
WORKDIR /app

# Copy the built Go application and .env file into the container
COPY --from=builder /app/main .
COPY --from=builder /app/scripts ./scripts

# Expose the port
EXPOSE 8080

# Run the Go application
CMD ["./main"]
