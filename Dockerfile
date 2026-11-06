FROM golang:1.27-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o barista-bot .

FROM alpine:3.20
RUN adduser -D appuser
WORKDIR /app
COPY --from=builder /app/barista-bot .
USER appuser

EXPOSE 8080
CMD ["./barista-bot"]
