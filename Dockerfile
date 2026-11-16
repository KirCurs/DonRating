FROM golang:1.25.3-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/feedback ./cmd/feedback

FROM alpine:3.22
RUN apk add --no-cache ca-certificates \
    && addgroup -S -g 10001 app \
    && adduser -S -D -H -u 10001 -G app app
COPY --from=build --chown=10001:10001 /out/feedback /usr/local/bin/feedback
USER 10001:10001
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/feedback"]
