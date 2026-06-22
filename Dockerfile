FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/webrtc-sip ./cmd/gateway
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/phone-cli ./cmd/phone-cli

FROM alpine:3.22
RUN addgroup -S phone && adduser -S -G phone phone
WORKDIR /app
COPY --from=build /out/webrtc-sip /usr/local/bin/webrtc-sip
COPY --from=build /out/phone-cli /usr/local/bin/phone-cli
COPY web ./web
USER phone
EXPOSE 8080/tcp 5066/udp 40000/udp
ENV HTTP_ADDR=0.0.0.0:8080
ENTRYPOINT ["webrtc-sip"]
