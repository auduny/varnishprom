vcl 4.1;
import std;
import directors;

backend chaos1 {
        .host = "127.0.0.1:8181";
}

backend chaos2 {
        .host = "127.0.0.1:8182";
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
	set req.http.x-cache = "HIT";
	if (obj.ttl <= 0s && obj.grace > 0s) {
		set req.http.x-cache = "STALE";
	}
}

sub vcl_miss {
	set req.http.X-Varnish-Cache = "MISS";
}

sub vcl_pass {
	set req.http.X-Varnish-Cache = "PASS";
}

sub vcl_pipe {
	set req.http.X-Varnish-Cache = "PIPE";
}

sub vcl_synth {
	set req.http.X-Varnish-Cache = "SYNTH";
}

sub vcl_backend_response {
	set beresp.http.Varnish-Backend = beresp.backend;
}

sub vcl_deliver {
	if (obj.uncacheable) {
		set resp.http.X-Varnish-Cache = resp.http.X-Varnish-Cache + " uncacheable" ;
	}
	set resp.http.X-Varnish-Hits = obj.hits;
	set resp.http.X-Varnish-Director = req.http.X-Varnish-Director;
	std.log("prom=backends backend=" + resp.http.X-Varnish-Backend + ",director="+ resp.http.X-Varnish-Director + ",cache=" + req.http.X-Varnish-Cache + ",status=" + resp.status +",desc=vcl_deliver");
}

sub vcl_synth {
	set resp.http.X-Cache = "ERROR";
	std.log("prom=backends backend=" + resp.http.V-Backend + ",director="+ resp.http.V-Director + ",cache=" + resp.http.V-Cache + ",status=" + resp.status);
}


