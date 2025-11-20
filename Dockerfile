# Stage 1: Build React frontend
FROM node:18-alpine AS frontend-builder

WORKDIR /app/web

# Copy package files and install dependencies
COPY web/package*.json ./
RUN npm ci

# Copy source and build
COPY web/ ./
RUN npm run build

# Stage 2: Build Go backend
FROM golang:1.21-alpine AS backend-builder

WORKDIR /app

# Install build dependencies
RUN apk add --no-cache gcc musl-dev libx11-dev

# Copy go mod files and download dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Copy built frontend from previous stage
COPY --from=frontend-builder /app/web/dist ./web/dist

# Build the Go binary
RUN CGO_ENABLED=1 GOOS=linux go build -o focusstreamer ./cmd/focusstreamer

# Stage 3: Final runtime image
FROM alpine:latest

# Install runtime dependencies for X11
RUN apk add --no-cache \
    libx11 \
    libxrandr \
    libxinerama \
    libxcursor \
    libxi

WORKDIR /app

# Copy the binary and web assets
COPY --from=backend-builder /app/focusstreamer .
COPY --from=backend-builder /app/web/dist ./web/dist

# Create config directory
RUN mkdir -p /root/.config/focusstreamer

# Expose the default port
EXPOSE 8080

# Set environment variable for X11
ENV DISPLAY=:0

# Run the application
ENTRYPOINT ["./focusstreamer"]
CMD ["serve"]
