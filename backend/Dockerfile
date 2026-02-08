FROM golang:1.23-alpine AS build
WORKDIR /src
RUN apk add --no-cache git

COPY go.mod ./
COPY main.go ./
COPY internal ./internal

RUN go mod tidy && go mod download
RUN go build -o /out/app ./main.go

FROM alpine:3.20
WORKDIR /app
COPY --from=build /out/app /app/app
EXPOSE 8080
CMD ["/app/app"]
