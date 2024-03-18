FROM golang as builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY *.go ./
RUN CGO_ENABLED=0 GOOS=linux go build -o /varnishprom

#Testing Varnish Enterprise needs payed varnsh enterprise subscription
#FROM quay.io/varnish-software/varnish-plus:latest
#Testing Varnish Open Source 6.0
#FROM:varnish:6.0

#Testing Varnish Open Source 7.4
FROM varnish:7.4


COPY --from=builder /varnishprom /varnishprom
# Set the working directory
EXPOSE 7083

# Run the varnish daemon
ENTRYPOINT ["/varnishprom"]

