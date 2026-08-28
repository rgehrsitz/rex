FROM golang:1.26.6-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /out/rexc ./cmd/rexc && \
    CGO_ENABLED=0 go build -trimpath -o /out/rexd ./cmd/rexd

FROM alpine:3.21

COPY --from=build /out/rexc /usr/local/bin/rexc
COPY --from=build /out/rexd /usr/local/bin/rexd
