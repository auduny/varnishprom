# Varnish Prometheus Exporter

This [Varnish](https://varnish-cache.org/) prometheus exporter exposes metrics gathered from the Varnish shared memory log AND from Varnish internal counters (varnishstat).

## Usage
```
./varnishprom --help
Usage of ./varnishprom:
  -h string
    	Hostname to use in metrics, defaults to hostname -S' (default "airmone")
  -i string
    	Listen interface for metrics endpoint (default "127.0.0.1:8083")
  -k string
    	logkey to look for promethus metrics (default "prom")
  -p string
    	Path for metrics endpoint (default "/metrics")
```

