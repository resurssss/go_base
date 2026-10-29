FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
# CGO_ENABLED=0 делает бинарник статическим (запустится в минимальном образе)
RUN CGO_ENABLED=0 go build -o /consumer .

FROM alpine:3.20
COPY --from=build /consumer /consumer
ENTRYPOINT ["/consumer"]
