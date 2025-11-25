FROM docker.io/library/golang:1.25.4 AS builder
WORKDIR /app
COPY . .
ENV CGO_ENABLED=0 GOOS=linux GOARCH=amd64
RUN go build -o /app/bin/controller ./main.go

FROM gcr.io/distroless/static:nonroot
COPY --from=builder /app/bin/controller /controller
ENTRYPOINT ["/controller"]
