FROM golang as builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY *.go ./
RUN CGO_ENABLED=0 GOOS=linux go build -o /varnishprom

FROM varnish

COPY --from=builder /varnishprom /varnishprom
# Set the working directory
EXPOSE 7083

# Run the varnish daemon
ENTRYPOINT ["/varnishprom"]

