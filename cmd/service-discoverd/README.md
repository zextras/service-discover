Service-discover daemon
===

This folder contains the service-discover daemon. Even if the whole
service-discovering is done by Consul, we build a wrapper around this to
interact with Zimbra and provide a dynamically set of options to Consul that
otherwise we would need to provide statically. On top of that, this daemon takes
care of possible start up and tear down checks.
