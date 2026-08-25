# 第一階段：編譯 (Builder)
FROM golang:1.25-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY *.go ./
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o sso-broker main.go

# 第二階段：執行 (Runner)
FROM alpine:latest
WORKDIR /root/
COPY --from=builder /app/sso-broker .
EXPOSE 8080
CMD ["./sso-broker"]