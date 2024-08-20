vcl 4.1;
import std;
import directors;
import vsthrottle;

probe chaosprobe {
	.url = "/status";
	.timeout = 1s;
	.interval = 2s;
}
backend chaos1 {
        .host = "chaosbackend:8180";
		.probe = chaosprobe;
		.between_bytes_timeout = 2s;
		.first_byte_timeout = 2s;
		.connect_timeout = 1s;
}

backend chaos2 {
        .host = "chaosbackend:8181";
		.probe = chaosprobe;
		.between_bytes_timeout = 2s;
		.first_byte_timeout = 2s;
		.connect_timeout = 1s;

}

sub vcl_init {
    new chaos = directors.round_robin();
    chaos.add_backend(chaos1);
    chaos.add_backend(chaos2);
}

sub vcl_recv {
	unset req.http.cookie;
	if (req.url ~ "^/foo") {
		set req.backend_hint = chaos1;
	} else if(req.url ~ "^/bar") {
		set req.backend_hint = chaos2;
	} else {
		set req.backend_hint = chaos.backend();
	}
	set req.http.X-Varnish-Director = req.backend_hint;
}

sub vcl_hit {
	set req.http.X-Varnish-Cache = "HIT";
	if (obj.ttl <= 0s && obj.grace > 0s) {
		set req.http.x-cache = "STALE";
	}
}

sub vcl_miss {
	set req.http.X-Varnish-Cache = "MISS";
	if (vsthrottle.is_denied("apikey:" + client.ip, 1000,10s,30s)) {
		set req.http.X-Varnish-Cache = "THROTTLED";
	    return (synth(429, "Throttling Backend"));
	}
}

sub vcl_pass {
	set req.http.X-Varnish-Cache = "PASS";
}

sub vcl_pipe {
	set req.http.X-Varnish-Cache = "PIPE";
}

sub vcl_synth {
	set resp.http.X-Varnish-Cache = "SYNTH";
}

sub vcl_backend_fetch {
}

sub vcl_backend_response {
	set beresp.http.Varnish-Backend = beresp.backend;
}

sub vcl_backend_error {
	set beresp.http.X-Varnish-Cache = "BACKEND_ERROR";
}

sub vcl_deliver {
	if (obj.uncacheable) {
		set resp.http.X-Varnish-Cache = resp.http.X-Varnish-Cache + " uncacheable" ;
	}
	if (!resp.http.X-Varnish-Cache) {
		set resp.http.X-Varnish-Cache = req.http.X-Varnish-Cache;
	}	
	set resp.http.X-Varnish-Hits = obj.hits;
	set resp.http.X-Varnish-Director = req.http.X-Varnish-Director;
	std.log("prom=backends backend=" + resp.http.X-Varnish-Backend + ",director="+ resp.http.X-Varnish-Director + ",cache=" + req.http.X-Varnish-Cache + ",status=" + resp.status +",desc=vcl_deliver");
}

