# Varnish Prometheus Exporter

This [Varnish](https://varnish-cache.org/) prometheus exporter exposes metrics gathered from the Varnish shared memory log AND from Varnish internal counters (varnishstat) depending on the flags used.

## Use case 

Most prometheus exporters for varnish will just parse and export the `varnishstat` metrics. This exporter also parses the `varnishlog` and exports the metrics from the log. This is useful for monitoring exactly what you want from within VCL by adding
 ```
 std.log("<keyword>=<metricname> label1=<value> label2=<value>")
 ```
  to the part of the VCL you like to create a counter for

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

This can be run using `docker-compose up` in this repo. This spins up [chaosbackend](https://github.com/auduny/chaosbackend), varnish and the exporter.

Surf to http://localhost:8080/ to see the backend response through varnish

Then go to http://localhost:7083/metrics to see the metrics


## Usage
```shell
./varnishprom -i 0.0.0.0:7083 -c (amazonaws.com|vglive.no) -l -s -g /etc/varnish.git
```

All flags:

```shell
❯ ./varnishprom --help
Usage of ./varnishprom:
  -S string
        Varnish admin secret file
  -T string
        Varnish admin interface
  -V string
        Loglevel for varnishprom (debug,info,warn,error) (default "info")
  -c string
        Regexp aganst director to collapse backend (default "^kozebamze$")
  -g string
        Check git commit hash of given directory
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
  -v    Print version and exit
```

It needs to be started with the `-l` and/or `-s` flags to start the varnishlog and/or varnishstat parsers.

## systemd file
Example [systemd file](varnishprom.service) to run the exporter as a service.

## Bugs

The varnishstat parses is trying to be smart, but might be stupid. It will try to parse the output of `varnishstat -1`

-   VCL_Log        prom=backends director=vg_frimand_udo,cache=HIT,status=200
-   VCL_Log        prom=backends backend=,director=chaos,cache=HIT,status=200,desc=vcl_deliver  