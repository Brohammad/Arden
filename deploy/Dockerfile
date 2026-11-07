FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/arden ./cmd/arden

FROM alpine:3.22
RUN apk add --no-cache ca-certificates
COPY --from=build /out/arden /usr/local/bin/arden
ENTRYPOINT ["arden"]
