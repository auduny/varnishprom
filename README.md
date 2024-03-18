# Varnish Prometheus Exporter

This [Varnish](https://varnish-cache.org/) prometheus exporter exposes metrics gathered from the Varnish shared memory log AND from Varnish internal counters (varnishstat) depending on the flags used.

## Use case 

Most prometheus exporters for varnish will just parse and export the `varnishstat` metrics. This exporter also parses the `varnishlog` and exports the metrics from the log. This is useful for monitoring exactly what you want from within VCL by adding `std.log("<keyword>=<metricname> label1=<value> label2=<value>")` to the part of the VCL you like to create a counter for

The default keyword is `prom`, but this can be changed with the `-k` flag.

### Examples
Count the number of retries above 1 in the backend response:

```vcl
sub vcl_backend_response {
    if (bereq.retries > 0) {
      std.log("prom=backend_retries retries=" + bereq.retries);
    }
}
```

A full working vcl example to count hits/misses for each backend is [here](varnish.vcl)

## Usage
```
./varnishprom --help
Usage of ./varnishprom:
❯ ./varnishprom --help                                     
Usage of ./varnishprom:
  -a string
        Varnish admin interface (default "127.0.0.1:42717")
  -h string
        Hostname to use in metrics, defaults to hostname -S (default "airmone")
  -i string
        Listen interface for metrics endpoint (default "127.0.0.1:7083")
  -k string
        logkey to look for promethus metrics (default "prom")
  -l    Start varnishlog parser
  -p string
        Path for metrics endpoint (default "/metrics")
  -s    Start varnshstats parser
```

It needs to be started with the `-l` and/or `-s` flags to start the varnishlog and/or varnishstat parsers.

## systemd file
Example [systemd file](varnishprom.service) to run the exporter as a service.

## Bugs

The varnishstat parses is trying to be smart, but might be stupid. It will try to parse the output of `varnishstat -1`
