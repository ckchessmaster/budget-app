FROM golang:1.27-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/api \
 && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/migrate ./cmd/migrate \
 && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/worker ./cmd/worker

FROM alpine:3.20

RUN apk add --no-cache ca-certificates \
 && addgroup -g 10001 -S app \
 && adduser -u 10001 -S -G app app

COPY --from=builder /out/api /usr/local/bin/api
COPY --from=builder /out/migrate /usr/local/bin/migrate
# The Temporal sync worker shares this image. Bank sync never runs unless something
# polls the task queue, and shipping the binary without deploying a container for it
# is how sync came to be silently dead in production.
COPY --from=builder /out/worker /usr/local/bin/worker

USER 10001:10001
EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/api"]
