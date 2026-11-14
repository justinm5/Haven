FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /haven ./cmd/haven

FROM alpine:3.23
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=build /haven /app/haven
USER 65532:65532
EXPOSE 8081
ENTRYPOINT ["/app/haven"]
