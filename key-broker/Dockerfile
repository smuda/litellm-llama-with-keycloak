FROM golang:1.27 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 GOWORK=off go build -o /key-broker ./cmd/key-broker

FROM gcr.io/distroless/static
COPY --from=build /key-broker /key-broker
ENTRYPOINT ["/key-broker"]
