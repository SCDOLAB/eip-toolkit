# ---- builder ----
FROM golang:1.23-alpine AS builder
WORKDIR /src

# cache deps
COPY go.mod go.sum ./
RUN go mod download

# build
COPY go/ ./go/
RUN CGO_ENABLED=0 go build -o /out/logfilter ./go/logfilter

# ---- runtime ----
FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=builder /out/logfilter /app/logfilter

EXPOSE 8080
ENTRYPOINT ["/app/logfilter"]
