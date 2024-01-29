vcl 4.1;
import std;
import directors;

backend chaos1 {
        .host = "127.0.0.1:8080";
}

backend chaos2 {
        .host = "127.0.0.1:8081";
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
	set req.http.X-Director = req.backend_hint;
}

sub vcl_hit {
	set req.http.V-Cache = "HIT";
}

sub vcl_miss {
	set req.http.V-Cache = "MISS";
}

sub vcl_pass {
	set req.http.V-Cache = "PASS";
}

sub vcl_pipe {
	set req.http.V-Cache = "PIPE";
}

sub vcl_backend_response {
	set beresp.http.V-Backend = beresp.backend;
}

sub vcl_deliver {
	set resp.http.V-Cache = req.http.V-Cache + ":" + obj.hits;
	set resp.http.V-Director = req.http.X-Director;
	std.log("prom=backends backend=" + resp.http.V-Backend + ",director="+ resp.http.V-Director + ",cache=" + req.http.V-Cache + ",status=" + resp.status);
}

sub vcl_synth {
	set resp.http.X-Cache = "ERROR";
	std.log("prom=backends backend=" + resp.http.V-Backend + ",director="+ resp.http.V-Director + ",cache=" + resp.http.V-Cache + ",status=" + resp.status);
}


