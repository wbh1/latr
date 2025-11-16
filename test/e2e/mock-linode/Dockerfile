FROM golang:1.21-alpine

WORKDIR /app

COPY main.go .

RUN go build -o mock-linode main.go

EXPOSE 8080

CMD ["./mock-linode"]
