FROM golang:1.24-alpine AS builder
ENV GOTOOLCHAIN=auto
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /agenthub-server ./cmd/agenthub-server

FROM alpine:3.20
RUN apk add --no-cache git
COPY --from=builder /agenthub-server /usr/local/bin/agenthub-server
EXPOSE 8080
ENTRYPOINT ["agenthub-server"]
CMD ["--listen", ":8080", "--data", "/data"]
